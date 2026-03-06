package budget

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/middleware"
)

// windowKind identifies which fixed window a budget limit belongs to.
type windowKind int

const (
	windowHour windowKind = iota
	windowDay
)

// budgetDenial represents a denied request with the applicable retry window.
type budgetDenial struct {
	window windowKind
}

// retryAfterSeconds returns the number of seconds until the current window resets (UTC).
func retryAfterSeconds(now time.Time, w windowKind) int64 {
	switch w {
	case windowHour:
		next := now.UTC().Truncate(time.Hour).Add(time.Hour)
		return int64(next.Sub(now).Seconds()) + 1
	default: // windowDay
		y, m, d := now.UTC().Date()
		midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
		return int64(midnight.Sub(now).Seconds()) + 1
	}
}

// numericToFloat converts a pgtype.Numeric to float64; returns 0 if not valid.
func numericToFloat(n pgtype.Numeric) float64 {
	if !n.Valid {
		return 0
	}
	f, _ := n.Float64Value()
	return f.Float64
}

// isZeroUUID returns true when the UUID is the zero value (no team).
func isZeroUUID(u pgtype.UUID) bool {
	return u == (pgtype.UUID{})
}

// budgetExceededResponse writes a 429 response with Retry-After header and JSON body.
func budgetExceededResponse(w http.ResponseWriter, retryAfter int64) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", itoa(retryAfter))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":               "budget_exceeded",
		"retry_after_seconds": retryAfter,
	})
}

// itoa converts an int64 to its decimal string representation without importing strconv.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// usageResult aggregates usage fetch results and any error.
type userUsageResult struct {
	hour db.GetCurrentWindowUsageHourRow
	day  db.GetCurrentWindowUsageDayRow
	err  error
}

type teamUsageResult struct {
	hour db.GetTeamWindowUsageHourRow
	day  db.GetTeamWindowUsageDayRow
	err  error
}

// BudgetAdmission returns middleware that denies requests exceeding budget limits.
// It must be placed AFTER VirtualKeyAuth in the middleware chain.
//
// Admission logic (smallest-remaining-wins):
//   - Fetch budget policy; if not found, pass through.
//   - Fetch user window usage (hour + day) concurrently.
//   - If TeamID is non-zero, also fetch team window usage concurrently.
//   - For each limit (cost_hour, cost_day, tokens_hour, tokens_day) check if any
//     party (user OR team) has exhausted it.
//   - Deny with 429 + Retry-After if any limit is exhausted.
func BudgetAdmission(q db.Querier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Step 1: Retrieve auth context; if absent pass through.
			kac, ok := middleware.FromContext(ctx)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Step 2: Fetch budget policy.
			policy, err := q.GetBudgetPolicy(ctx, db.GetBudgetPolicyParams{
				OrgID:  kac.OrgID,
				UserID: kac.UserID,
				TeamID: kac.TeamID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// No policy = no limit.
					next.ServeHTTP(w, r)
					return
				}
				// DB error — fail open to avoid blocking all traffic.
				next.ServeHTTP(w, r)
				return
			}

			// Step 3: Fetch user window usage concurrently (hour + day).
			var wg sync.WaitGroup
			var userResult userUsageResult
			var teamResult teamUsageResult
			hasTeam := !isZeroUUID(kac.TeamID)

			wg.Add(1)
			go func() {
				defer wg.Done()
				var innerErr error
				userResult.hour, innerErr = q.GetCurrentWindowUsageHour(ctx, db.GetCurrentWindowUsageHourParams{
					OrgID:  kac.OrgID,
					UserID: kac.UserID,
				})
				if innerErr != nil {
					userResult.err = innerErr
					return
				}
				userResult.day, innerErr = q.GetCurrentWindowUsageDay(ctx, db.GetCurrentWindowUsageDayParams{
					OrgID:  kac.OrgID,
					UserID: kac.UserID,
				})
				if innerErr != nil {
					userResult.err = innerErr
				}
			}()

			// Step 4: Fetch team window usage concurrently (if team exists).
			if hasTeam {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var innerErr error
					teamResult.hour, innerErr = q.GetTeamWindowUsageHour(ctx, db.GetTeamWindowUsageHourParams{
						OrgID:  kac.OrgID,
						TeamID: kac.TeamID,
					})
					if innerErr != nil {
						teamResult.err = innerErr
						return
					}
					teamResult.day, innerErr = q.GetTeamWindowUsageDay(ctx, db.GetTeamWindowUsageDayParams{
						OrgID:  kac.OrgID,
						TeamID: kac.TeamID,
					})
					if innerErr != nil {
						teamResult.err = innerErr
					}
				}()
			}

			wg.Wait()

			// Fail open on DB errors for usage fetches.
			if userResult.err != nil || teamResult.err != nil {
				next.ServeHTTP(w, r)
				return
			}

			// Step 5: Check limits — smallest-remaining-wins.
			now := time.Now().UTC()

			// Check cost_hour limit.
			if policy.MaxCostUsdHour.Valid {
				limit := numericToFloat(policy.MaxCostUsdHour)
				userCostHour := numericToFloat(userResult.hour.CostUsd)
				if userCostHour >= limit {
					budgetExceededResponse(w, retryAfterSeconds(now, windowHour))
					return
				}
				if hasTeam {
					teamCostHour := numericToFloat(teamResult.hour.CostUsd)
					if teamCostHour >= limit {
						budgetExceededResponse(w, retryAfterSeconds(now, windowHour))
						return
					}
				}
			}

			// Check cost_day limit.
			if policy.MaxCostUsdDay.Valid {
				limit := numericToFloat(policy.MaxCostUsdDay)
				userCostDay := numericToFloat(userResult.day.CostUsd)
				if userCostDay >= limit {
					budgetExceededResponse(w, retryAfterSeconds(now, windowDay))
					return
				}
				if hasTeam {
					teamCostDay := numericToFloat(teamResult.day.CostUsd)
					if teamCostDay >= limit {
						budgetExceededResponse(w, retryAfterSeconds(now, windowDay))
						return
					}
				}
			}

			// Check tokens_hour limit.
			if policy.MaxTokensHour != nil {
				limit := *policy.MaxTokensHour
				if userResult.hour.TotalTokens >= limit {
					budgetExceededResponse(w, retryAfterSeconds(now, windowHour))
					return
				}
				if hasTeam && teamResult.hour.TotalTokens >= limit {
					budgetExceededResponse(w, retryAfterSeconds(now, windowHour))
					return
				}
			}

			// Check tokens_day limit.
			if policy.MaxTokensDay != nil {
				limit := *policy.MaxTokensDay
				if userResult.day.TotalTokens >= limit {
					budgetExceededResponse(w, retryAfterSeconds(now, windowDay))
					return
				}
				if hasTeam && teamResult.day.TotalTokens >= limit {
					budgetExceededResponse(w, retryAfterSeconds(now, windowDay))
					return
				}
			}

			// Step 7: All limits satisfied — pass through.
			next.ServeHTTP(w, r)
		})
	}
}
