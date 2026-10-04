package memtable_test

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elvan/lsm-store/internal/keys"
	"github.com/elvan/lsm-store/internal/memtable"
)

func TestEmpty(t *testing.T) {
	m := memtable.New()
	if _, _, ok := m.Get([]byte("a"), keys.MaxSeq); ok {
		t.Error("Get on empty memtable: ok = true")
	}
	if n := m.Len(); n != 0 {
		t.Errorf("Len() = %d, want 0", n)
	}
	if sz := m.ApproximateSize(); sz != 0 {
		t.Errorf("ApproximateSize() = %d, want 0", sz)
	}
	it := m.NewIterator()
	it.First()
	if it.Valid() {
		t.Error("iterator on empty memtable is valid")
	}
}

func TestAddAndGet(t *testing.T) {
	m := memtable.New()
	m.Add(1, keys.KindSet, []byte("apple"), []byte("red"))
	m.Add(2, keys.KindSet, []byte("banana"), []byte("yellow"))

	v, kind, ok := m.Get([]byte("apple"), keys.MaxSeq)
	if !ok || kind != keys.KindSet || string(v) != "red" {
		t.Errorf("Get(apple) = (%q, %v, %v), want (\"red\", SET, true)", v, kind, ok)
	}
	v, kind, ok = m.Get([]byte("banana"), keys.MaxSeq)
	if !ok || kind != keys.KindSet || string(v) != "yellow" {
		t.Errorf("Get(banana) = (%q, %v, %v), want (\"yellow\", SET, true)", v, kind, ok)
	}
	if m.Len() != 2 {
		t.Errorf("Len() = %d, want 2", m.Len())
	}
}

func TestEmptyValue(t *testing.T) {
	m := memtable.New()
	m.Add(1, keys.KindSet, []byte("k"), []byte{})
	v, kind, ok := m.Get([]byte("k"), keys.MaxSeq)
	if !ok || kind != keys.KindSet || len(v) != 0 {
		t.Errorf("Get(k) = (%q, %v, %v), want (\"\", SET, true)", v, kind, ok)
	}
}

