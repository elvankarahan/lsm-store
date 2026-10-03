package skiplist

import (
	"math/rand/v2"
	"sync"
)

// MaxHeight with p = 1/4, 12 levels handle
// about 4^12 ≈ 16M entries efficiently.
const MaxHeight = 12

type node[K, V any] struct {
	key  K
	val  V
	next []*node[K, V]
}

type SkipList[K, V any] struct {
	head   *node[K, V]
	cmp    func(a, b K) int
	height int
	length int
	rng    *rand.Rand
	mu     sync.RWMutex
}

func New[K, V any](cmp func(a, b K) int) *SkipList[K, V] {
	return &SkipList[K, V]{
		head: &node[K, V]{
			next: make([]*node[K, V], MaxHeight),
		},
		cmp:    cmp,
		height: 1,
		rng:    rand.New(rand.NewPCG(1, 2)),
	}
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
	level := s.height - 1

	for {
		next := x.next[level]

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

func (s *SkipList[K, V]) Insert(key K, val V) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	var prev [MaxHeight]*node[K, V]
	found := s.findGreaterOrEqual(key, &prev)
	if found != nil && s.cmp(found.key, key) == 0 {
		return false
	}
	h := s.randomHeight()
	if h > s.height {
		for i := s.height; i < h; i++ {
			prev[i] = s.head
		}
		s.height = h
	}
	n := &node[K, V]{
		key:  key,
		val:  val,
		next: make([]*node[K, V], h),
	}
	for i := range h {
		n.next[i] = prev[i].next[i]
		prev[i].next[i] = n
	}
	s.length++
	return true
}

func (s *SkipList[K, V]) Get(key K) (V, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	found := s.findGreaterOrEqual(key, nil)
	if found != nil && s.cmp(found.key, key) == 0 {
		return found.val, true
	}
	var zero V
	return zero, false
}

func (s *SkipList[K, V]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.length
}

func (s *SkipList[K, V]) Height() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.height
}

func (s *SkipList[K, V]) NewIterator() *Iterator[K, V] {
	return &Iterator[K, V]{
		list:    s,
		current: nil,
	}
}
