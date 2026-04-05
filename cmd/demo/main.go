package main

import "shelf"

func main() {
	ht := shelf.NewHashTable[string, string](16)

	ht.Insert("name", "Alice")
	ht.Insert("age", "30")
	ht.Insert("city", "Berlin")

	if v, ok := ht.Get("name"); ok {
		println("name:", v)
	}

	ht.Delete("age")
	println("size:", ht.Size())
	println("has age:", ht.Contains("age"))
	println("keys:", len(ht.Keys()))

	ht.Clear()
	println("after clear:", ht.Size())
}
