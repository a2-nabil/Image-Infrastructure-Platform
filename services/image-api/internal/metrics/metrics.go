package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry is a minimal Prometheus-compatible in-process metrics store.
type Registry struct {
	mu sync.Mutex

	httpRequests map[string]*atomic.Int64
	httpLatency  map[string]*latencyAgg

	uploadsTotal       atomic.Int64
	uploadsDeduped     atomic.Int64
	variantJobsOK      atomic.Int64
	variantJobsFail    atomic.Int64
	guestPurgesTotal   atomic.Int64
	presignIssuedTotal atomic.Int64
}

type latencyAgg struct {
	count atomic.Int64
	sumNs atomic.Int64
}

// Default is the process-wide registry.
var Default = New()

func New() *Registry {
	return &Registry{
		httpRequests: make(map[string]*atomic.Int64),
		httpLatency:  make(map[string]*latencyAgg),
	}
}

func (r *Registry) ObserveHTTP(method, route string, status int, d time.Duration) {
	if r == nil {
		return
	}
	key := fmt.Sprintf("%s|%s|%d", method, normalizeRoute(route), status)
	r.mu.Lock()
	counter, ok := r.httpRequests[key]
	if !ok {
		counter = &atomic.Int64{}
		r.httpRequests[key] = counter
	}
	lat, ok := r.httpLatency[key]
	if !ok {
		lat = &latencyAgg{}
		r.httpLatency[key] = lat
	}
	r.mu.Unlock()

	counter.Add(1)
	lat.count.Add(1)
	lat.sumNs.Add(d.Nanoseconds())
}

func (r *Registry) IncUpload(deduplicated bool) {
	if r == nil {
		return
	}
	r.uploadsTotal.Add(1)
	if deduplicated {
		r.uploadsDeduped.Add(1)
	}
}

func (r *Registry) IncVariantJob(ok bool) {
	if r == nil {
		return
	}
	if ok {
		r.variantJobsOK.Add(1)
		return
	}
	r.variantJobsFail.Add(1)
}

func (r *Registry) AddGuestPurges(n int) {
	if r == nil || n <= 0 {
		return
	}
	r.guestPurgesTotal.Add(int64(n))
}

func (r *Registry) IncPresignIssued() {
	if r == nil {
		return
	}
	r.presignIssuedTotal.Add(1)
}

// Handler exposes Prometheus text format on GET /metrics.
func Handler(w http.ResponseWriter, _ *http.Request) {
	Default.WritePrometheus(w)
}

func (r *Registry) WritePrometheus(w http.ResponseWriter) {
	if r == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	var b strings.Builder
	b.WriteString("# HELP image_api_uploads_total Total image uploads processed.\n")
	b.WriteString("# TYPE image_api_uploads_total counter\n")
	fmt.Fprintf(&b, "image_api_uploads_total %d\n", r.uploadsTotal.Load())

	b.WriteString("# HELP image_api_uploads_deduplicated_total Uploads served from fingerprint cache.\n")
	b.WriteString("# TYPE image_api_uploads_deduplicated_total counter\n")
	fmt.Fprintf(&b, "image_api_uploads_deduplicated_total %d\n", r.uploadsDeduped.Load())

	b.WriteString("# HELP image_api_variant_jobs_total Async variant jobs by outcome.\n")
	b.WriteString("# TYPE image_api_variant_jobs_total counter\n")
	fmt.Fprintf(&b, "image_api_variant_jobs_total{result=\"ok\"} %d\n", r.variantJobsOK.Load())
	fmt.Fprintf(&b, "image_api_variant_jobs_total{result=\"error\"} %d\n", r.variantJobsFail.Load())

	b.WriteString("# HELP image_api_guest_purges_total Expired guest images purged.\n")
	b.WriteString("# TYPE image_api_guest_purges_total counter\n")
	fmt.Fprintf(&b, "image_api_guest_purges_total %d\n", r.guestPurgesTotal.Load())

	b.WriteString("# HELP image_api_presign_issued_total Presigned URLs issued.\n")
	b.WriteString("# TYPE image_api_presign_issued_total counter\n")
	fmt.Fprintf(&b, "image_api_presign_issued_total %d\n", r.presignIssuedTotal.Load())

	r.mu.Lock()
	keys := make([]string, 0, len(r.httpRequests))
	for k := range r.httpRequests {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString("# HELP image_api_http_requests_total HTTP requests by method, route, and status.\n")
	b.WriteString("# TYPE image_api_http_requests_total counter\n")
	for _, key := range keys {
		method, route, status := splitHTTPKey(key)
		fmt.Fprintf(&b, "image_api_http_requests_total{method=%q,route=%q,status=%q} %d\n",
			method, route, status, r.httpRequests[key].Load())
	}

	b.WriteString("# HELP image_api_http_request_duration_seconds_sum Cumulative request latency.\n")
	b.WriteString("# TYPE image_api_http_request_duration_seconds_sum counter\n")
	b.WriteString("# HELP image_api_http_request_duration_seconds_count Request latency sample count.\n")
	b.WriteString("# TYPE image_api_http_request_duration_seconds_count counter\n")
	for _, key := range keys {
		lat := r.httpLatency[key]
		if lat == nil {
			continue
		}
		method, route, status := splitHTTPKey(key)
		count := lat.count.Load()
		sumSec := float64(lat.sumNs.Load()) / 1e9
		fmt.Fprintf(&b, "image_api_http_request_duration_seconds_sum{method=%q,route=%q,status=%q} %.9f\n",
			method, route, status, sumSec)
		fmt.Fprintf(&b, "image_api_http_request_duration_seconds_count{method=%q,route=%q,status=%q} %d\n",
			method, route, status, count)
	}
	r.mu.Unlock()

	_, _ = w.Write([]byte(b.String()))
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return "unknown"
	}
	// Collapse UUID path segments for cardinality control.
	parts := strings.Split(route, "/")
	for i, p := range parts {
		if looksLikeUUID(p) {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func splitHTTPKey(key string) (method, route, status string) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) != 3 {
		return "UNKNOWN", "unknown", "0"
	}
	return parts[0], parts[1], parts[2]
}
