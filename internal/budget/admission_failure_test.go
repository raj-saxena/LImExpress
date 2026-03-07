package budget_test

import (
	"errors"
	"testing"

	"github.com/limexpress/gateway/internal/db"
)

func TestBudgetAdmission_PolicyLookupError_FailsOpen(t *testing.T) {
	q := &mockQuerier{policyErr: errors.New("db unavailable")}
	w := runRequest(t, q, defaultKAC())
	assertPassThrough(t, w)
}

func TestBudgetAdmission_UsageLookupError_FailsOpen(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(1.0),
		},
		userHourErr: errors.New("usage read failed"),
	}
	w := runRequest(t, q, defaultKAC())
	assertPassThrough(t, w)
}

func TestBudgetAdmission_TeamUsageLookupError_FailsOpen(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(1.0),
		},
		teamDayErr: errors.New("team usage read failed"),
	}
	w := runRequest(t, q, teamKAC())
	assertPassThrough(t, w)
}
