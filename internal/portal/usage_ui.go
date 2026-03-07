package portal

import "github.com/limexpress/gateway/internal/portal/templates"

// Re-export usage row types from the templates package so portal handlers
// can reference them without importing the templates package directly.

// DailyRow holds aggregated daily usage data for the usage dashboard.
type DailyRow = templates.DailyRow

// TopUserRow holds per-user aggregated usage for the usage dashboard.
type TopUserRow = templates.TopUserRow

// TopModelRow holds per-model aggregated usage for the usage dashboard.
type TopModelRow = templates.TopModelRow
