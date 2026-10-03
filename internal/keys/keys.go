// Package keys defines the internal key format used throughout the store.
//
// An internal key is a user key followed by an 8-byte little-endian trailer
// holding (seq << 8 | kind). Internal keys are ordered by user key ascending,
// then by trailer descending, so the newest version of a key sorts first.
package keys

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
)

// Kind distinguishes a value from a deletion tombstone.
type Kind uint8

const (
	KindDelete Kind = 0
	KindSet    Kind = 1
)

const (
	// TrailerLen is the number of bytes appended to every user key.
	TrailerLen = 8
	// MaxSeq is the largest sequence number that fits in the trailer (2^56 - 1).
	MaxSeq uint64 = (1 << 56) - 1
)

// Append appends the internal key for (userKey, seq, kind) to dst and
// returns the extended slice. It panics if seq > MaxSeq.
func Append(dst, userKey []byte, seq uint64, kind Kind) []byte {
	if seq > MaxSeq {
		panic("keys: sequence number out of range")
	}
	dst = append(dst, userKey...)
	trailer := (seq << 8) | uint64(kind)
	return binary.LittleEndian.AppendUint64(dst, trailer)
}

// Make returns a newly allocated internal key for (userKey, seq, kind).
func Make(userKey []byte, seq uint64, kind Kind) []byte {
	buff := make([]byte, 0, len(userKey)+TrailerLen)
	return Append(buff, userKey, seq, kind)
}

// Decode splits an internal key into its parts. ok is false if ikey is
// shorter than TrailerLen or the kind is unknown.
// The returned userKey aliases ikey (no copy is made).
func Decode(ikey []byte) (userKey []byte, seq uint64, kind Kind, ok bool) {
	if len(ikey) < TrailerLen {
		return nil, 0, 0, false
	}
	split := len(ikey) - TrailerLen
	trailer := binary.LittleEndian.Uint64(ikey[split:])
	k := Kind(trailer & 0xFF)
	if k != KindDelete && k != KindSet {
		return nil, 0, 0, false
	}
	return ikey[:split], trailer >> 8, k, true
}

// UserKey returns the user-key part of ikey (aliasing ikey).
// ikey must be a valid internal key.
func UserKey(ikey []byte) []byte {
	return ikey[:len(ikey)-TrailerLen]
}

// Compare orders internal keys: user key ascending, then trailer descending.
// It returns -1, 0 or +1. Both arguments must be valid internal keys.
func Compare(a, b []byte) int {
	aKey := UserKey(a)
	bKey := UserKey(b)
	if c := bytes.Compare(aKey, bKey); c != 0 {
		return c
	}
	aTrailer := binary.LittleEndian.Uint64(a[len(a)-TrailerLen:])
	bTrailer := binary.LittleEndian.Uint64(b[len(b)-TrailerLen:])
	return cmp.Compare(bTrailer, aTrailer)
}

// SeekKey returns the smallest internal key that is visible at sequence
// number seq for userKey, i.e. the key to seek to when looking up userKey
// "as of" seq.
func SeekKey(userKey []byte, seq uint64) []byte {
	return Make(userKey, seq, KindSet)
}

// String returns "SET" or "DEL" (or "Kind(n)" for unknown values),
// which makes test failures readable.
func (k Kind) String() string {
	switch k {
	case KindDelete:
		return "DEL"
	case KindSet:
		return "SET"
	default:
		return fmt.Sprintf("Kind(%d)", k)
	}
}
