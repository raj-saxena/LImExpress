package gateway

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/config"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/middleware"
	"github.com/limexpress/gateway/internal/proxy"
)

// PostChargeAccounting wraps the proxy handler and records token usage + cost
// after the upstream response has been fully streamed to the client.
//
// It must be placed around the proxy handler in the middleware chain, after
// VirtualKeyAuth has already injected a KeyAuthContext.
//
// Accounting is fire-and-forget: DB writes happen in a goroutine and never
// block or fail the client response.
func PostChargeAccounting(q db.Querier, pricing map[string]config.ModelPrice) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			kac, ok := middleware.FromContext(r.Context())
			if !ok {
				// No auth context — pass through; VirtualKeyAuth will 401 this.
				next.ServeHTTP(w, r)
				return
			}

			// Pre-seed a *Usage pointer in context. The proxy will fill it
			// while streaming and we read it after ServeHTTP returns.
			uPtr := &proxy.Usage{}
			ctx := proxy.ContextWithUsage(r.Context(), uPtr)

			next.ServeHTTP(w, r.WithContext(ctx))

			// Nothing to record if the upstream returned no usage data
			// (e.g. non-2xx, cancelled request, or provider didn't report tokens).
			if uPtr.InputTokens == 0 && uPtr.OutputTokens == 0 {
				return
			}

			// Snapshot immutable values before the goroutine runs.
			costUSD := computeCost(uPtr.Model, uPtr.InputTokens, uPtr.OutputTokens, pricing)
			u := *uPtr // copy so the goroutine owns its data
			k := *kac

			go func() {
				recordUsage(context.Background(), q, &k, &u, costUSD)
			}()
		})
	}
}

// computeCost returns cost in USD for the given token counts using the config
// pricing table. Returns 0 if the model is not found in the table.
func computeCost(model string, inputTokens, outputTokens int32, pricing map[string]config.ModelPrice) float64 {
	p, ok := pricing[model]
	if !ok {
		return 0
	}
	return float64(inputTokens)/1_000_000*p.InputPerMToken +
		float64(outputTokens)/1_000_000*p.OutputPerMToken
}

// recordUsage writes a usage_events row and upserts the hour + day aggregates.
// Errors are silently dropped — accounting failures must not surface to callers.
func recordUsage(ctx context.Context, q db.Querier, kac *middleware.KeyAuthContext, u *proxy.Usage, costUSD float64) {
	cost := numericFromFloat(costUSD)

	_, _ = q.InsertUsageEvent(ctx, db.InsertUsageEventParams{
		OrgID:        kac.OrgID,
		UserID:       kac.UserID,
		TeamID:       kac.TeamID,
		VirtualKeyID: kac.KeyID,
		Provider:     u.Provider,
		Model:        u.Model,
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		CostUsd:      cost,
	})

	hourParams := db.UpsertUsageAggHourParams{
		OrgID:        kac.OrgID,
		UserID:       kac.UserID,
		TeamID:       kac.TeamID,
		Provider:     u.Provider,
		Model:        u.Model,
		InputTokens:  int64(u.InputTokens),
		OutputTokens: int64(u.OutputTokens),
		CostUsd:      cost,
	}
	_ = q.UpsertUsageAggHour(ctx, hourParams)
	_ = q.UpsertUsageAggDay(ctx, db.UpsertUsageAggDayParams(hourParams))
}

// numericFromFloat converts a float64 USD amount to pgtype.Numeric (8 dp).
func numericFromFloat(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(fmt.Sprintf("%.8f", f))
	return n
}
