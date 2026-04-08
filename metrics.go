package shelf

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// Counter is a monotonically increasing metric value.
type Counter struct {
	value atomic.Uint64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Add increments the counter by n.
func (c *Counter) Add(n uint64) {
	c.value.Add(n)
}

// Gauge is a metric that can go up and down.
type Gauge struct {
	value atomic.Int64
}

// Set sets the gauge to v.
func (g *Gauge) Set(v int64) {
	g.value.Store(v)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.value.Add(1)
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	g.value.Add(-1)
}

// Histogram records observations into buckets.
type Histogram struct {
	mu      sync.Mutex
	buckets map[float64]uint64
	sum     float64
	count   uint64
}

// NewHistogram creates a histogram with the given bucket boundaries.
func NewHistogram(bucketBounds []float64) *Histogram {
	h := &Histogram{buckets: make(map[float64]uint64)}
	for _, b := range bucketBounds {
		h.buckets[b] = 0
	}
	return h
}

// Observe records a value into the histogram.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.count++
	for bucket := range h.buckets {
		if v <= bucket {
			h.buckets[bucket]++
		}
	}
}

// Registry holds all metrics for a service.
type Registry struct {
	counters         map[string]*Counter
	gauges           map[string]*Gauge
	histograms       map[string]*Histogram
	histogramBuckets map[string][]float64
	mu               sync.RWMutex
}

// NewRegistry creates an empty metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:         make(map[string]*Counter),
		gauges:           make(map[string]*Gauge),
		histograms:       make(map[string]*Histogram),
		histogramBuckets: make(map[string][]float64),
	}
}

// Counter returns or creates a counter with the given name.
func (r *Registry) Counter(name string) *Counter {
	r.mu.RLock()
	c, ok := r.counters[name]
	r.mu.RUnlock()
	if ok {
		return c
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c = &Counter{}
	r.counters[name] = c
	return c
}

// Gauge returns or creates a gauge with the given name.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.RLock()
	g, ok := r.gauges[name]
	r.mu.RUnlock()
	if ok {
		return g
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	g = &Gauge{}
	r.gauges[name] = g
	return g
}

// Histogram returns or creates a histogram with the given name and buckets.
func (r *Registry) Histogram(name string, buckets []float64) *Histogram {
	r.mu.RLock()
	h, ok := r.histograms[name]
	r.mu.RUnlock()
	if ok {
		return h
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h = NewHistogram(buckets)
	r.histograms[name] = h
	r.histogramBuckets[name] = buckets
	return h
}

// WriteTo renders all metrics in Prometheus text format to w.
// If store is non-nil, shelf_store_size is included.
func (r *Registry) WriteTo(w io.Writer, store *Store) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, c := range r.counters {
		fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, c.value.Load())
	}

	for name, g := range r.gauges {
		fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n", name, name, g.value.Load())
	}

	if store != nil {
		fmt.Fprintf(w, "# TYPE shelf_store_size gauge\nshelf_store_size %d\n", store.Size())
	}

	for name, h := range r.histograms {
		h.mu.Lock()
		buckets := r.histogramBuckets[name]
		sort.Float64s(buckets)
		fmt.Fprintf(w, "# TYPE %s histogram\n", name)
		for _, bucket := range buckets {
			fmt.Fprintf(w, "%s_bucket{le=\"%.3f\"} %d\n", name, bucket, h.buckets[bucket])
		}
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, h.count)
		fmt.Fprintf(w, "%s_sum %.6f\n", name, h.sum)
		fmt.Fprintf(w, "%s_count %d\n", name, h.count)
		h.mu.Unlock()
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\ngo_memstats_alloc_bytes %d\n", mem.Alloc)
	fmt.Fprintf(w, "# TYPE go_memstats_sys_bytes gauge\ngo_memstats_sys_bytes %d\n", mem.Sys)
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\ngo_goroutines %d\n", runtime.NumGoroutine())
}
