package sstable_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/elvan/lsm-store/internal/keys"
	"github.com/elvan/lsm-store/internal/sstable"
)

func makeKey(userKey string, seq uint64) []byte {
	return keys.Make([]byte(userKey), seq, keys.KindSet)
}

func TestEmptySSTable(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 4096)
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	reader, err := sstable.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	if _, err := reader.Get(makeKey("any", 1)); !errors.Is(err, sstable.ErrNotFound) {
		t.Fatalf("Get on empty table = %v, want ErrNotFound", err)
	}

	it := reader.NewIterator()
	it.First()
	if it.Valid() {
		t.Fatal("iterator on empty table is valid")
	}
	it.SeekGE(makeKey("any", 1))
	if it.Valid() {
		t.Fatal("iterator after SeekGE on empty table is valid")
	}
}

func TestSingleBlock(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 4096)

	k1 := makeKey("apple", 10)
	k2 := makeKey("banana", 20)
	k3 := makeKey("cherry", 30)

	_ = w.Add(k1, []byte("val-apple"))
	_ = w.Add(k2, []byte("val-banana"))
	_ = w.Add(k3, []byte("val-cherry"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := sstable.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		k   []byte
		val string
	}{
		{k1, "val-apple"},
		{k2, "val-banana"},
		{k3, "val-cherry"},
	} {
		v, err := r.Get(tc.k)
		if err != nil || string(v) != tc.val {
			t.Fatalf("Get(%s) = (%q, %v), want %q", tc.k, v, err, tc.val)
		}
	}

	// Missing keys
	if _, err := r.Get(makeKey("apricot", 1)); !errors.Is(err, sstable.ErrNotFound) {
		t.Fatalf("Get(apricot) = %v, want ErrNotFound", err)
	}
	if _, err := r.Get(makeKey("date", 1)); !errors.Is(err, sstable.ErrNotFound) {
		t.Fatalf("Get(date) = %v, want ErrNotFound", err)
	}
}

func TestMultiBlockAndSeek(t *testing.T) {
	var buf bytes.Buffer
	// Small block size to force creation of multiple data blocks
	w := sstable.NewWriter(&buf, 256)

	const n = 1000
	var addedKeys [][]byte
	var addedVals [][]byte

	for i := range n {
		uk := fmt.Sprintf("key-%05d", i)
		k := makeKey(uk, uint64(i+1))
		v := []byte(fmt.Sprintf("val-%05d", i))
		addedKeys = append(addedKeys, k)
		addedVals = append(addedVals, v)
		if err := w.Add(k, v); err != nil {
			t.Fatalf("Add[%d] failed: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := sstable.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	// 1. Point lookup Get for all keys
	for i := range n {
		v, err := r.Get(addedKeys[i])
		if err != nil || !bytes.Equal(v, addedVals[i]) {
			t.Fatalf("Get[%d] = (%q, %v), want %q", i, v, err, addedVals[i])
		}
	}

	// 2. Sequential iterator traversal
	it := r.NewIterator()
	it.First()
	count := 0
	for it.Valid() {
		if !bytes.Equal(it.Key(), addedKeys[count]) {
			t.Fatalf("it[%d].Key = %x, want %x", count, it.Key(), addedKeys[count])
		}
		if !bytes.Equal(it.Value(), addedVals[count]) {
			t.Fatalf("it[%d].Value = %q, want %q", count, it.Value(), addedVals[count])
		}
		count++
		it.Next()
	}
	if count != n {
		t.Fatalf("iterated %d items, want %d", count, n)
	}

	// 3. SeekGE
	for _, targetIdx := range []int{0, 1, 50, 100, 500, 999} {
		it.SeekGE(addedKeys[targetIdx])
		if !it.Valid() || !bytes.Equal(it.Key(), addedKeys[targetIdx]) {
			t.Fatalf("SeekGE(%d) landed on valid=%v key=%x, want %x", targetIdx, it.Valid(), it.Key(), addedKeys[targetIdx])
		}
	}

	// SeekGE beyond last key
	it.SeekGE(makeKey("zzz", 1))
	if it.Valid() {
		t.Fatalf("SeekGE beyond last key is valid: %x", it.Key())
	}
}

func TestUnsortedKeysRejected(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 4096)
	if err := w.Add(makeKey("b", 1), []byte("v")); err != nil {
		t.Fatal(err)
	}
	// Adding "a" after "b" is out of order
	if err := w.Add(makeKey("a", 1), []byte("v")); err == nil {
		t.Fatal("expected error on out-of-order key, got nil")
	}
	// Adding duplicate key is also rejected
	if err := w.Add(makeKey("b", 1), []byte("v")); err == nil {
		t.Fatal("expected error on duplicate key, got nil")
	}
}

func TestConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.sst")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	w := sstable.NewWriter(f, 256)
	const n = 500
	for i := range n {
		_ = w.Add(makeKey(fmt.Sprintf("k-%04d", i), 1), []byte(fmt.Sprintf("v-%04d", i)))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	fi, err := rf.Stat()
	if err != nil {
		t.Fatal(err)
	}

	r, err := sstable.NewReader(rf, fi.Size())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func(wId int) {
			defer wg.Done()
			for i := range n {
				k := makeKey(fmt.Sprintf("k-%04d", (i+wId)%n), 1)
				want := fmt.Sprintf("v-%04d", (i+wId)%n)
				val, err := r.Get(k)
				if err != nil || string(val) != want {
					t.Errorf("worker %d Get = (%q, %v), want %q", wId, val, err, want)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestChecksumCorruption(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 256)
	_ = w.Add(makeKey("a", 1), []byte("val-a"))
	_ = w.Add(makeKey("b", 1), []byte("val-b"))
	_ = w.Close()

	raw := buf.Bytes()
	// Corrupt a byte in the first data block
	raw[5] ^= 0xff

	r, err := sstable.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}

	_, err = r.Get(makeKey("a", 1))
	if err == nil {
		t.Fatal("expected checksum corruption error, got nil")
	}
	if !errors.Is(err, sstable.ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
}

func TestFooterMagicCorruption(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 256)
	_ = w.Add(makeKey("a", 1), []byte("val-a"))
	_ = w.Close()

	raw := buf.Bytes()
	// Corrupt magic number in the last 8 bytes
	raw[len(raw)-1] ^= 0xff

	_, err := sstable.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err == nil {
		t.Fatal("expected magic corruption error, got nil")
	}
	if !errors.Is(err, sstable.ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
}

func FuzzBlockDecoder(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{})
	f.Add([]byte{1, 1, 'k', 'v', 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		// DecodeBlock must never panic
		_ = sstable.DecodeBlock(data)
	})
}

func BenchmarkWriterAdd(b *testing.B) {
	w := sstable.NewWriter(io.Discard, 4096)
	key := makeKey("bench-key", 1)
	val := []byte("bench-value-sample")
	b.ReportAllocs()
	for b.Loop() {
		_ = w.Add(key, val)
	}
}

func BenchmarkReaderGet(b *testing.B) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 4096)
	const n = 10_000
	var keysList [][]byte
	for i := range n {
		k := makeKey(fmt.Sprintf("key-%06d", i), uint64(i+1))
		keysList = append(keysList, k)
		_ = w.Add(k, []byte("val"))
	}
	_ = w.Close()

	data := buf.Bytes()
	r, err := sstable.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_, _ = r.Get(keysList[i%n])
	}
}
