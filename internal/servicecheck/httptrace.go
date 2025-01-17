package servicecheck

import (
	"crypto/tls"
	"log"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// unique type for context.Context to avoid collisions.
type kubenurseTypeKey struct{}

// // http.RoundTripper
type RoundTripperFunc func(req *http.Request) (*http.Response, error)

func (rt RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

// This collects traces and logs errors. As promhttp.InstrumentRoundTripperTrace doesn't process
// errors, this is custom made and inspired by prometheus/client_golang's promhttp
func withHttptrace(registry *prometheus.Registry, next http.RoundTripper, durationHistogram []float64) http.RoundTripper {
	httpclientReqTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "httpclient_requests_total",
			Help:      "A counter for requests from the kubenurse http client.",
		},
		// []string{"code", "method", "type"}, // TODO
		[]string{"code", "method"},
	)

	httpclientReqDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "httpclient_request_duration_seconds",
			Help:      "A latency histogram of request latencies from the kubenurse http client.",
			Buckets:   durationHistogram,
		},
		// []string{"type"}, // TODO
		[]string{},
	)

	httpclientTraceReqDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "httpclient_trace_request_duration_seconds",
			Help:      "Latency histogram for requests from the kubenurse http client. Time in seconds since the start of the http request.",
			Buckets:   durationHistogram,
		},
		[]string{"event"},
		// []string{"event", "type"}, // TODO
	)

	registry.MustRegister(httpclientReqTotal, httpclientReqDuration, httpclientTraceReqDuration)

	collectMetric := func(traceEventType string, start time.Time, r *http.Request, err error) {
		td := time.Since(start).Seconds()
		kubenurseTypeLabel := r.Context().Value(kubenurseTypeKey{}).(string)

		// If we got an error inside a trace, log it and do not collect metrics
		if err != nil {
			log.Printf("httptrace: failed %s for %s with %v", traceEventType, kubenurseTypeLabel, err)
			return
		}

		httpclientTraceReqDuration.WithLabelValues(traceEventType).Observe(td) // TODO: add back kubenurseTypeKey
	}

	// Return a http.RoundTripper for tracing requests
	return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		// Capture request time
		start := time.Now()

		// Add tracing hooks
		trace := &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				collectMetric("got_conn", start, r, nil)
			},
			DNSStart: func(info httptrace.DNSStartInfo) {
				collectMetric("dns_start", start, r, nil)
			},
			DNSDone: func(info httptrace.DNSDoneInfo) {
				collectMetric("dns_done", start, r, info.Err)
			},
			ConnectStart: func(_, _ string) {
				collectMetric("connect_start", start, r, nil)
			},
			ConnectDone: func(_, _ string, err error) {
				collectMetric("connect_done", start, r, err)
			},
			TLSHandshakeStart: func() {
				collectMetric("tls_handshake_start", start, r, nil)
			},
			TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
				collectMetric("tls_handshake_done", start, r, nil)
			},
			WroteRequest: func(info httptrace.WroteRequestInfo) {
				collectMetric("wrote_request", start, r, info.Err)
			},
			GotFirstResponseByte: func() {
				collectMetric("got_first_resp_byte", start, r, nil)
			},
		}

		// Do request with tracing enabled
		r = r.WithContext(httptrace.WithClientTrace(r.Context(), trace))

		// // TODO: uncomment when issue #55 is solved (N^2 request will increase cardinality of path_ metrics too much otherwise)
		// typeFromCtxFn := promhttp.WithLabelFromCtx("type", func(ctx context.Context) string {
		// 	return ctx.Value(kubenurseTypeKey{}).(string)
		// })

		rt := next // variable pinning :) essential, to prevent always re-instrumenting the original variable
		rt = promhttp.InstrumentRoundTripperCounter(httpclientReqTotal, rt)
		rt = promhttp.InstrumentRoundTripperDuration(httpclientReqDuration, rt)
		return rt.RoundTrip(r)
	})
}
