package shelf

import (
	"container/list"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sync"
)

type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

type Bucket[K comparable, V any] struct {
	entries *list.List
}

type HashTable[K comparable, V any] struct {
	numShards int
	shards    []*Shard[K, V]
}

type Shard[K comparable, V any] struct {
	mu         sync.RWMutex
	numBuckets int
	count      int
	buckets    []*Bucket[K, V]
}

func newShard[K comparable, V any](numBuckets int) *Shard[K, V] {
	if numBuckets <= 0 {
		panic("number of buckets must be greater than 0")
	}
	buckets := make([]*Bucket[K, V], numBuckets)
	for i := 0; i < numBuckets; i++ {
		buckets[i] = newBucket[K, V]()
	}
	return &Shard[K, V]{
		numBuckets: numBuckets,
		count:      0,
		buckets:    buckets,
	}
}

func newBucket[K comparable, V any]() *Bucket[K, V] {
	return &Bucket[K, V]{
		entries: list.New(),
	}
}

func NewHashTable[K comparable, V any](numShards int) *HashTable[K, V] {
	if numShards <= 0 {
		panic("number of shards must be greater than 0")
	}
	shards := make([]*Shard[K, V], numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = newShard[K, V](numShards * 2)
	}
	return &HashTable[K, V]{
		numShards: numShards,
		shards:    shards,
	}
}

func (ht *HashTable[K, V]) shardIndex(key K) int {
	return int(hashKey(key) % uint64(ht.numShards))
}

func hashKey[K comparable](key K) uint64 {
	h := fnv.New64a()
	switch v := any(key).(type) {
	case string:
		h.Write([]byte(v))
	case int:
		b := [8]byte{}
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		h.Write(b[:])
	case int64:
		b := [8]byte{}
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		h.Write(b[:])
	case uint64:
		b := [8]byte{}
		binary.LittleEndian.PutUint64(b[:], v)
		h.Write(b[:])
	case int32:
		b := [4]byte{}
		binary.LittleEndian.PutUint32(b[:], uint32(v))
		h.Write(b[:])
	case uint32:
		b := [4]byte{}
		binary.LittleEndian.PutUint32(b[:], v)
		h.Write(b[:])
	case float64:
		b := [8]byte{}
		binary.LittleEndian.PutUint64(b[:], uint64(v))
		h.Write(b[:])
	case bool:
		if v {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	default:
		fmt.Fprint(h, key)
	}
	return h.Sum64()
}

func (s *Shard[K, V]) bucketIndex(key K) int {
	return int(hashKey(key) % uint64(s.numBuckets))
}

func (ht *HashTable[K, V]) Insert(key K, value V) {
	shardIdx := ht.shardIndex(key)
	shard := ht.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if float64(shard.count+1)/float64(shard.numBuckets) > 0.75 {
		ht.resize(shard)
	}

	index := shard.bucketIndex(key)
	bucket := shard.buckets[index]

	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*Entry[K, V])
		if entry.Key == key {
			entry.Value = value
			return
		}
	}

	bucket.entries.PushBack(&Entry[K, V]{Key: key, Value: value})
	shard.count++
}

func (ht *HashTable[K, V]) resize(shard *Shard[K, V]) {
	newNumBuckets := shard.numBuckets * 2
	newBuckets := make([]*Bucket[K, V], newNumBuckets)
	for i := 0; i < newNumBuckets; i++ {
		newBuckets[i] = newBucket[K, V]()
	}

	for _, bucket := range shard.buckets {
		for e := bucket.entries.Front(); e != nil; e = e.Next() {
			entry := e.Value.(*Entry[K, V])
			index := int(hashKey(entry.Key) % uint64(newNumBuckets))
			newBuckets[index].entries.PushBack(entry)
		}
	}

	shard.buckets = newBuckets
	shard.numBuckets = newNumBuckets
}

func (ht *HashTable[K, V]) Get(key K) (V, bool) {
	shardIdx := ht.shardIndex(key)
	shard := ht.shards[shardIdx]

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	index := shard.bucketIndex(key)
	bucket := shard.buckets[index]

	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*Entry[K, V])
		if entry.Key == key {
			return entry.Value, true
		}
	}

	var zero V
	return zero, false
}

func (ht *HashTable[K, V]) Delete(key K) bool {
	shardIdx := ht.shardIndex(key)
	shard := ht.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	index := shard.bucketIndex(key)
	bucket := shard.buckets[index]

	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*Entry[K, V])
		if entry.Key == key {
			bucket.entries.Remove(e)
			shard.count--
			return true
		}
	}

	return false
}

func (ht *HashTable[K, V]) Size() int {
	size := 0
	for _, shard := range ht.shards {
		shard.mu.RLock()
		size += shard.count
		shard.mu.RUnlock()
	}

	return size
}

func (ht *HashTable[K, V]) Contains(key K) bool {
	_, found := ht.Get(key)
	return found
}

func (ht *HashTable[K, V]) Keys() []K {
	keys := make([]K, 0, ht.Size())
	for _, shard := range ht.shards {
		shard.mu.RLock()
		for _, bucket := range shard.buckets {
			for e := bucket.entries.Front(); e != nil; e = e.Next() {
				entry := e.Value.(*Entry[K, V])
				keys = append(keys, entry.Key)
			}
		}
		shard.mu.RUnlock()
	}
	return keys
}

func (ht *HashTable[K, V]) Values() []V {
	values := make([]V, 0, ht.Size())
	for _, shard := range ht.shards {
		shard.mu.RLock()
		for _, bucket := range shard.buckets {
			for e := bucket.entries.Front(); e != nil; e = e.Next() {
				entry := e.Value.(*Entry[K, V])
				values = append(values, entry.Value)
			}
		}
		shard.mu.RUnlock()
	}
	return values
}

func (ht *HashTable[K, V]) Clear() {
	for _, shard := range ht.shards {
		shard.mu.Lock()
		for _, bucket := range shard.buckets {
			bucket.entries.Init()
		}
		shard.count = 0
		shard.mu.Unlock()
	}
}