func TestVersionsAndVisibility(t *testing.T) {
	m := memtable.New()
	k := []byte("k")
	m.Add(10, keys.KindSet, k, []byte("v10"))
	m.Add(20, keys.KindSet, k, []byte("v20"))
	m.Add(30, keys.KindDelete, k, nil)
	m.Add(40, keys.KindSet, k, []byte("v40"))

	tests := []struct {
		seq      uint64
		wantOK   bool
		wantKind keys.Kind
		wantVal  string
	}{
		{keys.MaxSeq, true, keys.KindSet, "v40"},
		{40, true, keys.KindSet, "v40"},
		{39, true, keys.KindDelete, ""},
		{30, true, keys.KindDelete, ""},
		{29, true, keys.KindSet, "v20"},
		{20, true, keys.KindSet, "v20"},
		{15, true, keys.KindSet, "v10"},
		{10, true, keys.KindSet, "v10"},
		{9, false, 0, ""},
		{0, false, 0, ""},
	}
	for _, tt := range tests {
		v, kind, ok := m.Get(k, tt.seq)
		if ok != tt.wantOK {
			t.Errorf("Get(k, %d): ok = %v, want %v", tt.seq, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if kind != tt.wantKind || string(v) != tt.wantVal {
			t.Errorf("Get(k, %d) = (%q, %v), want (%q, %v)", tt.seq, v, kind, tt.wantVal, tt.wantKind)
		}
		if kind == keys.KindDelete && v != nil {
			t.Errorf("Get(k, %d): tombstone returned non-nil value %q", tt.seq, v)
		}
	}
}

func TestGetDoesNotMatchOtherKeys(t *testing.T) {
	m := memtable.New()
	m.Add(1, keys.KindSet, []byte("ab"), []byte("x"))
	m.Add(2, keys.KindSet, []byte("a\x00"), []byte("y"))
	for _, k := range []string{"a", "b", "", "abc", "aa"} {
		if v, _, ok := m.Get([]byte(k), keys.MaxSeq); ok {
			t.Errorf("Get(%q) = %q, want not found", k, v)
		}
	}
}

func TestAddCopiesInput(t *testing.T) {
	m := memtable.New()
	key := []byte("key")
	val := []byte("value")
	m.Add(1, keys.KindSet, key, val)

	// The caller reuses its buffers.
	copy(key, "XXX")
	copy(val, "XXXXX")

	v, _, ok := m.Get([]byte("key"), keys.MaxSeq)
	if !ok || string(v) != "value" {
		t.Fatalf("Get(key) = (%q, %v) after caller mutated its buffers, want (\"value\", true)", v, ok)
	}
}

func TestApproximateSizeGrows(t *testing.T) {
	m := memtable.New()
	payload := 0
	prev := 0
	for i := range 100 {
		k := []byte(fmt.Sprintf("key-%03d", i))
		v := bytes.Repeat([]byte{'v'}, i)
		m.Add(uint64(i+1), keys.KindSet, k, v)
		payload += len(k) + len(v)

		sz := m.ApproximateSize()
		if sz <= prev {
			t.Fatalf("after Add #%d: ApproximateSize() = %d, not greater than previous %d", i, sz, prev)
		}
		if sz < payload {
			t.Fatalf("after Add #%d: ApproximateSize() = %d, smaller than payload bytes %d", i, sz, payload)
		}
		prev = sz
	}
}

func TestIteratorOrder(t *testing.T) {
	m := memtable.New()
	rng := rand.New(rand.NewPCG(1, 2))
	const n = 2000
	for seq := uint64(1); seq <= n; seq++ {
		k := []byte(fmt.Sprintf("k%03d", rng.IntN(300))) // many versions per key
		kind := keys.KindSet
		var v []byte
		if rng.IntN(5) == 0 {
			kind = keys.KindDelete
		} else {
			v = []byte(fmt.Sprintf("v%d", seq))
		}
		m.Add(seq, kind, k, v)
	}

	it := m.NewIterator()
	var prev []byte
	count := 0
	for it.First(); it.Valid(); it.Next() {
		ik := it.Key()
		_, seq, kind, ok := keys.Decode(ik)
		if !ok {
			t.Fatalf("iterator returned malformed internal key %x", ik)
		}
		if prev != nil && keys.Compare(prev, ik) >= 0 {
			t.Fatalf("iterator not in internal-key order: %x then %x", prev, ik)
		}
		if kind == keys.KindSet && string(it.Value()) != fmt.Sprintf("v%d", seq) {
			t.Fatalf("seq %d: value %q, want %q", seq, it.Value(), fmt.Sprintf("v%d", seq))
		}
		prev = ik
		count++
	}
	if count != n || m.Len() != n {
		t.Fatalf("iterated %d entries, Len() = %d, want %d", count, m.Len(), n)
	}
}

func TestConcurrentGetDuringAdd(t *testing.T) {
	m := memtable.New()
	const n = 10_000
	var published atomic.Int64
	done := make(chan struct{})
	errs := make(chan error, 8)

	var wg sync.WaitGroup
	for r := range 4 {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(r), 1))
			for {
				select {
				case <-done:
					return
				default:
				}
				p := published.Load()
				if p == 0 {
					continue
				}
				i := rng.Int64N(p)
				k := fmt.Sprintf("key-%05d", i)
				v, kind, ok := m.Get([]byte(k), keys.MaxSeq)
				if !ok || kind != keys.KindSet || string(v) != fmt.Sprintf("val-%05d", i) {
					select {
					case errs <- fmt.Errorf("Get(%s) = (%q, %v, %v)", k, v, kind, ok):
					default:
					}
					return
				}
				_ = m.ApproximateSize()
				_ = m.Len()
			}
		})
	}

	for i := range n {
		m.Add(uint64(i+1), keys.KindSet, []byte(fmt.Sprintf("key-%05d", i)), []byte(fmt.Sprintf("val-%05d", i)))
		published.Store(int64(i + 1))
	}
	close(done)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func BenchmarkAdd(b *testing.B) {
	m := memtable.New()
	key := make([]byte, 16)
	val := make([]byte, 100)
	rng := rand.New(rand.NewPCG(1, 1))
	b.ReportAllocs()
	var seq uint64
	for b.Loop() {
		seq++
		for i := range key {
			key[i] = byte('a' + rng.IntN(26))
		}
		m.Add(seq, keys.KindSet, key, val)
	}
}
