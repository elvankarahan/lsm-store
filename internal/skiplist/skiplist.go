package skiplist

import (
	"math/rand/v2"
	"sync/atomic"
)

// MaxHeight with p = 1/4, 12 levels handle
// about 4^12 ≈ 16M entries efficiently.
const MaxHeight = 12

type node[K, V any] struct {
	key  K
	val  V
	next []atomic.Pointer[node[K, V]]
}

type SkipList[K, V any] struct {
	head   *node[K, V]
	cmp    func(a, b K) int
	height atomic.Int64
	length atomic.Int64
	rng    *rand.Rand
}

func New[K, V any](cmp func(a, b K) int) *SkipList[K, V] {
	list := &SkipList[K, V]{
		head: &node[K, V]{
			next: make([]atomic.Pointer[node[K, V]], MaxHeight),
		},
		cmp: cmp,
		rng: rand.New(rand.NewPCG(1, 2)),
	}
	list.height.Store(1)
	return list
}

func (s *SkipList[K, V]) randomHeight() int {
	h := 1
	for h < MaxHeight && s.rng.IntN(4) == 0 {
		h++
	}
	return h
}

func (s *SkipList[K, V]) findGreaterOrEqual(key K, prev *[MaxHeight]*node[K, V]) *node[K, V] {
	x := s.head
	level := int(s.height.Load()) - 1

	for {
		next := x.next[level].Load()

		if next != nil && s.cmp(next.key, key) < 0 {
			x = next
		} else {
			if prev != nil {
				prev[level] = x
			}
			if level == 0 {
				return next
			}
			level--
		}
	}
}

// Insert callers must serialize; there is no lock
func (s *SkipList[K, V]) Insert(key K, val V) bool {
	var prev [MaxHeight]*node[K, V]
	found := s.findGreaterOrEqual(key, &prev)
	if found != nil && s.cmp(found.key, key) == 0 {
		return false
	}
	h := s.randomHeight()
	oldH := int(s.height.Load())
	if h > oldH {
		s.height.Store(int64(h))
		for i := oldH; i < h; i++ {
			prev[i] = s.head
		}
	}
	n := &node[K, V]{
		key:  key,
		val:  val,
		next: make([]atomic.Pointer[node[K, V]], h),
	}
	for i := range h {
		n.next[i].Store(prev[i].next[i].Load())
	}
	for i := range h {
		prev[i].next[i].Store(n)
	}
	s.length.Add(1)
	return true
}

func (s *SkipList[K, V]) Get(key K) (V, bool) {
	found := s.findGreaterOrEqual(key, nil)
	if found != nil && s.cmp(found.key, key) == 0 {
		return found.val, true
	}
	var zero V
	return zero, false
}

func (s *SkipList[K, V]) Len() int {
	return int(s.length.Load())
}

func (s *SkipList[K, V]) Height() int {
	return int(s.height.Load())
}

func (s *SkipList[K, V]) NewIterator() *Iterator[K, V] {
	return &Iterator[K, V]{
		list: s,
		cur:  nil,
	}
}
