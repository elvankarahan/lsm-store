package skiplist_test

import (
	"bytes"
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elvan/lsm-store/internal/skiplist"
)

func newIntList() *skiplist.SkipList[int, string] {
	return skiplist.New[int, string](cmp.Compare[int])
}

func collect[K, V any](s *skiplist.SkipList[K, V]) ([]K, []V) {
	var ks []K
	var vs []V
	it := s.NewIterator()
	for it.First(); it.Valid(); it.Next() {
		ks = append(ks, it.Key())
		vs = append(vs, it.Value())
	}
	return ks, vs
}

func TestConcurrentReadWrite(t *testing.T) {
	l := skiplist.New[int, int](cmp.Compare[int])
	const n = 10000

	var done atomic.Bool
	var wg sync.WaitGroup

	// readers
	for range 4 {
		wg.Go(func() {
			for !done.Load() {
				for range 5 {
					k := rand.IntN(n)
					if v, ok := l.Get(k); ok && v != k {
						t.Errorf("Get(%d) returned wrong value %d", k, v)
						return
					}
				}

				it := l.NewIterator()
				it.First()
				last := -1
				for it.Valid() {
					if it.Key() <= last {
						t.Errorf("key is invalid")
						return
					}
					if it.Value() != it.Key() {
						t.Errorf("error")
					}
					last = it.Key()
					it.Next()
				}
			}
		})
	}

	// writer
	for _, k := range rand.Perm(n) {
		l.Insert(k, k)
	}
	done.Store(true)
	wg.Wait()

	le := l.Len()
	if le != n {
		t.Errorf("Len() = %d, want %d", n, le)
	}
	it := l.NewIterator()
	it.First()
	for i := range n {
		if v, ok := l.Get(i); !ok || v != i {
			t.Errorf("wrong value")
		}
	}

}

func TestTT(t *testing.T) {
	l := newIntList()
	model := make(map[int]string, 1000)
	for i := range 1000 {
		key := rand.IntN(500)
		val := strconv.Itoa(i)
		if l.Insert(key, val) {
			if _, exists := model[key]; exists {
				t.Errorf("Insert returned true for existing key %d", key)
			}
			model[key] = val
		} else {
			old, ok := model[key]
			if !ok {
				t.Errorf("Insert returned false for new key %d", key)
			}
			if got, _ := l.Get(key); got != old {
				t.Errorf("duplicate insert changed key %d: want %q, got %q", key, old, got)
			}
		}
	}
	mL := len(model)
	lL := l.Len()
	if mL != lL {
		t.Errorf("len not equal: model: %d, list: %d", mL, lL)
	}
	for i, v := range model {
		f, ok := l.Get(i)
		if !ok {
			t.Errorf("not found: %d", i)
			continue
		}
		if f != v {
			t.Errorf("values does not match, want: %s, found: %s", v, f)
		}
	}

	if _, ok := l.Get(1010); ok {
		t.Errorf("found not inserted key")
	}
}

func TestEmpty(t *testing.T) {
	s := newIntList()
	if n := s.Len(); n != 0 {
		t.Errorf("Len() = %d, want 0", n)
	}
	if h := s.Height(); h != 1 {
		t.Errorf("Height() = %d, want 1", h)
	}
	if _, ok := s.Get(1); ok {
		t.Error("Get(1) on empty list: ok = true")
	}
	it := s.NewIterator()
	it.First()
	if it.Valid() {
		t.Error("First() on empty list: Valid() = true")
	}
	it.SeekGE(0)
	if it.Valid() {
		t.Error("SeekGE(0) on empty list: Valid() = true")
	}
}

func TestInsertAndGet(t *testing.T) {
	s := newIntList()
	for _, k := range []int{5, 1, 3} {
		if !s.Insert(k, "v"+strconv.Itoa(k)) {
			t.Fatalf("Insert(%d) = false, want true", k)
		}
	}
	for _, k := range []int{1, 3, 5} {
		v, ok := s.Get(k)
		if !ok || v != "v"+strconv.Itoa(k) {
			t.Errorf("Get(%d) = (%q, %v), want (%q, true)", k, v, ok, "v"+strconv.Itoa(k))
		}
	}
	for _, k := range []int{0, 2, 4, 6} {
		if _, ok := s.Get(k); ok {
			t.Errorf("Get(%d): ok = true for missing key", k)
		}
	}
	if n := s.Len(); n != 3 {
		t.Errorf("Len() = %d, want 3", n)
	}
}

