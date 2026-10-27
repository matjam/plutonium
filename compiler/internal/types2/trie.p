// Translated by go2pu from internal/types2/trie.go at the go-cutover tag; edited by hand since.

// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types2

import (
	"pluto/sort"
	"compiler/compat"
	"compiler/pu"
)

// A trie[V] maps keys to values V, similar to a map.
// A key is a list of integers; two keys collide if one of them is a prefix of the other.
// For instance, [1, 2] and [1, 2, 3] collide, but [1, 2, 3] and [1, 2, 4] do not.
// If all keys have length 1, a trie degenerates into an ordinary map[int]V.
//
// Plutonium: an index leads either to a subtrie or to a value, in two maps,
// and a subtrie is a node of its own: a map holding maps, or interfaces
// holding them, would have each hold a counted reference to the next.
type trie[V any] struct {
	subs   map[int]*trie[V] // non-leaf nodes
	leaves map[int]V        // leaf nodes
}

func newTrie[V any]() *trie[V] {
	return pu.New(trie[V]{subs: make(map[int]*trie[V]), leaves: make(map[int]V)})
}

// insert inserts a value with a key into the trie; key must not be empty.
// If key doesn't collide with any other key in the trie, insert succeeds
// and returns (val, 0). Otherwise, insert fails and returns (alt, n) where
// alt is an unspecified but deterministically chosen value with a colliding
// key and n is the length > 0 of the common key prefix. The trie is not
// changed in this case.
func (tr *trie[V]) insert(key []int, val V) (V, int) {
	// A key serves as the path from the trie root to its corresponding value.
	for l, index := range key {
		if sub, exists := tr.subs[index]; exists {
			// Path continues.
			tr = sub
		} else if v, exists := tr.leaves[index]; exists {
			// Collision: An existing key ends here.
			// This means the existing key is a prefix of (or exactly equal to) key.
			return v, l + 1
		} else {
			// Path doesn't exist yet, we need to build it.
			if l == len(key)-1 {
				// No prefix collision detected; insert val as a new leaf node.
				tr.leaves[index] = val
				return val, 0
			}
			node := newTrie[V]()
			tr.subs[index] = node
			tr = node
		}
	}

	if len(key) == 0 {
		panic("trie.insert: key must not be empty")
	}

	// Collision: path ends here, but the trie continues.
	// This means key is a prefix of an existing key.
	// Return a value from the subtrie.
	for len(tr.subs)+len(tr.leaves) != 0 {
		k := tr.pickKey()
		if sub, ok := tr.subs[k]; ok {
			tr = sub
			continue
		}
		return tr.leaves[k], len(key)
	}

	panic("trie.insert: unreachable")
}

// pickKey deterministically picks a key of the trie and returns it.
// Only called in case of a collision.
// The trie must not be empty.
func (tr *trie[V]) pickKey() int {
	var keys []int
	for k := range tr.subs {
		keys = compat.AppendPlain(keys, k)
	}
	for k := range tr.leaves {
		keys = compat.AppendPlain(keys, k)
	}
	sort.Ints(keys) // guarantee deterministic element pick
	return keys[0]
}
