package skiplist

type Iterator[K, V any] struct {
	list *SkipList[K, V]
	cur  *node[K, V]
}

func (it *Iterator[K, V]) First() {
	it.cur = it.list.head.next[0].Load()
}

func (it *Iterator[K, V]) SeekGE(key K) {
	it.cur = it.list.findGreaterOrEqual(key, nil)
}

func (it *Iterator[K, V]) Next() {
	it.cur = it.cur.next[0].Load()
}

func (it *Iterator[K, V]) Valid() bool {
	return it.cur != nil
}

func (it *Iterator[K, V]) Key() K {
	return it.cur.key
}

func (it *Iterator[K, V]) Value() V {
	return it.cur.val
}
