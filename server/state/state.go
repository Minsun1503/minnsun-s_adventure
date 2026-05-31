package state

import "sync"

// TypedSyncMap wraps Go's standard sync.Map with Generics to provide compile-time type safety.
// It is highly optimized for concurrent use cases where keys are mostly read rather than written,
// or when multiple goroutines read, write, and overwrite keys for disjoint sets of cells.
//
// Parameters:
//   - K: The comparable key type.
//   - V: The value type.
type TypedSyncMap[K comparable, V any] struct {
	m sync.Map // The underlying thread-safe sync.Map.
}

// Set stores the key-value pair in the map, overwriting any existing value.
// It is thread-safe and does not require explicit locking.
//
// Parameters:
//   - key: The key associated with the value.
//   - value: The value to store.
func (t *TypedSyncMap[K, V]) Set(key K, value V) {
	t.m.Store(key, value)
}

// Get retrieves the value associated with the key from the map.
// It is optimized for concurrent lock-free reads.
//
// Parameters:
//   - key: The key to look up.
//
// Returns:
//   - The stored value of type V (or the zero value of V if not found).
//   - A boolean flag indicating whether the key was found.
func (t *TypedSyncMap[K, V]) Get(key K) (V, bool) {
	v, ok := t.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	val, ok2 := v.(V)
	if !ok2 {
		var zero V
		return zero, false
	}
	return val, true
}

// Delete removes the key and its associated value from the map.
//
// Parameters:
//   - key: The key to delete.
func (t *TypedSyncMap[K, V]) Delete(key K) {
	t.m.Delete(key)
}

// Range calls the provided callback function sequentially for each key-value pair in the map.
// If the callback function returns false, the iteration stops.
//
// Parameters:
//   - f: A callback function accepting key K and value V, returning false to stop.
func (t *TypedSyncMap[K, V]) Range(f func(key K, value V) bool) {
	t.m.Range(func(k, v any) bool {
		return f(k.(K), v.(V))
	})
}
