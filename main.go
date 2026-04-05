package main

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

func main() {
	// Example usage
	str := "aleluia"
	numBuckets := 30
	index := bucketIndex(str, numBuckets)
	println("Bucket index for", str, "is", index)
}



