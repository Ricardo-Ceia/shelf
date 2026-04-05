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

func main() {
	//test bucket creation
	bucket := NewBucket(0)
	println(bucket.bucketIndex)
	//test hash table creation
	hashTable := NewHashTable(10)
	println(hashTable.buckets)
	//test insertion
	hashTable.Insert("key1", "value1")
	hashTable.Insert("key2", "value2")
	hashTable.Insert("key3", "value3")
	//test retrieval
	index := bucketIndex("key1", hashTable.numBuckets)
	bucket = hashTable.buckets[index]
	for e := bucket.entries.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*HashTableEntry)
		if entry.key == "key1" {
			println(entry.value.(string))
		}
	}
}



