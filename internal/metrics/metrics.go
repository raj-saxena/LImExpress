package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RequestsTotal counts completed proxy requests.
// Labels: org, provider, model, status_class (2xx, 4xx, 5xx)
var RequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "llmgw",
		Subsystem: "proxy",
		Name:      "requests_total",
		Help:      "Total number of completed proxy requests.",
	},
	[]string{"org", "provider", "model", "status_class"},
)

// BudgetDeniedTotal counts requests rejected by budget admission.
// Labels: org, reason (user_hour, user_day, team_hour, team_day)
var BudgetDeniedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "llmgw",
		Subsystem: "budget",
		Name:      "denied_total",
		Help:      "Total number of requests rejected by budget admission control.",
	},
	[]string{"org", "reason"},
)

// InputTokensTotal counts input tokens processed.
// Labels: org, provider, model
var InputTokensTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "llmgw",
		Subsystem: "usage",
		Name:      "input_tokens_total",
		Help:      "Total number of input tokens processed.",
	},
	[]string{"org", "provider", "model"},
)

// OutputTokensTotal counts output tokens processed.
// Labels: org, provider, model
var OutputTokensTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "llmgw",
		Subsystem: "usage",
		Name:      "output_tokens_total",
		Help:      "Total number of output tokens processed.",
	},
	[]string{"org", "provider", "model"},
)

// CostUSDTotal tracks total cost in USD.
// Labels: org, provider, model
var CostUSDTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "llmgw",
		Subsystem: "usage",
		Name:      "cost_usd_total",
		Help:      "Total cost in USD for all processed requests.",
	},
	[]string{"org", "provider", "model"},
)

// StreamDurationSeconds observes SSE stream duration.
// Labels: org, provider, model
var StreamDurationSeconds = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "llmgw",
		Subsystem: "proxy",
		Name:      "stream_duration_seconds",
		Help:      "Duration in seconds of SSE streams.",
		Buckets:   []float64{0.1, 0.5, 1, 5, 10, 30, 60, 120, 300},
	},
	[]string{"org", "provider", "model"},
)

// ActiveStreams tracks currently open SSE streams.
// Labels: org
var ActiveStreams = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: "llmgw",
		Subsystem: "proxy",
		Name:      "active_streams",
		Help:      "Number of currently open SSE streams.",
	},
	[]string{"org"},
)
