package sstable_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/elvan/lsm-store/internal/keys"
	"github.com/elvan/lsm-store/internal/sstable"
)

type countingReaderAt struct {
	r     io.ReaderAt
	reads atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.reads.Add(1)
	return c.r.ReadAt(p, off)
}

func TestBloomFilterSkipsDiskReads(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 256)

	const n = 1_000
	for i := range n {
		k := keys.Make([]byte(fmt.Sprintf("present-%05d", i)), uint64(i+1), keys.KindSet)
		if err := w.Add(k, []byte("value")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	counter := &countingReaderAt{r: bytes.NewReader(buf.Bytes())}
	r, err := sstable.NewReader(counter, int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	// Verify zero false negatives on present user keys.
	for i := range n {
		uk := []byte(fmt.Sprintf("present-%05d", i))
		if !r.MayContain(uk) {
			t.Fatalf("MayContain(%q) = false for present key", uk)
		}
	}

	// Reset read counter after NewReader loaded the index & filter.
	counter.reads.Store(0)

	// Probe 1,000 missing keys: Bloom filter should reject ~99% without touching disk.
	const missingCount = 1_000
	for i := range missingCount {
		k := keys.Make([]byte(fmt.Sprintf("absent-%05d", i)), keys.MaxSeq, keys.KindSet)
		if _, err := r.Get(k); !errors.Is(err, sstable.ErrNotFound) {
			t.Fatalf("Get(absent-%05d) = %v, want ErrNotFound", i, err)
		}
	}

	diskReads := counter.reads.Load()
	if diskReads > 35 {
		t.Fatalf("Bloom filter did not skip disk reads: %d/%d missing lookups read from disk (want <= 35)", diskReads, missingCount)
	}
	t.Logf("Missing-key disk reads avoided: %d/%d (%.1f%%)", missingCount-diskReads, missingCount, float64(missingCount-diskReads)/float64(missingCount)*100)
}

func TestBloomFilterUsesUserKey(t *testing.T) {
	var buf bytes.Buffer
	w := sstable.NewWriter(&buf, 256)
	if err := w.Add(keys.Make([]byte("apple"), 10, keys.KindSet), []byte("red")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := sstable.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	// Must match on user key regardless of sequence number.
	if !r.MayContain([]byte("apple")) {
		t.Fatal("MayContain(\"apple\") = false, want true (filter must hash user keys, not internal keys)")
	}
}
