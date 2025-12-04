// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package lru provides LRU cache implementations.
package lru

import (
	"container/list"
	"sync"

	"github.com/luxfi/cache"
)

var _ cache.Cacher[struct{}, struct{}] = (*Cache[struct{}, struct{}])(nil)

// entry is a cache entry.
type entry[K comparable, V any] struct {
	key   K
	value V
}

// Cache is a thread-safe LRU cache.
type Cache[K comparable, V any] struct {
	lock     sync.Mutex
	size     int
	elements map[K]*list.Element
	order    *list.List
}

// NewCache creates a new LRU cache with the specified size.
func NewCache[K comparable, V any](size int) *Cache[K, V] {
	if size <= 0 {
		size = 1
	}
	return &Cache[K, V]{
		size:     size,
		elements: make(map[K]*list.Element),
		order:    list.New(),
	}
}

// Put inserts an element into the cache.
func (c *Cache[K, V]) Put(key K, value V) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if elem, ok := c.elements[key]; ok {
		// Update existing entry
		elem.Value.(*entry[K, V]).value = value
		c.order.MoveToFront(elem)
		return
	}

	// Evict oldest if at capacity
	if c.order.Len() >= c.size {
		oldest := c.order.Back()
		if oldest != nil {
			c.removeElement(oldest)
		}
	}

	// Add new entry
	e := &entry[K, V]{key: key, value: value}
	elem := c.order.PushFront(e)
	c.elements[key] = elem
}

// Get returns the entry with the key, if it exists.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if elem, ok := c.elements[key]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(*entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Evict removes the specified entry from the cache.
func (c *Cache[K, V]) Evict(key K) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if elem, ok := c.elements[key]; ok {
		c.removeElement(elem)
	}
}

// Flush removes all entries from the cache.
func (c *Cache[K, V]) Flush() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.elements = make(map[K]*list.Element)
	c.order.Init()
}

// Len returns the number of elements in the cache.
func (c *Cache[K, V]) Len() int {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.order.Len()
}

// PortionFilled returns fraction of cache currently filled.
func (c *Cache[K, V]) PortionFilled() float64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	return float64(c.order.Len()) / float64(c.size)
}

func (c *Cache[K, V]) removeElement(elem *list.Element) {
	e := elem.Value.(*entry[K, V])
	delete(c.elements, e.key)
	c.order.Remove(elem)
}
