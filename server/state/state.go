package state

import "sync"

// SafeMap wraps a standard Go map with a sync.RWMutex to prevent concurrent read/write race conditions.
// It uses Go Generics to support any comparable key type K and any value type V.
type SafeMap[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// NewSafeMap initializes and returns a new thread-safe SafeMap instance.
//
// Returns:
//   - A pointer to the initialized SafeMap[K, V].
func NewSafeMap[K comparable, V any]() *SafeMap[K, V] {
	return &SafeMap[K, V]{
		m: make(map[K]V),
	}
}

// Set safely adds or updates a key-value pair in the map.
// It acquires a write lock (Lock) to ensure exclusive access.
//
// Parameters:
//   - key: The key associated with the value.
//   - value: The value to store.
func (sm *SafeMap[K, V]) Set(key K, value V) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.m[key] = value
}

// Get safely retrieves a value from the map by its key.
// It acquires a shared read lock (RLock) allowing concurrent reads.
//
// Parameters:
//   - key: The key to look up.
//
// Returns:
//   - The stored value of type V, or the zero value of V if the key does not exist.
func (sm *SafeMap[K, V]) Get(key K) V {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.m[key]
}

// Delete safely removes a key and its associated value from the map.
// It acquires a write lock (Lock) to ensure exclusive access.
//
// Parameters:
//   - key: The key to delete.
func (sm *SafeMap[K, V]) Delete(key K) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.m, key)
}

// Len safely returns the current number of elements stored in the map.
// It acquires a shared read lock (RLock).
//
// Returns:
//   - The total number of key-value pairs as an integer.
func (sm *SafeMap[K, V]) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.m)
}

// Range safely iterates over all key-value pairs in the map, executing the provided callback function for each pair.
// It acquires a shared read lock (RLock) to ensure the map remains unchanged during iteration.
//
// Parameters:
//   - f: A callback function accepting key K and value V.
func (sm *SafeMap[K, V]) Range(f func(key K, value V)) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for k, v := range sm.m {
		f(k, v)
	}
}


