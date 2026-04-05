package main

import (
	"container/list"
)

type  Bucket struct{
	bucketIndex int
	entries *list.List 
}

type HashTable struct {
	buckets []*Bucket
	numBuckets int
}

type HashTableEntry struct {
	key string
	//the value can be of any type, so we use an empty interface
	value interface{}
}

func NewHashTable(numBuckets int) *HashTable {
	buckets := make([]*Bucket, numBuckets)
	for i := 0; i < numBuckets; i++ {
		buckets[i] = NewBucket(i)
	}
	return &HashTable{
		buckets: buckets,
		numBuckets: numBuckets,
	}	
}

func fnv_a1(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h *= 1099511628211
		h ^= uint64(s[i])
	}
	return h
}


func bucketIndex(s string, numBuckets int) int {
	hash := fnv_a1(s)
	return int(hash % uint64(numBuckets))
}

func NewBucket(bucketIndex int) *Bucket {
	return &Bucket{
		bucketIndex: bucketIndex,
		entries: list.New(),
	}
}

func (ht *HashTable) Insert(key string, value interface{}) {
	index := bucketIndex(key, ht.numBuckets)
	bucket := ht.buckets[index]
	//check if the key already exists in the bucket
	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*HashTableEntry)
		if entry.key == key {
			//update the value if the key already exists
			entry.value = value
			return
		}
	}
	//if the key does not exist, add a new entry to the bucket
	bucket.entries.PushBack(&HashTableEntry{
		key: key,
		value: value,
	})
}

func (ht *HashTable) GET(key string) (interface{},bool){
	index := bucketIndex(key,ht.numBuckets)
	bucket := ht.buckets[index]
	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*HashTableEntry)
		if entry.key == key {
			return entry.value,true
		}
	}
	return nil,false
}

func (ht *HashTable) Delete(key string) bool {
	index := bucketIndex(key,ht.numBuckets)
	bucket := ht.buckets[index]
	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*HashTableEntry)
		if entry.key == key {
			bucket.entries.Remove(e)
			return true
		}
	}
	return false
}


func main() {
	// test get and Insert
	ht := NewHashTable(10)
	ht.Insert("name", "Alice")
	ht.Insert("age", 30)
	ht.Insert("city", "New York")
	
	if value, found := ht.GET("name"); found {
		println("name:", value.(string))
	} else {
		println("name not found")
	}
	//test Delete
	if ht.Delete("age") {
		println("age deleted")
	} else {
		println("age not found")
	}
}



