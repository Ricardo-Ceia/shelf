package main

import (
	"container/list"
	"fmt"
	"hash/fnv"
)

type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

type Bucket[K comparable, V any] struct {
	entries *list.List
}

type HashTable[K comparable, V any] struct {
	buckets    []*Bucket[K, V]
	numBuckets int
	count      int
}

func NewBucket[K comparable, V any]() *Bucket[K, V] {
	return &Bucket[K, V]{
		entries: list.New(),
	}
}

func NewHashTable[K comparable, V any](numBuckets int) *HashTable[K, V] {
	if numBuckets <= 0 {
		panic("number of buckets must be greater than 0")
	}
	buckets := make([]*Bucket[K, V], numBuckets)
	for i := 0; i < numBuckets; i++ {
		buckets[i] = NewBucket[K, V]()
	}
	return &HashTable[K, V]{
		buckets:    buckets,
		numBuckets: numBuckets,
		count:      0,
	}
}

func hashKey[K comparable](key K) uint64 {
	h := fnv.New64a()
	fmt.Fprint(h, key)
	return h.Sum64()
}

func (ht *HashTable[K, V]) bucketIndex(key K) int {
	return int(hashKey(key) % uint64(ht.numBuckets))
}

func (ht *HashTable[K, V]) Insert(key K, value V) {
	index := ht.bucketIndex(key)
	bucket := ht.buckets[index]

	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*Entry[K, V])
		if entry.Key == key {
			entry.Value = value
			return
		}
	}

	bucket.entries.PushBack(&Entry[K, V]{
		Key:   key,
		Value: value,
	})
	ht.count++
}

func (ht *HashTable[K, V]) Get(key K) (V, bool) {
	index := ht.bucketIndex(key)
	bucket := ht.buckets[index]

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
	index := ht.bucketIndex(key)
	bucket := ht.buckets[index]

	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*Entry[K, V])
		if entry.Key == key {
			bucket.entries.Remove(e)
			ht.count--
			return true
		}
	}

	return false
}

func (ht *HashTable[K, V]) Size() int {
	return ht.count
}

func (ht *HashTable[K, V]) Contains(key K) bool {
	_, found := ht.Get(key)
	return found
}

func (ht *HashTable[K, V]) Keys() []K {
	keys := make([]K, 0, ht.count)
	for _, bucket := range ht.buckets {
		for e := bucket.entries.Front(); e != nil; e = e.Next() {
			entry := e.Value.(*Entry[K, V])
			keys = append(keys, entry.Key)
		}
	}
	return keys
}

func (ht *HashTable[K, V]) Values() []V {
	values := make([]V, 0, ht.count)
	for _, bucket := range ht.buckets {
		for e := bucket.entries.Front(); e != nil; e = e.Next() {
			entry := e.Value.(*Entry[K, V])
			values = append(values, entry.Value)
		}
	}
	return values
}

func (ht *HashTable[K, V]) Clear() {
	for _, bucket := range ht.buckets {
		bucket.entries = list.New()
	}
	ht.count = 0
}

func main() {
	ht := NewHashTable[string, string](10)
	ht.Insert("name", "Alice")
	ht.Insert("city", "New York")
	ht.Insert("lang", "Go")

	if value, found := ht.Get("name"); found {
		println("name:", value)
	} else {
		println("name not found")
	}

	if ht.Delete("city") {
		println("city deleted")
	} else {
		println("city not found")
	}

	println("size:", ht.Size())
	println("contains lang:", ht.Contains("lang"))

	keys := ht.Keys()
	print("keys: ")
	for i, k := range keys {
		if i > 0 {
			print(", ")
		}
		print(k)
	}
	println()

	ht.Clear()
	println("after clear, size:", ht.Size())
}
