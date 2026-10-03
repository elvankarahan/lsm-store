package skiplist

type Iterator[K, V any] struct {
	list    *SkipList[K, V]
	current *node[K, V]
}

func (it *Iterator[K, V]) First() {
	it.list.mu.RLock()
	defer it.list.mu.RUnlock()
	it.current = it.list.head.next[0]
}

func (it *Iterator[K, V]) SeekGE(key K) {
	it.list.mu.RLock()
	defer it.list.mu.RUnlock()
	it.current = it.list.findGreaterOrEqual(key, nil)
}

func (it *Iterator[K, V]) Next() {
	it.list.mu.RLock()
	defer it.list.mu.RUnlock()
	it.current = it.current.next[0]
}

func (it *Iterator[K, V]) Valid() bool {
	return it.current != nil
}

func (it *Iterator[K, V]) Key() K {
	return it.current.key
}

func (it *Iterator[K, V]) Value() V {
	return it.current.val
}
