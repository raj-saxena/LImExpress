package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/middleware"
)

// AnalyticsHandler handles analytics API endpoints for usage data.
// It must be mounted after VirtualKeyAuth middleware so that KeyAuthContext
// is available in the request context.
type AnalyticsHandler struct {
	q db.Querier
}

// NewAnalyticsHandler creates an AnalyticsHandler backed by the given querier.
func NewAnalyticsHandler(q db.Querier) *AnalyticsHandler {
	return &AnalyticsHandler{q: q}
}

// RegisterRoutes mounts the analytics endpoints on the given router.
func (h *AnalyticsHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/usage/daily", h.dailyUsage)
	r.Get("/api/v1/usage/top-users", h.topUsers)
	r.Get("/api/v1/usage/top-models", h.topModels)
}

// dailyUsage handles GET /api/v1/usage/daily.
func (h *AnalyticsHandler) dailyUsage(w http.ResponseWriter, r *http.Request) {
	kac, ok := middleware.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	rows, err := h.q.GetDailyUsageByOrg(r.Context(), db.GetDailyUsageByOrgParams{
		OrgID:         kac.OrgID,
		WindowStart:   toTimestamptz(from),
		WindowStart_2: toTimestamptz(to),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type dailyRow struct {
		Day          string  `json:"day"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		CostUSD      float64 `json:"cost_usd"`
		RequestCount int32   `json:"request_count"`
	}

	data := make([]dailyRow, 0, len(rows))
	for _, r := range rows {
		cost := numericToFloat64(r.CostUsd)
		day := pgDateToString(r.Day)
		data = append(data, dailyRow{
			Day:          day,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			CostUSD:      cost,
			RequestCount: r.RequestCount,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// topUsers handles GET /api/v1/usage/top-users.
func (h *AnalyticsHandler) topUsers(w http.ResponseWriter, r *http.Request) {
	kac, ok := middleware.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	limit := parseLimit(r)

	rows, err := h.q.GetTopUsersByOrg(r.Context(), db.GetTopUsersByOrgParams{
		OrgID:         kac.OrgID,
		WindowStart:   toTimestamptz(from),
		WindowStart_2: toTimestamptz(to),
		Limit:         int32(limit),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type userRow struct {
		UserID        string  `json:"user_id"`
		Email         string  `json:"email"`
		TotalCostUSD  float64 `json:"total_cost_usd"`
		TotalRequests int32   `json:"total_requests"`
	}

	data := make([]userRow, 0, len(rows))
	for _, r := range rows {
		data = append(data, userRow{
			UserID:        uuidToString(r.ID),
			Email:         r.Email,
			TotalCostUSD:  numericToFloat64(r.TotalCostUsd),
			TotalRequests: r.TotalRequests,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// topModels handles GET /api/v1/usage/top-models.
func (h *AnalyticsHandler) topModels(w http.ResponseWriter, r *http.Request) {
	kac, ok := middleware.FromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	from, to, ok := parseDateRange(w, r)
	if !ok {
		return
	}

	limit := parseLimit(r)

	rows, err := h.q.GetTopModelsByOrg(r.Context(), db.GetTopModelsByOrgParams{
		OrgID:         kac.OrgID,
		WindowStart:   toTimestamptz(from),
		WindowStart_2: toTimestamptz(to),
		Limit:         int32(limit),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type modelRow struct {
		Model         string  `json:"model"`
		Provider      string  `json:"provider"`
		TotalCostUSD  float64 `json:"total_cost_usd"`
		TotalRequests int32   `json:"total_requests"`
	}

	data := make([]modelRow, 0, len(rows))
	for _, r := range rows {
		data = append(data, modelRow{
			Model:         r.Model,
			Provider:      r.Provider,
			TotalCostUSD:  numericToFloat64(r.TotalCostUsd),
			TotalRequests: r.TotalRequests,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// parseDateRange parses the `from` and `to` query parameters.
// Returns (from, to, true) on success or writes an error response and returns
// (zero, zero, false) on failure.
func parseDateRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	now := time.Now().UTC()
	// Default: last 90 days
	defaultFrom := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -90)
	// Default to: tomorrow midnight (exclusive upper bound)
	defaultTo := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

	from := defaultFrom
	to := defaultTo

	if s := r.URL.Query().Get("from"); s != "" {
		t, err := parseDate(s)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid date: from")
			return time.Time{}, time.Time{}, false
		}
		from = t
	}

	if s := r.URL.Query().Get("to"); s != "" {
		t, err := parseDate(s)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid date: to")
			return time.Time{}, time.Time{}, false
		}
		to = t
	}

	return from, to, true
}

// parseDate parses a date string as either RFC3339 or date-only (2006-01-02).
func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		// Interpret date-only strings as UTC midnight.
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %q", s)
}

// parseLimit parses the `limit` query param and clamps it to [1, 100].
// Returns 10 if the param is absent or zero.
func parseLimit(r *http.Request) int {
	const defaultLimit = 10
	const maxLimit = 100

	s := r.URL.Query().Get("limit")
	if s == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 1
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

// toTimestamptz wraps a time.Time in a valid pgtype.Timestamptz.
func toTimestamptz(t time.Time) pgtype.Timestamptz {
	var ts pgtype.Timestamptz
	ts.Time = t
	ts.Valid = true
	return ts
}

// numericToFloat64 converts a pgtype.Numeric to float64; returns 0 on error.
func numericToFloat64(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil {
		return 0
	}
	return f.Float64
}

// uuidToString converts a pgtype.UUID to the standard 8-4-4-4-12 string form.
func uuidToString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// pgDateToString converts a pgtype.Date to "YYYY-MM-DD" format.
func pgDateToString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
