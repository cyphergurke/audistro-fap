package obs

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	registerOnce sync.Once

	handlerRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "fap",
			Name:      "http_handler_requests_total",
			Help:      "Total number of HTTP handler requests by handler and result.",
		},
		[]string{"handler", "result"},
	)

	authOutcomes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "fap",
			Name:      "auth_outcomes_total",
			Help:      "Authorization outcomes for protected routes.",
		},
		[]string{"result"},
	)
)

func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(handlerRequests)
		prometheus.MustRegister(authOutcomes)
	})
}

func ObserveChallenge(result string) {
	Register()
	handlerRequests.WithLabelValues("challenge", result).Inc()
}

func ObserveToken(result string) {
	Register()
	handlerRequests.WithLabelValues("token", result).Inc()
}

func ObserveWebhook(result string) {
	Register()
	handlerRequests.WithLabelValues("webhook_lnbits", result).Inc()
}

func ObserveAuth(result string) {
	Register()
	authOutcomes.WithLabelValues(result).Inc()
}
