package bloom_test

import (
	"fmt"
	"testing"

	"github.com/elvan/lsm-store/internal/bloom"
)

func TestEmptyAndNilFilter(t *testing.T) {
	if f := bloom.New(nil, 10); f != nil {
		t.Fatalf("New(nil, 10) = %x, want nil", f)
	}
	if f := bloom.New([][]byte{[]byte("a")}, 0); f != nil {
		t.Fatalf("New(keys, 0) = %x, want nil", f)
	}

	var nilFilter bloom.Filter
	if !nilFilter.MayContain([]byte("anything")) {
		t.Error("nil Filter.MayContain returned false, want true")
	}
	if !(bloom.Filter{}).MayContain([]byte("anything")) {
		t.Error("empty Filter.MayContain returned false, want true")
	}
}

func TestNoFalseNegatives(t *testing.T) {
	for _, n := range []int{1, 10, 100, 1_000, 10_000} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			keys := make([][]byte, n)
			for i := range n {
				keys[i] = []byte(fmt.Sprintf("key-%06d", i))
			}

			f := bloom.New(keys, 10)
			if len(f) == 0 {
				t.Fatal("expected non-empty filter")
			}

			for i, k := range keys {
				if !f.MayContain(k) {
					t.Fatalf("false negative for key %d (%q)", i, k)
				}
			}
		})
	}
}

func TestFalsePositiveRate(t *testing.T) {
	const n = 10_000
	keys := make([][]byte, n)
	for i := range n {
		keys[i] = []byte(fmt.Sprintf("present-key-%06d", i))
	}

	f := bloom.New(keys, 10)

	const probes = 10_000
	falsePositives := 0
	for i := range probes {
		missing := []byte(fmt.Sprintf("absent-key-%06d", i))
		if f.MayContain(missing) {
			falsePositives++
		}
	}

	rate := float64(falsePositives) / float64(probes)
	// Theoretical FPR at 10 bits/key is ~0.82%. Allow generous headroom up to 2.5%.
	if rate > 0.025 {
		t.Fatalf("false positive rate = %.3f%% (%d/%d), want <= 2.5%%", rate*100, falsePositives, probes)
	}
	t.Logf("10 bits/key FPR = %.2f%% (%d/%d), filter size = %d bytes", rate*100, falsePositives, probes, len(f))
}

func TestCorruptOrShortFilterDoesNotPanic(t *testing.T) {
	// Single byte or huge k should conservatively return true without panicking.
	if !(bloom.Filter{0x05}).MayContain([]byte("k")) {
		t.Error("1-byte filter should conservatively return true")
	}
	if !(bloom.Filter{0x00, 0x00, 0xff}).MayContain([]byte("k")) {
		t.Error("filter with k=255 should conservatively return true")
	}
	if !(bloom.Filter{0x00, 0x00, 0x00}).MayContain([]byte("k")) {
		t.Error("filter with k=0 should conservatively return true")
	}
}

func BenchmarkMayContain(b *testing.B) {
	const n = 10_000
	keys := make([][]byte, n)
	for i := range n {
		keys[i] = []byte(fmt.Sprintf("key-%06d", i))
	}
	f := bloom.New(keys, 10)
	probe := []byte("missing-key-123456")

	b.ReportAllocs()
	for b.Loop() {
		_ = f.MayContain(probe)
	}
}
