package shelf

import (
	"fmt"
	"sync"
	"testing"
)

//Unit Tests: Correctness Gates

func TestInsertAndGet(t *testing.T) {
	ht := NewHashTable[string, int](16)

	ht.Insert("a", 1)
	ht.Insert("b", 2)
	ht.Insert("c", 3)

	for _, tc := range []struct {
		key   string
		want  int
		found bool
	}{
		{"a", 1, true},
		{"b", 2, true},
		{"c", 3, true},
		{"d", 0, false},
	} {
		v, ok := ht.Get(tc.key)
		if ok != tc.found {
			t.Fatalf("Get(%q) found=%v, want %v", tc.key, ok, tc.found)
		}
		if ok && v != tc.want {
			t.Fatalf("Get(%q) = %v, want %v", tc.key, v, tc.want)
		}
	}
}

func TestUpdateExisting(t *testing.T) {
	ht := NewHashTable[string, int](16)

	ht.Insert("key", 1)
	ht.Insert("key", 2)

	if ht.Size() != 1 {
		t.Fatalf("Size = %d, want 1 after update", ht.Size())
	}

	v, ok := ht.Get("key")
	if !ok || v != 2 {
		t.Fatalf("Get(key) = %v, %v; want 2, true", v, ok)
	}
}

func TestGetMissing(t *testing.T) {
	ht := NewHashTable[string, int](16)
	_, ok := ht.Get("nonexistent")
	if ok {
		t.Fatal("Get on missing key returned true")
	}
}

func TestDeleteExisting(t *testing.T) {
	ht := NewHashTable[string, int](16)
	ht.Insert("key", 42)

	if !ht.Delete("key") {
		t.Fatal("Delete existing key returned false")
	}

	if ht.Size() != 0 {
		t.Fatalf("Size = %d after delete, want 0", ht.Size())
	}

	if _, ok := ht.Get("key"); ok {
		t.Fatal("Get after delete returned true")
	}
}

func TestDeleteMissing(t *testing.T) {
	ht := NewHashTable[string, int](16)
	if ht.Delete("nonexistent") {
		t.Fatal("Delete missing key returned true")
	}
}

func TestSize(t *testing.T) {
	ht := NewHashTable[string, int](16)

	if ht.Size() != 0 {
		t.Fatalf("empty Size = %d, want 0", ht.Size())
	}

	ht.Insert("a", 1)
	ht.Insert("b", 2)
	if ht.Size() != 2 {
		t.Fatalf("Size after 2 inserts = %d, want 2", ht.Size())
	}

	ht.Delete("a")
	if ht.Size() != 1 {
		t.Fatalf("Size after 1 delete = %d, want 1", ht.Size())
	}
}

func TestContains(t *testing.T) {
	ht := NewHashTable[string, int](16)
	ht.Insert("key", 1)

	if !ht.Contains("key") {
		t.Fatal("Contains(key) = false, want true")
	}
	if ht.Contains("missing") {
		t.Fatal("Contains(missing) = true, want false")
	}
}

func TestKeysAndValues(t *testing.T) {
	ht := NewHashTable[string, int](16)
	ht.Insert("a", 1)
	ht.Insert("b", 2)
	ht.Insert("c", 3)

	keys := ht.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys() returned %d items, want 3", len(keys))
	}

	vals := ht.Values()
	if len(vals) != 3 {
		t.Fatalf("Values() returned %d items, want 3", len(vals))
	}

	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !keySet[want] {
			t.Fatalf("Keys() missing %q", want)
		}
	}
}

func TestClear(t *testing.T) {
	ht := NewHashTable[string, int](16)
	ht.Insert("a", 1)
	ht.Insert("b", 2)
	ht.Insert("c", 3)

	ht.Clear()

	if ht.Size() != 0 {
		t.Fatalf("Size after Clear = %d, want 0", ht.Size())
	}

	if len(ht.Keys()) != 0 {
		t.Fatalf("Keys() after Clear returned %d items, want 0", len(ht.Keys()))
	}

	// Verify we can insert after clear
	ht.Insert("d", 4)
	v, ok := ht.Get("d")
	if !ok || v != 4 {
		t.Fatalf("Insert after Clear failed: Get(d) = %v, %v", v, ok)
	}
}

