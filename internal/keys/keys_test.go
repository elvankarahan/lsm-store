package keys_test

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/elvan/lsm-store/internal/keys"
)

// entry is the decoded, human-readable form of an internal key.
type entry struct {
	userKey string
	seq     uint64
	kind    keys.Kind
}

func (e entry) encode() []byte { return keys.Make([]byte(e.userKey), e.seq, e.kind) }

func sign(x int) int {
	switch {
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 0
}

func TestAppendDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		userKey []byte
		seq     uint64
		kind    keys.Kind
	}{
		{"simple set", []byte("apple"), 1, keys.KindSet},
		{"simple delete", []byte("apple"), 42, keys.KindDelete},
		{"empty user key", []byte{}, 7, keys.KindSet},
		{"zero seq", []byte("k"), 0, keys.KindSet},
		{"max seq", []byte("k"), keys.MaxSeq, keys.KindDelete},
		{"binary user key", []byte{0x00, 0xff, 0x00, 0x01}, 123456789, keys.KindSet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ik := keys.Make(tt.userKey, tt.seq, tt.kind)
			if got, want := len(ik), len(tt.userKey)+keys.TrailerLen; got != want {
				t.Fatalf("len(Make(...)) = %d, want %d", got, want)
			}

			uk, seq, kind, ok := keys.Decode(ik)
			if !ok {
				t.Fatalf("Decode(%x): ok = false, want true", ik)
			}
			if !bytes.Equal(uk, tt.userKey) {
				t.Errorf("userKey = %q, want %q", uk, tt.userKey)
			}
			if seq != tt.seq {
				t.Errorf("seq = %d, want %d", seq, tt.seq)
			}
			if kind != tt.kind {
				t.Errorf("kind = %v, want %v", kind, tt.kind)
			}
			if got := keys.UserKey(ik); !bytes.Equal(got, tt.userKey) {
				t.Errorf("UserKey = %q, want %q", got, tt.userKey)
			}
		})
	}
}

// TestEncodingFormat pins the exact on-disk byte layout.
func TestEncodingFormat(t *testing.T) {
	got := keys.Make([]byte("ab"), 0x0102030405, keys.KindSet)
	want := []byte{'a', 'b', 0x01, 0x05, 0x04, 0x03, 0x02, 0x01, 0x00, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("Make(\"ab\", 0x0102030405, KindSet) = %x, want %x", got, want)
	}
}

func TestAppendPreservesPrefix(t *testing.T) {
	prefix := []byte("prefix-")
	dst := bytes.Clone(prefix)
	out := keys.Append(dst, []byte("k"), 5, keys.KindDelete)

	if !bytes.HasPrefix(out, prefix) {
		t.Fatalf("Append result %q does not keep prefix %q", out, prefix)
	}
	uk, seq, kind, ok := keys.Decode(out[len(prefix):])
	if !ok || string(uk) != "k" || seq != 5 || kind != keys.KindDelete {
		t.Fatalf("Decode(appended part) = (%q, %d, %v, %v), want (\"k\", 5, DEL, true)", uk, seq, kind, ok)
	}
}

func TestAppendUsesSpareCapacity(t *testing.T) {
	dst := make([]byte, 0, 64)
	out := keys.Append(dst, []byte("key"), 1, keys.KindSet)
	if &out[0] != &dst[:1][0] {
		t.Fatal("Append reallocated even though dst had enough spare capacity")
	}
}

func TestAppendPanicsOnSeqOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Append with seq > MaxSeq did not panic")
		}
	}()
	keys.Append(nil, []byte("k"), keys.MaxSeq+1, keys.KindSet)
}

func TestDecodeRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		ikey []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"seven bytes", make([]byte, 7)},
		{"unknown kind", binary.LittleEndian.AppendUint64([]byte("k"), 5<<8|2)},
		{"kind 0xff", binary.LittleEndian.AppendUint64([]byte("k"), 5<<8|0xff)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, ok := keys.Decode(tt.ikey); ok {
				t.Fatalf("Decode(%x): ok = true, want false", tt.ikey)
			}
		})
	}
}

func TestCompareOrdering(t *testing.T) {
	// Already in the expected order.
	want := []entry{
		{"", 3, keys.KindSet},
		{"a", 9, keys.KindSet},
		{"a", 9, keys.KindDelete}, // same seq: Set sorts before Delete
		{"a", 5, keys.KindDelete},
		{"a", 1, keys.KindSet},
		{"a\x00", 1, keys.KindSet}, // catches comparing raw bytes instead of user keys
		{"ab", 300, keys.KindSet},
		{"b", 100, keys.KindSet},
		{"b", 2, keys.KindDelete},
		{"b", 0, keys.KindSet},
	}

	encoded := make([][]byte, len(want))
	for i, e := range want {
		encoded[i] = e.encode()
	}

	for i := range encoded {
		for j := range encoded {
			got := sign(keys.Compare(encoded[i], encoded[j]))
			if exp := cmp.Compare(i, j); got != exp {
				t.Errorf("Compare(%v, %v) = %d, want %d", want[i], want[j], got, exp)
			}
		}
	}

	// Sorting a shuffled copy must give the expected order back.
	rng := rand.New(rand.NewPCG(1, 2))
	shuffled := slices.Clone(encoded)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	slices.SortFunc(shuffled, keys.Compare)
	for i := range shuffled {
		if !bytes.Equal(shuffled[i], encoded[i]) {
			t.Fatalf("sorted[%d] = %x, want %x (%v)", i, shuffled[i], encoded[i], want[i])
		}
	}
}

