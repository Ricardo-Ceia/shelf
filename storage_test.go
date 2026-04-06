package shelf

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

//Unit Tests: Correctness Gates

func TestOpenCreatesDirectory(t *testing.T) {
	dir := t.TempDir() + "/new-store"

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("directory was not created")
	}
}

func TestOpenEmptyStore(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	if store.Size() != 0 {
		t.Fatalf("empty store Size = %d, want 0", store.Size())
	}
}

func TestSetAndGet(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	if err := store.Set("key1", []byte("value1")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := store.Set("key2", []byte("value2")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	for _, tc := range []struct {
		key   string
		want  string
		found bool
	}{
		{"key1", "value1", true},
		{"key2", "value2", true},
		{"missing", "", false},
	} {
		v, ok := store.Get(tc.key)
		if ok != tc.found {
			t.Fatalf("Get(%q) found=%v, want %v", tc.key, ok, tc.found)
		}
		if ok && string(v) != tc.want {
			t.Fatalf("Get(%q) = %q, want %q", tc.key, v, tc.want)
		}
	}
}

func TestUpdateExistingKey(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	store.Set("key", []byte("old"))
	store.Set("key", []byte("new"))

	if store.Size() != 1 {
		t.Fatalf("Size = %d, want 1 after update", store.Size())
	}

	v, ok := store.Get("key")
	if !ok || string(v) != "new" {
		t.Fatalf("Get(key) = %q, %v; want new, true", v, ok)
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	store.Set("key", []byte("value"))

	existed, err := store.Delete("key")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if !existed {
		t.Fatal("Delete returned existed=false, want true")
	}

	if store.Size() != 0 {
		t.Fatalf("Size after delete = %d, want 0", store.Size())
	}

	if _, ok := store.Get("key"); ok {
		t.Fatal("Get after delete returned true")
	}

	existed, err = store.Delete("nonexistent")
	if err != nil {
		t.Fatalf("Delete missing key error: %v", err)
	}
	if existed {
		t.Fatal("Delete missing key returned existed=true, want false")
	}
}

func TestRecoveryAfterClose(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("name", []byte("Alice"))
	store.Set("age", []byte("30"))
	store.Set("city", []byte("Berlin"))
	store.Delete("age")

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 2 {
		t.Fatalf("Recovered Size = %d, want 2", store2.Size())
	}

	for _, tc := range []struct {
		key   string
		want  string
		found bool
	}{
		{"name", "Alice", true},
		{"age", "", false},
		{"city", "Berlin", true},
	} {
		v, ok := store2.Get(tc.key)
		if ok != tc.found {
			t.Fatalf("Recovered Get(%q) found=%v, want %v", tc.key, ok, tc.found)
		}
		if ok && string(v) != tc.want {
			t.Fatalf("Recovered Get(%q) = %q, want %q", tc.key, v, tc.want)
		}
	}
}

func TestRecoveryAfterCrash(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("key1", []byte("value1"))
	store.Set("key2", []byte("value2"))

	walPath := filepath.Join(dir, walFileName)

	store.wal.Close()

	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	truncated := data[:len(data)/2]
	if err := os.WriteFile(walPath, truncated, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open after crash failed: %v", err)
	}
	defer store2.Close()

	v, ok := store2.Get("key1")
	if !ok {
		t.Fatal("key1 should have been recovered")
	}
	if string(v) != "value1" {
		t.Fatalf("key1 = %q, want value1", v)
	}
}

func TestCorruptedWAL(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("good", []byte("data"))

	walPath := filepath.Join(dir, walFileName)

	store.wal.Close()

	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	corrupted[len(corrupted)-1] ^= 0xFF

	if err := os.WriteFile(walPath, corrupted, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open after corruption failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 0 {
		t.Fatalf("Recovered Size = %d, want 0 (entry was corrupted)", store2.Size())
	}
}

func TestPartialCorruptionKeepsValidEntries(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("first", []byte("data1"))
	store.Set("second", []byte("data2"))
	store.Set("third", []byte("data3"))

	walPath := filepath.Join(dir, walFileName)

	store.wal.Close()

	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	corrupted := make([]byte, len(data))
	copy(corrupted, data)

	entryStart := 0
	for i := 0; i < 2; i++ {
		keyLen := int(binary.BigEndian.Uint32(data[entryStart+1:]))
		valLen := int(binary.BigEndian.Uint32(data[entryStart+5+keyLen:]))
		entryStart += 1 + 4 + keyLen + 4 + valLen + 4
	}

	if entryStart < len(data) {
		corrupted[entryStart] ^= 0xFF
	}

	if err := os.WriteFile(walPath, corrupted, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open after partial corruption failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 2 {
		t.Fatalf("Recovered Size = %d, want 2", store2.Size())
	}

	for _, tc := range []struct {
		key   string
		want  string
		found bool
	}{
		{"first", "data1", true},
		{"second", "data2", true},
		{"third", "", false},
	} {
		v, ok := store2.Get(tc.key)
		if ok != tc.found {
			t.Fatalf("Recovered Get(%q) found=%v, want %v", tc.key, ok, tc.found)
		}
		if ok && string(v) != tc.want {
			t.Fatalf("Recovered Get(%q) = %q, want %q", tc.key, v, tc.want)
		}
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	const goroutines = 50
	const writesPerGoroutine = 200

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				key := fmt.Sprintf("k-%d-%d", id, i)
				if err := store.Set(key, []byte("v")); err != nil {
					t.Errorf("Set failed: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()

	if store.Size() != goroutines*writesPerGoroutine {
		t.Fatalf("Size = %d, want %d", store.Size(), goroutines*writesPerGoroutine)
	}
}

func TestConcurrentReadWriteDelete(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	const goroutines = 100
	const opsPerGoroutine = 500

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("k-%d", i%100)
				switch i % 4 {
				case 0:
					store.Set(key, []byte(fmt.Sprintf("v-%d-%d", id, i)))
				case 1:
					store.Get(key)
				case 2:
					store.Delete(key)
				case 3:
					store.Size()
				}
			}
		}(g)
	}

	wg.Wait()
}

func TestCloseSyncs(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("key", []byte("value"))

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	defer store2.Close()

	v, ok := store2.Get("key")
	if !ok || string(v) != "value" {
		t.Fatalf("After Close+Reopen, Get(key) = %q, %v; want value, true", v, ok)
	}
}

func TestEmptyValue(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	store.Set("empty", []byte{})

	v, ok := store.Get("empty")
	if !ok {
		t.Fatal("Get(empty) = false, want true")
	}
	if len(v) != 0 {
		t.Fatalf("Get(empty) = %q, want empty slice", v)
	}
}

func TestBinaryValues(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	binary := []byte{0x00, 0x01, 0xFF, 0xFE, 0x80}

	store.Set("binary", binary)

	v, ok := store.Get("binary")
	if !ok {
		t.Fatal("Get(binary) = false, want true")
	}
	if string(v) != string(binary) {
		t.Fatalf("Get(binary) = %v, want %v", v, binary)
	}
}

func TestLargeValues(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	large := make([]byte, 1<<20)
	for i := range large {
		large[i] = byte(i % 256)
	}

	store.Set("large", large)

	v, ok := store.Get("large")
	if !ok {
		t.Fatal("Get(large) = false, want true")
	}
	if len(v) != len(large) {
		t.Fatalf("Get(large) len = %d, want %d", len(v), len(large))
	}
	for i := range large {
		if v[i] != large[i] {
			t.Fatalf("Get(large)[%d] = %d, want %d", i, v[i], large[i])
		}
	}
}

func TestManyKeysRecovery(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	const n = 10000
	for i := 0; i < n; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte(fmt.Sprintf("value-%d", i)))
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != n {
		t.Fatalf("Recovered Size = %d, want %d", store2.Size(), n)
	}

	for i := 0; i < n; i++ {
		v, ok := store2.Get(fmt.Sprintf("key-%d", i))
		if !ok {
			t.Fatalf("Get(key-%d) = false, want true", i)
		}
		if string(v) != fmt.Sprintf("value-%d", i) {
			t.Fatalf("Get(key-%d) = %q, want value-%d", i, v, i)
		}
	}
}

func TestWALEntryFormat(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("test", []byte("data"))

	walPath := filepath.Join(dir, walFileName)

	store.wal.Close()

	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(data) < 1+4+4+4+4+4 {
		t.Fatalf("WAL too short: %d bytes", len(data))
	}

	if data[0] != byte(opSet) {
		t.Fatalf("op = 0x%02x, want 0x%02x", data[0], opSet)
	}

	keyLen := binary.BigEndian.Uint32(data[1:5])
	if keyLen != 4 {
		t.Fatalf("keyLen = %d, want 4", keyLen)
	}

	key := data[5 : 5+keyLen]
	if string(key) != "test" {
		t.Fatalf("key = %q, want test", key)
	}

	valLen := binary.BigEndian.Uint32(data[5+keyLen : 9+keyLen])
	if valLen != 4 {
		t.Fatalf("valLen = %d, want 4", valLen)
	}

	value := data[9+keyLen : 9+keyLen+valLen]
	if string(value) != "data" {
		t.Fatalf("value = %q, want data", value)
	}

	storedCRC := binary.BigEndian.Uint32(data[9+keyLen+valLen:])
	computedCRC := crc32.ChecksumIEEE(data[:9+keyLen+valLen])
	if storedCRC != computedCRC {
		t.Fatalf("CRC mismatch: stored=0x%08x, computed=0x%08x", storedCRC, computedCRC)
	}
}

func TestDeleteWALEntryFormat(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("key", []byte("value"))
	store.Delete("key")

	walPath := filepath.Join(dir, walFileName)

	store.wal.Close()

	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if data[0] != byte(opSet) {
		t.Fatalf("first op = 0x%02x, want opSet", data[0])
	}

	keyLen := binary.BigEndian.Uint32(data[1:5])
	valLen := binary.BigEndian.Uint32(data[5+keyLen : 9+keyLen])
	setEntryLen := 1 + 4 + keyLen + 4 + valLen + 4

	if data[setEntryLen] != byte(opDel) {
		t.Fatalf("second op = 0x%02x, want opDel", data[setEntryLen])
	}

	delKeyLen := binary.BigEndian.Uint32(data[setEntryLen+1 : setEntryLen+5])
	if delKeyLen != keyLen {
		t.Fatalf("del keyLen = %d, want %d", delKeyLen, keyLen)
	}

	delValLen := binary.BigEndian.Uint32(data[setEntryLen+5+delKeyLen : setEntryLen+9+delKeyLen])
	if delValLen != 0 {
		t.Fatalf("del valLen = %d, want 0", delValLen)
	}

	delCRCStart := setEntryLen + 9 + delKeyLen
	storedCRC := binary.BigEndian.Uint32(data[delCRCStart:])
	computedCRC := crc32.ChecksumIEEE(data[setEntryLen:delCRCStart])
	if storedCRC != computedCRC {
		t.Fatalf("Delete CRC mismatch: stored=0x%08x, computed=0x%08x", storedCRC, computedCRC)
	}
}

func TestRecoveryWithMixedOps(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	store.Set("a", []byte("1"))
	store.Set("b", []byte("2"))
	store.Set("c", []byte("3"))
	store.Delete("b")
	store.Set("a", []byte("updated"))
	store.Set("d", []byte("4"))
	store.Delete("c")

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	store2, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	defer store2.Close()

	if store2.Size() != 2 {
		t.Fatalf("Size = %d, want 2", store2.Size())
	}

	for _, tc := range []struct {
		key   string
		want  string
		found bool
	}{
		{"a", "updated", true},
		{"b", "", false},
		{"c", "", false},
		{"d", "4", true},
	} {
		v, ok := store2.Get(tc.key)
		if ok != tc.found {
			t.Fatalf("Get(%q) found=%v, want %v", tc.key, ok, tc.found)
		}
		if ok && string(v) != tc.want {
			t.Fatalf("Get(%q) = %q, want %q", tc.key, v, tc.want)
		}
	}
}

func TestKeys(t *testing.T) {
	dir := t.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	store.Set("a", []byte("1"))
	store.Set("b", []byte("2"))
	store.Set("c", []byte("3"))

	keys := store.Keys()
	if len(keys) != 3 {
		t.Fatalf("Keys() = %d items, want 3", len(keys))
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

// --- Benchmarks: Performance Regression Gates ---

func BenchmarkStoreSet(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
	}
}

func BenchmarkStoreGet(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	for i := 0; i < 10000; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get(fmt.Sprintf("key-%d", i%10000))
	}
}

func BenchmarkStoreDelete(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	for i := 0; i < 10000; i++ {
		store.Set(fmt.Sprintf("del-%d", i), []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Set(fmt.Sprintf("del-%d", 10000+i), []byte("value"))
		store.Delete(fmt.Sprintf("del-%d", 10000+i))
	}
}

func BenchmarkStoreSetGet(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
		store.Get(fmt.Sprintf("key-%d", i))
	}
}

func BenchmarkStoreSetConcurrent(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
			i++
		}
	})
}

func BenchmarkStoreGetConcurrent(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	for i := 0; i < 100000; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			store.Get(fmt.Sprintf("key-%d", i%100000))
			i++
		}
	})
}

func BenchmarkStoreMixed(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	for i := 0; i < 100000; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%5 == 0 {
				store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
			} else {
				store.Get(fmt.Sprintf("key-%d", i%100000))
			}
			i++
		}
	})
}

func BenchmarkStoreOpenReplay(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}

	for i := 0; i < 10000; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
	}
	store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir, 16)
		if err != nil {
			b.Fatalf("Open failed: %v", err)
		}
		s.Close()
	}
}

func BenchmarkStoreOpenReplay100K(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}

	for i := 0; i < 100000; i++ {
		store.Set(fmt.Sprintf("key-%d", i), []byte("value"))
	}
	store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir, 16)
		if err != nil {
			b.Fatalf("Open failed: %v", err)
		}
		s.Close()
	}
}

func BenchmarkStoreLargeValue(b *testing.B) {
	dir := b.TempDir()

	store, err := Open(dir, 16)
	if err != nil {
		b.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	value := make([]byte, 1<<16)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Set(fmt.Sprintf("key-%d", i), value)
	}
}
