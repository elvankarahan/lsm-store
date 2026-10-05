package wal_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/elvan/lsm-store"
	"github.com/elvan/lsm-store/internal/keys"
	"github.com/elvan/lsm-store/internal/wal"
)

type mockSyncer struct {
	*bytes.Buffer
	synced bool
}

func (m *mockSyncer) Sync() error {
	m.synced = true
	return nil
}

func TestBatchEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		batch wal.Batch
	}{
		{
			name: "empty batch",
			batch: wal.Batch{
				Seq:     1,
				Entries: nil,
			},
		},
		{
			name: "single put",
			batch: wal.Batch{
				Seq: 42,
				Entries: []wal.Entry{
					{Kind: keys.KindSet, Key: []byte("foo"), Value: []byte("bar")},
				},
			},
		},
		{
			name: "single delete",
			batch: wal.Batch{
				Seq: 100,
				Entries: []wal.Entry{
					{Kind: keys.KindDelete, Key: []byte("foo"), Value: nil},
				},
			},
		},
		{
			name: "mixed operations",
			batch: wal.Batch{
				Seq: 500,
				Entries: []wal.Entry{
					{Kind: keys.KindSet, Key: []byte("k1"), Value: []byte("v1")},
					{Kind: keys.KindDelete, Key: []byte("k2"), Value: nil},
					{Kind: keys.KindSet, Key: []byte("k3"), Value: []byte("v3")},
					{Kind: keys.KindSet, Key: []byte(""), Value: []byte("")},
					{Kind: keys.KindSet, Key: []byte{0x00, 0xff, 0xfe}, Value: []byte{0x01, 0x02}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := tt.batch.Encode(nil)
			decoded, err := wal.DecodeBatch(enc)
			if err != nil {
				t.Fatalf("DecodeBatch failed: %v", err)
			}
			if decoded.Seq != tt.batch.Seq {
				t.Errorf("Seq = %d, want %d", decoded.Seq, tt.batch.Seq)
			}
			if len(decoded.Entries) != len(tt.batch.Entries) {
				t.Fatalf("Entries count = %d, want %d", len(decoded.Entries), len(tt.batch.Entries))
			}
			for i := range decoded.Entries {
				got := decoded.Entries[i]
				want := tt.batch.Entries[i]
				if got.Kind != want.Kind {
					t.Errorf("entry[%d].Kind = %v, want %v", i, got.Kind, want.Kind)
				}
				if !bytes.Equal(got.Key, want.Key) {
					t.Errorf("entry[%d].Key = %q, want %q", i, got.Key, want.Key)
				}
				if want.Kind == keys.KindSet && !bytes.Equal(got.Value, want.Value) {
					t.Errorf("entry[%d].Value = %q, want %q", i, got.Value, want.Value)
				}
			}
		})
	}
}

func TestBatchHelperMethods(t *testing.T) {
	var b wal.Batch
	b.Seq = 10
	b.Put([]byte("k1"), []byte("v1"))
	b.Delete([]byte("k2"))

	if b.Count() != 2 {
		t.Errorf("Count() = %d, want 2", b.Count())
	}
	b.Reset()
	if b.Count() != 0 || b.Seq != 0 {
		t.Errorf("Reset() did not clear batch: count=%d, seq=%d", b.Count(), b.Seq)
	}
}

func TestWriterReaderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := wal.NewWriter(&buf)

	records := [][]byte{
		[]byte("hello world"),
		[]byte(""),
		bytes.Repeat([]byte("A"), 1024),
		bytes.Repeat([]byte("B"), 64*1024),
	}

	for _, rec := range records {
		if err := w.WriteRecord(rec); err != nil {
			t.Fatalf("WriteRecord failed: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	r := wal.NewReader(&buf)
	for i, want := range records {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("Next[%d] failed: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("Next[%d] = %q (len %d), want %q (len %d)", i, got, len(got), want, len(want))
		}
	}

	// Next call must return io.EOF
	_, err := r.Next()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Next after end of records: err = %v, want io.EOF", err)
	}
}

func TestSyncCallsUnderlyingSyncer(t *testing.T) {
	mock := &mockSyncer{Buffer: new(bytes.Buffer)}
	w := wal.NewWriter(mock)
	if err := w.WriteRecord([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if mock.synced {
		t.Fatal("Sync was called prematurely before w.Sync()")
	}
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	if !mock.synced {
		t.Fatal("w.Sync() did not invoke underlying Syncer")
	}
}

func TestTornTailRecovery(t *testing.T) {
	// Write 5 records. Then truncate at every byte from offset where record 5 starts
	// to the very end of the file. In every single truncated slice, exactly 4 records
	// must be recovered cleanly and Next() must return EOF without corrupt error.
	var fullBuf bytes.Buffer
	w := wal.NewWriter(&fullBuf)

	records := make([][]byte, 5)
	for i := range records {
		records[i] = []byte(fmt.Sprintf("record-%03d-payload-%s", i, bytes.Repeat([]byte{'x'}, i*50)))
		if err := w.WriteRecord(records[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	// Calculate offset where record 4 ends (record 5 starts)
	var prefixBuf bytes.Buffer
	pw := wal.NewWriter(&prefixBuf)
	for i := 0; i < 4; i++ {
		_ = pw.WriteRecord(records[i])
	}
	_ = pw.Flush()
	rec4EndOffset := prefixBuf.Len()

	fullBytes := fullBuf.Bytes()

	// Truncate at every byte offset from rec4EndOffset to len(fullBytes)-1
	for truncateAt := rec4EndOffset; truncateAt < len(fullBytes); truncateAt++ {
		truncated := fullBytes[:truncateAt]
		r := wal.NewReader(bytes.NewReader(truncated))

		for i := 0; i < 4; i++ {
			got, err := r.Next()
			if err != nil {
				t.Fatalf("at truncate offset %d: record %d read failed: %v", truncateAt, i, err)
			}
			if !bytes.Equal(got, records[i]) {
				t.Fatalf("at truncate offset %d: record %d mismatch", truncateAt, i)
			}
		}

		// The 5th record was torn/truncated, so Next must return clean io.EOF
		_, err := r.Next()
		if !errors.Is(err, io.EOF) {
			t.Fatalf("at truncate offset %d: Next() = %v, want clean io.EOF", truncateAt, err)
		}
	}
}

func TestBitFlipCorruption(t *testing.T) {
	var buf bytes.Buffer
	w := wal.NewWriter(&buf)
	_ = w.WriteRecord([]byte("rec-1"))
	_ = w.WriteRecord([]byte("rec-2"))
	_ = w.WriteRecord([]byte("rec-3-corrupted-target"))
	_ = w.WriteRecord([]byte("rec-4"))
	_ = w.Flush()

	data := buf.Bytes()

	// Locate rec-3 payload and flip a bit in it
	target := []byte("corrupted-target")
	idx := bytes.Index(data, target)
	if idx == -1 {
		t.Fatal("target substring not found")
	}

	corrupted := slices.Clone(data)
	corrupted[idx] ^= 0x01 // flip single bit

	r := wal.NewReader(bytes.NewReader(corrupted))

	// Records 1 and 2 must succeed
	if rec, err := r.Next(); err != nil || string(rec) != "rec-1" {
		t.Fatalf("rec 1: %v, %v", rec, err)
	}
	if rec, err := r.Next(); err != nil || string(rec) != "rec-2" {
		t.Fatalf("rec 2: %v, %v", rec, err)
	}

	// Record 3 must fail with ErrCorrupt
	_, err := r.Next()
	if err == nil {
		t.Fatal("expected CRC error on corrupted record, got nil")
	}
	if !errors.Is(err, lsm.ErrCorrupt) && !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err = %v, want error indicating corruption", err)
	}
}

func TestFileRoundTripOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "000001.wal")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}

	w := wal.NewWriter(f)
	var b wal.Batch
	b.Seq = 100
	b.Put([]byte("key-1"), []byte("val-1"))
	b.Delete([]byte("key-2"))

	if err := w.WriteRecord(b.Encode(nil)); err != nil {
		t.Fatal(err)
	}
	if err := w.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen for reading
	rf, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	r := wal.NewReader(rf)
	payload, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := wal.DecodeBatch(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Seq != 100 || decoded.Count() != 2 {
		t.Fatalf("decoded: seq=%d, count=%d", decoded.Seq, decoded.Count())
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func FuzzBatchDecode(f *testing.F) {
	// Seed with valid batches
	var b wal.Batch
	b.Seq = 1
	b.Put([]byte("hello"), []byte("world"))
	b.Delete([]byte("foo"))
	f.Add(b.Encode(nil))
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must never panic
		batch, err := wal.DecodeBatch(data)
		if err == nil {
			// If decode succeeded, re-encoding should decode cleanly too
			re := batch.Encode(nil)
			batch2, err2 := wal.DecodeBatch(re)
			if err2 != nil {
				t.Fatalf("re-encode decode failed: %v", err2)
			}
			if batch2.Seq != batch.Seq || batch2.Count() != batch.Count() {
				t.Fatalf("re-encode mismatch: %+v vs %+v", batch2, batch)
			}
		}
	})
}

func FuzzReader(f *testing.F) {
	var buf bytes.Buffer
	w := wal.NewWriter(&buf)
	_ = w.WriteRecord([]byte("fuzz-record-1"))
	_ = w.WriteRecord([]byte("fuzz-record-2"))
	_ = w.Flush()
	f.Add(buf.Bytes())
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3, 4})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := wal.NewReader(bytes.NewReader(data))
		for {
			_, err := r.Next()
			if err != nil {
				// Must return EOF or ErrCorrupt (or I/O error) without panicking
				break
			}
		}
	})
}

func BenchmarkWriteRecord(b *testing.B) {
	payload := make([]byte, 256)
	w := wal.NewWriter(io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		_ = w.WriteRecord(payload)
	}
}

func BenchmarkBatchEncode(b *testing.B) {
	var batch wal.Batch
	batch.Seq = 1000
	for i := range 10 {
		batch.Put([]byte(fmt.Sprintf("key-%03d", i)), []byte(fmt.Sprintf("val-%03d", i)))
	}
	buf := make([]byte, 0, 1024)
	b.ReportAllocs()
	for b.Loop() {
		_ = batch.Encode(buf[:0])
	}
}

func BenchmarkBatchDecode(b *testing.B) {
	var batch wal.Batch
	batch.Seq = 1000
	for i := range 10 {
		batch.Put([]byte(fmt.Sprintf("key-%03d", i)), []byte(fmt.Sprintf("val-%03d", i)))
	}
	encoded := batch.Encode(nil)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = wal.DecodeBatch(encoded)
	}
}
