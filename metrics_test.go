package shelf

import (
	"bytes"
	"strings"
	"testing"
)

func TestCounter(t *testing.T) {
	c := &Counter{}
	c.Inc()
	c.Inc()
	c.Add(5)

	if c.value.Load() != 7 {
		t.Fatalf("counter = %d, want 7", c.value.Load())
	}
}

func TestGauge(t *testing.T) {
	g := &Gauge{}
	g.Set(10)
	g.Inc()
	g.Inc()
	g.Dec()

	if g.value.Load() != 11 {
		t.Fatalf("gauge = %d, want 11", g.value.Load())
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram([]float64{0.1, 0.5, 1.0})

	h.Observe(0.05)
	h.Observe(0.3)
	h.Observe(0.8)
	h.Observe(2.0)

	if h.count != 4 {
		t.Fatalf("count = %d, want 4", h.count)
	}

	if h.sum != 3.15 {
		t.Fatalf("sum = %.2f, want 3.15", h.sum)
	}

	if h.buckets[0.1] != 1 {
		t.Fatalf("bucket[0.1] = %d, want 1", h.buckets[0.1])
	}
	if h.buckets[0.5] != 2 {
		t.Fatalf("bucket[0.5] = %d, want 2", h.buckets[0.5])
	}
	if h.buckets[1.0] != 3 {
		t.Fatalf("bucket[1.0] = %d, want 3", h.buckets[1.0])
	}
}

func TestRegistryCounter(t *testing.T) {
	r := NewRegistry()
	c1 := r.Counter("requests")
	c2 := r.Counter("requests")

	if c1 != c2 {
		t.Fatal("Counter() returned different instances for same name")
	}

	c1.Inc()
	c1.Inc()

	if c2.value.Load() != 2 {
		t.Fatalf("counter = %d, want 2", c2.value.Load())
	}
}

func TestRegistryGauge(t *testing.T) {
	r := NewRegistry()
	g1 := r.Gauge("size")
	g2 := r.Gauge("size")

	if g1 != g2 {
		t.Fatal("Gauge() returned different instances for same name")
	}

	g1.Set(42)

	if g2.value.Load() != 42 {
		t.Fatalf("gauge = %d, want 42", g2.value.Load())
	}
}

func TestRegistryHistogram(t *testing.T) {
	r := NewRegistry()
	buckets := []float64{0.1, 0.5, 1.0}
	h1 := r.Histogram("latency", buckets)
	h2 := r.Histogram("latency", buckets)

	if h1 != h2 {
		t.Fatal("Histogram() returned different instances for same name")
	}

	h1.Observe(0.3)

	if h2.count != 1 {
		t.Fatalf("histogram count = %d, want 1", h2.count)
	}
}

func TestRegistryWriteTo(t *testing.T) {
	r := NewRegistry()
	r.Counter("requests").Add(5)
	r.Gauge("size").Set(100)
	h := r.Histogram("latency", []float64{0.1, 0.5, 1.0})
	h.Observe(0.05)
	h.Observe(0.3)

	var buf bytes.Buffer
	r.WriteTo(&buf, nil)

	output := buf.String()

	if !strings.Contains(output, "# TYPE requests counter") {
		t.Fatal("missing counter type declaration")
	}
	if !strings.Contains(output, "requests 5") {
		t.Fatal("missing counter value")
	}
	if !strings.Contains(output, "# TYPE size gauge") {
		t.Fatal("missing gauge type declaration")
	}
	if !strings.Contains(output, "size 100") {
		t.Fatal("missing gauge value")
	}
	if !strings.Contains(output, "# TYPE latency histogram") {
		t.Fatal("missing histogram type declaration")
	}
	if !strings.Contains(output, `latency_bucket{le="0.100"} 1`) {
		t.Fatalf("missing histogram bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `latency_bucket{le="0.500"} 2`) {
		t.Fatalf("missing histogram bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `latency_bucket{le="1.000"} 2`) {
		t.Fatalf("missing histogram bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `latency_bucket{le="+Inf"} 2`) {
		t.Fatalf("missing histogram +Inf bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `latency_bucket{le="0.500"} 2`) {
		t.Fatalf("missing histogram bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `latency_bucket{le="1.000"} 2`) {
		t.Fatalf("missing histogram bucket, got:\n%s", output)
	}
	if !strings.Contains(output, `latency_bucket{le="+Inf"} 2`) {
		t.Fatalf("missing histogram +Inf bucket, got:\n%s", output)
	}
	if !strings.Contains(output, "latency_sum 0.350000") {
		t.Fatalf("missing histogram sum, got:\n%s", output)
	}
	if !strings.Contains(output, "latency_count 2") {
		t.Fatalf("missing histogram count, got:\n%s", output)
	}
}

func TestRegistryWriteToWithStore(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, 16, 0)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	store.Set("a", []byte("1"))
	store.Set("b", []byte("2"))

	r := NewRegistry()
	var buf bytes.Buffer
	r.WriteTo(&buf, store)

	output := buf.String()

	if !strings.Contains(output, "# TYPE shelf_store_size gauge") {
		t.Fatal("missing shelf_store_size type declaration")
	}
	if !strings.Contains(output, "shelf_store_size 2") {
		t.Fatal("missing shelf_store_size value")
	}
}

func TestRegistryWriteToEmpty(t *testing.T) {
	r := NewRegistry()
	var buf bytes.Buffer
	r.WriteTo(&buf, nil)

	if buf.Len() != 0 {
		t.Fatalf("empty registry produced output: %q", buf.String())
	}
}