func TestInsertDuplicate(t *testing.T) {
	s := newIntList()
	if !s.Insert(1, "first") {
		t.Fatal("first Insert(1) = false")
	}
	if s.Insert(1, "second") {
		t.Fatal("second Insert(1) = true, want false for duplicate key")
	}
	if v, _ := s.Get(1); v != "first" {
		t.Errorf("Get(1) = %q, want %q (duplicate Insert must not overwrite)", v, "first")
	}
	if n := s.Len(); n != 1 {
		t.Errorf("Len() = %d, want 1", n)
	}
}

func TestIterationIsSorted(t *testing.T) {
	const n = 1000
	s := newIntList()
	for _, k := range rand.New(rand.NewPCG(1, 2)).Perm(n) {
		s.Insert(k, strconv.Itoa(k))
	}
	ks, vs := collect(s)
	if len(ks) != n {
		t.Fatalf("iterated %d keys, want %d", len(ks), n)
	}
	for i := range n {
		if ks[i] != i || vs[i] != strconv.Itoa(i) {
			t.Fatalf("entry %d = (%d, %q), want (%d, %q)", i, ks[i], vs[i], i, strconv.Itoa(i))
		}
	}
}

func TestSeekGE(t *testing.T) {
	s := newIntList()
	for k := 0; k < 200; k += 2 { // 0, 2, 4, ..., 198
		s.Insert(k, strconv.Itoa(k))
	}
	tests := []struct {
		seek      int
		wantValid bool
		wantKey   int
	}{
		{-100, true, 0},
		{0, true, 0},
		{1, true, 2},
		{2, true, 2},
		{97, true, 98},
		{197, true, 198},
		{198, true, 198},
		{199, false, 0},
		{1000, false, 0},
	}
	it := s.NewIterator()
	for _, tt := range tests {
		it.SeekGE(tt.seek)
		if it.Valid() != tt.wantValid {
			t.Errorf("SeekGE(%d): Valid() = %v, want %v", tt.seek, it.Valid(), tt.wantValid)
			continue
		}
		if tt.wantValid && it.Key() != tt.wantKey {
			t.Errorf("SeekGE(%d): Key() = %d, want %d", tt.seek, it.Key(), tt.wantKey)
		}
	}

	// After a seek, Next must continue in order.
	it.SeekGE(51)
	for want := 52; want < 60; want += 2 {
		if !it.Valid() || it.Key() != want {
			t.Fatalf("after SeekGE(51)+Next: got valid=%v key=%v, want %d", it.Valid(), it.Key(), want)
		}
		it.Next()
	}
}

func TestByteSliceKeys(t *testing.T) {
	s := skiplist.New[[]byte, []byte](bytes.Compare)
	for _, k := range []string{"banana", "apple", "cherry", "apple\x00", ""} {
		s.Insert([]byte(k), []byte("v:"+k))
	}
	ks, vs := collect(s)
	want := []string{"", "apple", "apple\x00", "banana", "cherry"}
	if len(ks) != len(want) {
		t.Fatalf("got %d keys, want %d", len(ks), len(want))
	}
	for i := range want {
		if string(ks[i]) != want[i] || string(vs[i]) != "v:"+want[i] {
			t.Errorf("entry %d = (%q, %q), want (%q, %q)", i, ks[i], vs[i], want[i], "v:"+want[i])
		}
	}
}

func TestHeightGrowsLogarithmically(t *testing.T) {
	s := newIntList()
	for i := range 10_000 {
		s.Insert(i, "")
	}
	// With p = 1/4, the expected max height for 10k entries is ~log4(10000) ≈ 6.6.
	if h := s.Height(); h < 4 || h > skiplist.MaxHeight {
		t.Errorf("Height() = %d after 10k inserts, want between 4 and %d", h, skiplist.MaxHeight)
	}
}