func TestCollisionHandling(t *testing.T) {
	ht := NewHashTable[int, int](2)

	for i := 0; i < 100; i++ {
		ht.Insert(i, i*10)
	}

	if ht.Size() != 100 {
		t.Fatalf("Size = %d, want 100", ht.Size())
	}

	for i := 0; i < 100; i++ {
		v, ok := ht.Get(i)
		if !ok || v != i*10 {
			t.Fatalf("Get(%d) = %v, %v; want %d, true", i, v, ok, i*10)
		}
	}
}

func TestResizeSurvival(t *testing.T) {
	ht := NewHashTable[int, int](4)

	const n = 10000
	for i := 0; i < n; i++ {
		ht.Insert(i, i*10)
	}

	if ht.Size() != n {
		t.Fatalf("Size = %d, want %d", ht.Size(), n)
	}

	for i := 0; i < n; i++ {
		v, ok := ht.Get(i)
		if !ok || v != i*10 {
			t.Fatalf("Get(%d) = %v, %v; want %d, true", i, v, ok, i*10)
		}
	}

	for i := 0; i < n; i++ {
		if !ht.Delete(i) {
			t.Fatalf("Delete(%d) returned false", i)
		}
	}

	if ht.Size() != 0 {
		t.Fatalf("Size after deleting all = %d, want 0", ht.Size())
	}
}

func TestPropertyInsertN(t *testing.T) {
	sizes := []int{1, 100, 10000, 100000}

	for _, n := range sizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			ht := NewHashTable[string, int](16)

			for i := 0; i < n; i++ {
				ht.Insert(fmt.Sprintf("key-%d", i), i)
			}

			if ht.Size() != n {
				t.Fatalf("Size = %d, want %d", ht.Size(), n)
			}

			for i := 0; i < n; i++ {
				v, ok := ht.Get(fmt.Sprintf("key-%d", i))
				if !ok || v != i {
					t.Fatalf("Get(key-%d) = %v, %v; want %d, true", i, v, ok, i)
				}
			}

			keys := ht.Keys()
			if len(keys) != n {
				t.Fatalf("Keys() = %d items, want %d", len(keys), n)
			}
		})
	}
}

// --- Concurrency Tests: Race Detector Gates ---

func TestConcurrentReadWrite(t *testing.T) {
	ht := NewHashTable[string, int](16)

	const goroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("k-%d-%d", id, i)
				ht.Insert(key, i)
				ht.Get(key)
				if i%2 == 0 {
					ht.Delete(key)
				}
			}
		}(g)
	}

	wg.Wait()
}

func TestConcurrentResize(t *testing.T) {
	ht := NewHashTable[int, int](4)

	var wg sync.WaitGroup

	const writers = 50
	const writesPerWriter = 5000

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				ht.Insert(id*writesPerWriter+i, i)
			}
		}(w)
	}

	const readers = 50
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < writesPerWriter*writers; i++ {
				ht.Get(i)
				ht.Size()
				ht.Contains(i)
			}
		}()
	}

	wg.Wait()

	expected := writers * writesPerWriter
	if ht.Size() != expected {
		t.Fatalf("Size = %d, want %d", ht.Size(), expected)
	}
}

func TestConcurrentMixedOps(t *testing.T) {
	ht := NewHashTable[string, string](16)

	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				key := fmt.Sprintf("key-%d", j%100)
				switch j % 4 {
				case 0:
					ht.Insert(key, fmt.Sprintf("val-%d-%d", id, j))
				case 1:
					ht.Get(key)
				case 2:
					ht.Keys()
				case 3:
					ht.Values()
				}
			}
		}(i)
	}

	wg.Wait()
}

// --- Hash Distribution Test ---

