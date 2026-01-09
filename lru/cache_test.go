package lru

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerCache(t *testing.T) {
	require := require.New(t)

	cache := NewCache[string, string](3)

	// Test basic operations
	cache.Put("a", "apple")
	cache.Put("b", "banana")
	cache.Put("c", "cherry")

	require.Equal(3, cache.Len())
	require.Equal(1.0, cache.PortionFilled())

	// Test Get
	val, ok := cache.Get("a")
	require.True(ok)
	require.Equal("apple", val)

	// Test eviction
	cache.Put("d", "date")
	require.Equal(3, cache.Len()) // Should still be 3 after eviction

	// Test Flush
	cache.Flush()
	require.Equal(0, cache.Len())
	require.Equal(0.0, cache.PortionFilled())
}

func TestCacheWithEvictionCallback(t *testing.T) {
	require := require.New(t)

	evicted := make([]string, 0)
	cache := NewCacheWithOnEvict[string, string](2, func(k, v string) {
		evicted = append(evicted, k)
	})

	cache.Put("x", "value-x")
	cache.Put("y", "value-y")
	cache.Put("z", "value-z") // Should evict 'x'

	require.Len(evicted, 1)
	require.Equal("x", evicted[0])
}
