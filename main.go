package main

type  Bucket struct{
	bucketIndex int
	entries *LinkedList
}

type LinkedList struct {
	head *Node
}

type Node struct {
	value string
	next  *Node
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
		entries: NewLinkedList(),
	}
}

func NewLinkedList() *LinkedList {
	return &LinkedList{head: nil}
}

func (ll *LinkedList) Append(value string) {
	newNode := &Node{value: value, next: nil}
	if ll.head==nil{
		ll.head=newNode
		return
	}
	current := ll.head
	for current.next!=nil{
		current=current.next
	}
	current.next=newNode
}

func main() {
	//test LinkedList
	ll := NewLinkedList()
	ll.Append("first")
	ll.Append("second")
	ll.Append("third")
	current := ll.head
	for current != nil {
		print(current.value, " ")
		current = current.next
	}
	// Example usage
	str := "aleluia"
	numBuckets := 30
	index := bucketIndex(str, numBuckets)
	println("Bucket index for", str, "is", index)
}