// TestConcurrentReadersSingleWriter is the key test for Step B.
// Run it with: go test -race -count=20 -run Concurrent ./internal/skiplist/
func TestConcurrentReadersSingleWriter(t *testing.T) {
	const n = 20_000
	s := newIntList()
	perm := rand.New(rand.NewPCG(3, 4)).Perm(n)

	var published atomic.Int64 // number of perm entries fully inserted
	done := make(chan struct{})
	errs := make(chan error, 16)
	report := func(err error) {
		select {
		case errs <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	for r := range 4 {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(r), 99))
			it := s.NewIterator()
			for {
				select {
				case <-done:
					return
				default:
				}

				// 1. Every key the writer finished inserting must be visible.
				if p := published.Load(); p > 0 {
					k := perm[rng.IntN(int(p))]
					if v, ok := s.Get(k); !ok || v != strconv.Itoa(k) {
						report(fmt.Errorf("Get(%d) = (%q, %v) after it was inserted", k, v, ok))
						return
					}
				}

				// 2. Any partial scan must be strictly increasing, start at >= the
				//    seek key, and never expose a half-initialized node.
				start := rng.IntN(n)
				prev := start - 1
				count := 0
				for it.SeekGE(start); it.Valid() && count < 64; it.Next() {
					k := it.Key()
					if k <= prev {
						report(fmt.Errorf("scan from %d: key %d after %d (not increasing)", start, k, prev))
						return
					}
					if it.Value() != strconv.Itoa(k) {
						report(fmt.Errorf("key %d has value %q", k, it.Value()))
						return
					}
					prev = k
					count++
				}
				_ = s.Len()
				_ = s.Height()
			}
		})
	}

	for i, k := range perm {
		s.Insert(k, strconv.Itoa(k))
		published.Store(int64(i + 1))
	}
	close(done)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := s.Len(); got != n {
		t.Errorf("Len() = %d, want %d", got, n)
	}
}

// FuzzAgainstModel compares the skip list with a map, the obviously-correct model.
// Every 2 input bytes form one operation: (op, key).
func FuzzAgainstModel(f *testing.F) {
	f.Add([]byte{0, 5, 0, 3, 1, 5, 2, 4, 0, 5, 1, 9})
	f.Add([]byte{0, 0, 0, 255, 2, 0, 2, 128, 2, 255})

	f.Fuzz(func(t *testing.T, ops []byte) {
		s := skiplist.New[int, int](cmp.Compare[int])
		model := map[int]int{}

		for i := 0; i+1 < len(ops); i += 2 {
			op, k := ops[i]%3, int(ops[i+1])
			switch op {
			case 0: // insert
				_, exists := model[k]
				if got := s.Insert(k, i); got == exists {
					t.Fatalf("Insert(%d) = %v, but key exists = %v", k, got, exists)
				}
				if !exists {
					model[k] = i
				}
			case 1: // get
				v, ok := s.Get(k)
				mv, mok := model[k]
				if ok != mok || v != mv {
					t.Fatalf("Get(%d) = (%d, %v), want (%d, %v)", k, v, ok, mv, mok)
				}
			case 2: // seek
				want, found := 0, false
				for mk := range model {
					if mk >= k && (!found || mk < want) {
						want, found = mk, true
					}
				}
				it := s.NewIterator()
				it.SeekGE(k)
				if it.Valid() != found || (found && it.Key() != want) {
					t.Fatalf("SeekGE(%d): valid=%v, want valid=%v key=%d", k, it.Valid(), found, want)
				}
			}
		}

		if s.Len() != len(model) {
			t.Fatalf("Len() = %d, want %d", s.Len(), len(model))
		}
		wantKeys := make([]int, 0, len(model))
		for k := range model {
			wantKeys = append(wantKeys, k)
		}
		slices.Sort(wantKeys)
		gotKeys, gotVals := collect(s)
		if !slices.Equal(gotKeys, wantKeys) {
			t.Fatalf("iteration = %v, want %v", gotKeys, wantKeys)
		}
		for i, k := range gotKeys {
			if gotVals[i] != model[k] {
				t.Fatalf("value for %d = %d, want %d", k, gotVals[i], model[k])
			}
		}
	})
}

func BenchmarkInsert(b *testing.B) {
	rng := rand.New(rand.NewPCG(5, 6))
	s := skiplist.New[int, int](cmp.Compare[int])
	b.ReportAllocs()
	for b.Loop() {
		s.Insert(rng.Int(), 0)
	}
}

func BenchmarkGet(b *testing.B) {
	const n = 100_000
	s := skiplist.New[int, int](cmp.Compare[int])
	for i := range n {
		s.Insert(i, i)
	}
	rng := rand.New(rand.NewPCG(7, 8))
	b.ReportAllocs()
	for b.Loop() {
		s.Get(rng.IntN(n))
	}
}

func BenchmarkGetParallel(b *testing.B) {
	const n = 100_000
	s := skiplist.New[int, int](cmp.Compare[int])
	for i := range n {
		s.Insert(i, i)
	}
	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewPCG(rand.Uint64(), 0))
		for pb.Next() {
			s.Get(rng.IntN(n))
		}
	})
}
