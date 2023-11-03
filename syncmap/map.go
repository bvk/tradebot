package syncmap

import "sync"

type Map[K comparable, V any] struct {
	v sync.Map
}

func (m *Map[K, V]) Delete(key K) {
	m.v.Delete(key)
}

func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	v, ok := m.v.Load(key)
	if !ok {
		return value, ok
	}
	return v.(V), ok
}

func (m *Map[K, V]) Store(key K, value V) {
	m.v.Store(key, value)
}

func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.v.LoadAndDelete(key)
	if !loaded {
		return value, loaded
	}
	return v.(V), loaded
}

func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	a, loaded := m.v.LoadOrStore(key, value)
	return a.(V), loaded
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	return m.v.CompareAndDelete(key, old)
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) bool {
	return m.v.CompareAndSwap(key, old, new)
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.v.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}