func TestHashDistribution(t *testing.T) {
	ht := NewHashTable[string, int](16)

	const n = 10000
	for i := 0; i < n; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}

	totalBuckets := 0
	var maxShardCount int
	for _, shard := range ht.shards {
		shard.mu.RLock()
		totalBuckets += shard.numBuckets
		if shard.count > maxShardCount {
			maxShardCount = shard.count
		}
		shard.mu.RUnlock()
	}

	avgPerShard := float64(n) / float64(ht.numShards)
	maxAllowed := avgPerShard * 3.0

	if float64(maxShardCount) > maxAllowed {
		t.Errorf("max shard count %d exceeds 3x average %.0f (n=%d, shards=%d) — hash distribution is poor, consider changing hash function",
			maxShardCount, avgPerShard, n, ht.numShards)
	}

	if totalBuckets < n {
		t.Logf("total buckets %d for %d entries — resize may be needed for optimal performance", totalBuckets, n)
	}
}

// --- Benchmarks: Performance Regression Gates ---

func BenchmarkInsert(b *testing.B) {
	ht := NewHashTable[string, int](16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
}

func BenchmarkGet(b *testing.B) {
	ht := NewHashTable[string, int](16)
	for i := 0; i < 10000; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ht.Get(fmt.Sprintf("key-%d", i%10000))
	}
}

func BenchmarkDelete(b *testing.B) {
	ht := NewHashTable[string, int](16)
	for i := 0; i < 100000; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ht.Insert(fmt.Sprintf("del-%d", i), i)
		ht.Delete(fmt.Sprintf("del-%d", i))
	}
}

func BenchmarkInsertConcurrent(b *testing.B) {
	ht := NewHashTable[string, int](16)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ht.Insert(fmt.Sprintf("key-%d", i), i)
			i++
		}
	})
}

func BenchmarkGetConcurrent(b *testing.B) {
	ht := NewHashTable[string, int](16)
	for i := 0; i < 100000; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ht.Get(fmt.Sprintf("key-%d", i%100000))
			i++
		}
	})
}

func BenchmarkMixed(b *testing.B) {
	ht := NewHashTable[string, int](16)
	for i := 0; i < 100000; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%5 == 0 {
				ht.Insert(fmt.Sprintf("key-%d", i), i)
			} else {
				ht.Get(fmt.Sprintf("key-%d", i%100000))
			}
			i++
		}
	})
}

func BenchmarkInsert10K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ht := NewHashTable[string, int](16)
		b.StartTimer()
		for j := 0; j < 10000; j++ {
			ht.Insert(fmt.Sprintf("key-%d", j), j)
		}
	}
}

func BenchmarkGet10K(b *testing.B) {
	ht := NewHashTable[string, int](16)
	for i := 0; i < 10000; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ht.Clear()
		for j := 0; j < 10000; j++ {
			ht.Insert(fmt.Sprintf("key-%d", j), j)
		}
		b.StartTimer()
		for j := 0; j < 10000; j++ {
			ht.Get(fmt.Sprintf("key-%d", j))
		}
	}
}

func BenchmarkDelete10K(b *testing.B) {
	ht := NewHashTable[string, int](16)
	for i := 0; i < 10000; i++ {
		ht.Insert(fmt.Sprintf("key-%d", i), i)
	}
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ht.Clear()
		for j := 0; j < 10000; j++ {
			ht.Insert(fmt.Sprintf("key-%d", j), j)
		}
		b.StartTimer()
		for j := 0; j < 10000; j++ {
			ht.Delete(fmt.Sprintf("key-%d", j))
		}
	}
}

func BenchmarkConcurrentThroughput(b *testing.B) {
	for _, shards := range []int{4, 8, 16, 32, 64} {
		b.Run(fmt.Sprintf("shards=%d", shards), func(b *testing.B) {
			ht := NewHashTable[string, int](shards)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					ht.Insert(fmt.Sprintf("key-%d", i), i)
					ht.Get(fmt.Sprintf("key-%d", i))
					i++
				}
			})
		})
	}
}
