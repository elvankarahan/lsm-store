package wal

import (
	"bytes"
	"encoding/binary"

	"github.com/elvan/lsm-store/internal/keys"
)

type Entry struct {
	Kind  keys.Kind
	Key   []byte
	Value []byte
}

type Batch struct {
	Seq     uint64
	Entries []Entry
}

func (b *Batch) Put(key, value []byte) {
	b.Entries = append(b.Entries, Entry{
		Kind:  keys.KindSet,
		Key:   bytes.Clone(key),
		Value: bytes.Clone(value),
	})
}

func (b *Batch) Delete(key []byte) {
	b.Entries = append(b.Entries, Entry{
		Kind: keys.KindDelete,
		Key:  bytes.Clone(key),
	})
}

func (b *Batch) Reset() {
	clear(b.Entries)
	b.Seq = 0
	b.Entries = b.Entries[:0]
}

func (b *Batch) Count() int {
	return len(b.Entries)
}

func (b *Batch) Encode(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, b.Seq)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(b.Entries)))
	for _, e := range b.Entries {
		dst = append(dst, byte(e.Kind))
		dst = binary.AppendUvarint(dst, uint64(len(e.Key)))
		dst = append(dst, e.Key...)
		if e.Kind == keys.KindSet {
			dst = binary.AppendUvarint(dst, uint64(len(e.Value)))
			dst = append(dst, e.Value...)
		}
	}
	return dst
}

func readBytes(data []byte) (field, rest []byte, ok bool) {
	l, n := binary.Uvarint(data)
	if n <= 0 || l > uint64(len(data)-n) {
		return nil, nil, false
	}
	end := n + int(l)
	return data[n:end:end], data[end:], true
}

func DecodeBatch(data []byte) (Batch, error) {
	if len(data) < 12 {
		return Batch{}, ErrCorrupt
	}
	seq := binary.LittleEndian.Uint64(data[:8])
	count := binary.LittleEndian.Uint32(data[8:12])
	data = data[12:]
	if count > uint32(len(data)/2) {
		return Batch{}, ErrCorrupt
	}

	entries := make([]Entry, 0, min(count, 1024))
	for range count {
		if len(data) == 0 {
			return Batch{}, ErrCorrupt
		}
		kind := keys.Kind(data[0])
		if kind != keys.KindSet && kind != keys.KindDelete {
			return Batch{}, ErrCorrupt
		}
		data = data[1:]

		var e Entry
		var ok bool
		e.Kind = kind
		if e.Key, data, ok = readBytes(data); !ok {
			return Batch{}, ErrCorrupt
		}
		if kind == keys.KindSet {
			if e.Value, data, ok = readBytes(data); !ok {
				return Batch{}, ErrCorrupt
			}
		}
		entries = append(entries, e)
	}
	if len(data) != 0 {
		return Batch{}, ErrCorrupt
	}
	return Batch{Seq: seq, Entries: entries}, nil
}