func TestSeekKey(t *testing.T) {
	var versions [][]byte
	for seq := uint64(1); seq <= 10; seq++ {
		kind := keys.KindSet
		if seq%3 == 0 {
			kind = keys.KindDelete
		}
		versions = append(versions, keys.Make([]byte("k"), seq, kind))
	}
	before := keys.Make([]byte("j"), 1, keys.KindSet)          // smaller user key, very old
	after := keys.Make([]byte("l"), keys.MaxSeq, keys.KindSet) // larger user key, very new

	for s := uint64(0); s <= 11; s++ {
		sk := keys.SeekKey([]byte("k"), s)
		for _, v := range versions {
			_, vseq, _, _ := keys.Decode(v)
			c := keys.Compare(sk, v)
			if vseq <= s && c > 0 {
				t.Errorf("SeekKey(k, %d) sorts after visible version seq=%d", s, vseq)
			}
			if vseq > s && c <= 0 {
				t.Errorf("SeekKey(k, %d) sorts at/before invisible version seq=%d", s, vseq)
			}
		}
		if keys.Compare(sk, before) <= 0 {
			t.Errorf("SeekKey(k, %d) should sort after any key of user key \"j\"", s)
		}
		if keys.Compare(sk, after) >= 0 {
			t.Errorf("SeekKey(k, %d) should sort before any key of user key \"l\"", s)
		}
	}
}

func TestKindString(t *testing.T) {
	if got := keys.KindSet.String(); got != "SET" {
		t.Errorf("KindSet.String() = %q, want \"SET\"", got)
	}
	if got := keys.KindDelete.String(); got != "DEL" {
		t.Errorf("KindDelete.String() = %q, want \"DEL\"", got)
	}
	if got := keys.Kind(7).String(); got != "Kind(7)" {
		t.Errorf("Kind(7).String() = %q, want \"Kind(7)\"", got)
	}
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte("apple"), uint64(1), true)
	f.Add([]byte{}, uint64(0), false)
	f.Add([]byte{0xff, 0x00}, keys.MaxSeq, true)

	f.Fuzz(func(t *testing.T, userKey []byte, seq uint64, isSet bool) {
		seq &= keys.MaxSeq
		kind := keys.KindDelete
		if isSet {
			kind = keys.KindSet
		}
		uk, gotSeq, gotKind, ok := keys.Decode(keys.Make(userKey, seq, kind))
		if !ok || !bytes.Equal(uk, userKey) || gotSeq != seq || gotKind != kind {
			t.Fatalf("round trip of (%x, %d, %v) gave (%x, %d, %v, ok=%v)",
				userKey, seq, kind, uk, gotSeq, gotKind, ok)
		}
	})
}

func FuzzCompare(f *testing.F) {
	f.Add([]byte("a"), uint64(1), []byte("a\x00"), uint64(1))
	f.Add([]byte("k"), uint64(5), []byte("k"), uint64(9))
	f.Add([]byte{}, uint64(0), []byte{0}, keys.MaxSeq)

	f.Fuzz(func(t *testing.T, a []byte, seqA uint64, b []byte, seqB uint64) {
		seqA &= keys.MaxSeq
		seqB &= keys.MaxSeq
		ka := keys.Make(a, seqA, keys.KindSet)
		kb := keys.Make(b, seqB, keys.KindSet)

		// Obviously-correct reference: user key ascending, seq descending.
		want := bytes.Compare(a, b)
		if want == 0 {
			want = cmp.Compare(seqB, seqA)
		}

		got := sign(keys.Compare(ka, kb))
		if got != want {
			t.Fatalf("Compare((%x,%d), (%x,%d)) = %d, want %d", a, seqA, b, seqB, got, want)
		}
		if rev := sign(keys.Compare(kb, ka)); rev != -got {
			t.Fatalf("Compare is not antisymmetric: %d vs %d", got, rev)
		}
	})
}

func BenchmarkCompare(b *testing.B) {
	x := keys.Make([]byte("user:000000000042:profile"), 1000, keys.KindSet)
	y := keys.Make([]byte("user:000000000042:profile"), 999, keys.KindSet)
	for b.Loop() {
		keys.Compare(x, y)
	}
}

func BenchmarkMake(b *testing.B) {
	uk := []byte("user:000000000042:profile")
	b.ReportAllocs()
	for b.Loop() {
		keys.Make(uk, 1000, keys.KindSet)
	}
}
