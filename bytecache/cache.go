// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bytecache

import (
	"sync"
	"sync/atomic"
)

const (
	numShards    = 256
	shardMask    = numShards - 1
)

// Stats contains cache performance metrics.
type Stats struct {
	EntriesCount uint64
	BytesSize    uint64
	Collisions   uint64
	GetCalls     uint64
	SetCalls     uint64
	Misses       uint64
}

// Cache is a high-performance sharded LRU byte cache.
// It provides O(1) lookups with minimal lock contention.
type Cache struct {
	shards      [numShards]*byteShard
	maxBytes    int64
	getCalls    uint64
	setCalls    uint64
	misses      uint64
}

type byteShard struct {
	mu          sync.RWMutex
	items       map[string]*byteEntry
	head, tail  *byteEntry
	currentSize int64
	maxSize     int64
}

type byteEntry struct {
	key        string
	value      []byte
	size       int
	prev, next *byteEntry
}

// New creates a new byte cache with the given max size in bytes.
func New(maxBytes int) *Cache {
	if maxBytes <= 0 {
		maxBytes = 1
	}
	c := &Cache{maxBytes: int64(maxBytes)}
	perShard := int64(maxBytes) / numShards
	if perShard < 1 {
		perShard = 1
	}
	for i := range c.shards {
		c.shards[i] = &byteShard{
			items:   make(map[string]*byteEntry),
			maxSize: perShard,
		}
	}
	return c
}

func (c *Cache) shard(key []byte) *byteShard {
	h := uint8(0)
	for _, b := range key {
		h ^= b
	}
	return c.shards[h&shardMask]
}

// Reset clears all cached entries.
func (c *Cache) Reset() {
	for _, s := range c.shards {
		s.mu.Lock()
		s.items = make(map[string]*byteEntry)
		s.head, s.tail = nil, nil
		s.currentSize = 0
		s.mu.Unlock()
	}
}

// Del removes a key from the cache.
func (c *Cache) Del(key []byte) {
	s := c.shard(key)
	k := string(key)
	s.mu.Lock()
	if e, ok := s.items[k]; ok {
		s.unlink(e)
		s.currentSize -= int64(e.size)
		delete(s.items, k)
	}
	s.mu.Unlock()
}

// Has reports whether a key exists.
func (c *Cache) Has(key []byte) bool {
	s := c.shard(key)
	k := string(key)
	s.mu.RLock()
	_, ok := s.items[k]
	s.mu.RUnlock()
	return ok
}

// HasGet returns the value and whether it exists.
func (c *Cache) HasGet(dst, key []byte) ([]byte, bool) {
	atomic.AddUint64(&c.getCalls, 1)
	s := c.shard(key)
	k := string(key)

	s.mu.Lock()
	e, ok := s.items[k]
	if ok {
		s.moveToFront(e)
		val := e.value
		s.mu.Unlock()
		if dst == nil {
			return append([]byte(nil), val...), true
		}
		return append(dst[:0], val...), true
	}
	s.mu.Unlock()

	atomic.AddUint64(&c.misses, 1)
	if dst == nil {
		return nil, false
	}
	return dst[:0], false
}

// Get looks up a value by key, copying into dst if provided.
func (c *Cache) Get(dst, key []byte) []byte {
	atomic.AddUint64(&c.getCalls, 1)
	s := c.shard(key)
	k := string(key)

	s.mu.Lock()
	e, ok := s.items[k]
	if ok {
		s.moveToFront(e)
		val := e.value
		s.mu.Unlock()
		if dst == nil {
			return append([]byte(nil), val...)
		}
		return append(dst[:0], val...)
	}
	s.mu.Unlock()

	atomic.AddUint64(&c.misses, 1)
	if dst == nil {
		return nil
	}
	return dst[:0]
}

// GetBig is an alias for Get (compatibility).
func (c *Cache) GetBig(dst, key []byte) []byte {
	return c.Get(dst, key)
}

// Set stores a key/value pair.
func (c *Cache) Set(key, value []byte) {
	atomic.AddUint64(&c.setCalls, 1)
	s := c.shard(key)
	k := string(key)
	v := append([]byte(nil), value...)
	entrySize := len(k) + len(v)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Entry too large for shard
	if int64(entrySize) > s.maxSize {
		return
	}

	// Update existing
	if e, ok := s.items[k]; ok {
		s.currentSize -= int64(e.size)
		e.value = v
		e.size = entrySize
		s.currentSize += int64(entrySize)
		s.moveToFront(e)
		return
	}

	// Evict until we have space
	for s.currentSize+int64(entrySize) > s.maxSize && s.tail != nil {
		old := s.tail
		s.unlink(old)
		s.currentSize -= int64(old.size)
		delete(s.items, old.key)
	}

	// Insert new entry
	e := &byteEntry{key: k, value: v, size: entrySize}
	s.items[k] = e
	s.pushFront(e)
	s.currentSize += int64(entrySize)
}

// SetBig is an alias for Set (compatibility).
func (c *Cache) SetBig(key, value []byte) {
	c.Set(key, value)
}

// UpdateStats populates the provided stats struct.
func (c *Cache) UpdateStats(s *Stats) {
	if s == nil {
		return
	}
	var entries, size uint64
	for _, sh := range c.shards {
		sh.mu.RLock()
		entries += uint64(len(sh.items))
		size += uint64(sh.currentSize)
		sh.mu.RUnlock()
	}
	s.EntriesCount = entries
	s.BytesSize = size
	s.Collisions = 0
	s.GetCalls = atomic.LoadUint64(&c.getCalls)
	s.SetCalls = atomic.LoadUint64(&c.setCalls)
	s.Misses = atomic.LoadUint64(&c.misses)
}

// Doubly-linked list operations for LRU

func (s *byteShard) pushFront(e *byteEntry) {
	e.prev = nil
	e.next = s.head
	if s.head != nil {
		s.head.prev = e
	}
	s.head = e
	if s.tail == nil {
		s.tail = e
	}
}

func (s *byteShard) unlink(e *byteEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

func (s *byteShard) moveToFront(e *byteEntry) {
	if s.head == e {
		return
	}
	s.unlink(e)
	s.pushFront(e)
}

// SaveToFileConcurrent is a no-op for compatibility with fastcache API.
func (c *Cache) SaveToFileConcurrent(filePath string, concurrency int) error {
	return nil
}

// LoadFromFile is a no-op for compatibility with fastcache API.
func (c *Cache) LoadFromFile(filePath string) error {
	return nil
}
