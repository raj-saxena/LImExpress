package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/portal/auth"
)

// DashboardDataHandler exposes portal usage aggregation endpoints.
type DashboardDataHandler struct {
	q db.Querier
}

// NewDashboardDataHandler creates a dashboard API handler.
func NewDashboardDataHandler(q db.Querier) *DashboardDataHandler {
	return &DashboardDataHandler{q: q}
}

// RegisterRoutes mounts M2-T4 endpoints.
func (h *DashboardDataHandler) RegisterRoutes(r chi.Router) {
	r.Get("/portal/usage/daily", h.dailyUsage)
	r.Get("/portal/usage/top-users", h.topUsers)
	r.Get("/portal/usage/top-models", h.topModels)
}

func (h *DashboardDataHandler) dailyUsage(w http.ResponseWriter, r *http.Request) {
	org, ok := dashboardRequireOrgContext(w, r)
	if !ok {
		return
	}
	from, to, ok := dashboardParseDateRange(w, r)
	if !ok {
		return
	}

	rows, err := h.q.GetDailyUsageByOrg(r.Context(), db.GetDailyUsageByOrgParams{
		OrgID:         org.OrgID,
		WindowStart:   dashboardToTimestamptz(from),
		WindowStart_2: dashboardToTimestamptz(to),
	})
	if err != nil {
		dashboardWriteJSONError(w, http.StatusInternalServerError, "internal error")
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
	for _, row := range rows {
		data = append(data, dailyRow{
			Day:          dashboardDateToString(row.Day),
			InputTokens:  row.InputTokens,
			OutputTokens: row.OutputTokens,
			CostUSD:      dashboardNumericToFloat64(row.CostUsd),
			RequestCount: row.RequestCount,
		})
	}

	dashboardWriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *DashboardDataHandler) topUsers(w http.ResponseWriter, r *http.Request) {
	org, ok := dashboardRequireOrgContext(w, r)
	if !ok {
		return
	}
	from, to, ok := dashboardParseDateRange(w, r)
	if !ok {
		return
	}
	limit := dashboardParseLimit(r)

	rows, err := h.q.GetTopUsersByOrg(r.Context(), db.GetTopUsersByOrgParams{
		OrgID:         org.OrgID,
		WindowStart:   dashboardToTimestamptz(from),
		WindowStart_2: dashboardToTimestamptz(to),
		Limit:         int32(limit),
	})
	if err != nil {
		dashboardWriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type userRow struct {
		UserID        string  `json:"user_id"`
		Email         string  `json:"email"`
		TotalCostUSD  float64 `json:"total_cost_usd"`
		TotalRequests int32   `json:"total_requests"`
	}

	data := make([]userRow, 0, len(rows))
	for _, row := range rows {
		data = append(data, userRow{
			UserID:        dashboardUUIDToString(row.ID),
			Email:         row.Email,
			TotalCostUSD:  dashboardNumericToFloat64(row.TotalCostUsd),
			TotalRequests: row.TotalRequests,
		})
	}

	dashboardWriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *DashboardDataHandler) topModels(w http.ResponseWriter, r *http.Request) {
	org, ok := dashboardRequireOrgContext(w, r)
	if !ok {
		return
	}
	from, to, ok := dashboardParseDateRange(w, r)
	if !ok {
		return
	}
	limit := dashboardParseLimit(r)

	rows, err := h.q.GetTopModelsByOrg(r.Context(), db.GetTopModelsByOrgParams{
		OrgID:         org.OrgID,
		WindowStart:   dashboardToTimestamptz(from),
		WindowStart_2: dashboardToTimestamptz(to),
		Limit:         int32(limit),
	})
	if err != nil {
		dashboardWriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	type modelRow struct {
		Model         string  `json:"model"`
		Provider      string  `json:"provider"`
		TotalCostUSD  float64 `json:"total_cost_usd"`
		TotalRequests int32   `json:"total_requests"`
	}

	data := make([]modelRow, 0, len(rows))
	for _, row := range rows {
		data = append(data, modelRow{
			Model:         row.Model,
			Provider:      row.Provider,
			TotalCostUSD:  dashboardNumericToFloat64(row.TotalCostUsd),
			TotalRequests: row.TotalRequests,
		})
	}

	dashboardWriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func dashboardParseDateRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -90)
	to := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

	if s := r.URL.Query().Get("from"); s != "" {
		t, err := dashboardParseDate(s)
		if err != nil {
			dashboardWriteJSONError(w, http.StatusBadRequest, "invalid date: from")
			return time.Time{}, time.Time{}, false
		}
		from = t
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := dashboardParseDate(s)
		if err != nil {
			dashboardWriteJSONError(w, http.StatusBadRequest, "invalid date: to")
			return time.Time{}, time.Time{}, false
		}
		to = t
	}
	return from, to, true
}

func dashboardParseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %q", s)
}

func dashboardParseLimit(r *http.Request) int {
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

func dashboardToTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func dashboardNumericToFloat64(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil {
		return 0
	}
	return f.Float64
}

func dashboardDateToString(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

func dashboardUUIDToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func dashboardWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func dashboardWriteJSONError(w http.ResponseWriter, status int, msg string) {
	dashboardWriteJSON(w, status, map[string]string{"error": msg})
}

func dashboardRequireOrgContext(w http.ResponseWriter, r *http.Request) (*auth.OrgContext, bool) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		dashboardWriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		dashboardWriteJSONError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	return org, true
}
