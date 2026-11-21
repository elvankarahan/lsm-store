package sstable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

var ErrCorrupt = errors.New("sstable: data is corrupt")
var crcTable = crc32.MakeTable(crc32.Castagnoli)

type blockEntry struct {
	key   []byte
	value []byte
}

type blockWriter struct {
	buf     []byte
	entries int
}

func (bw *blockWriter) add(key, value []byte) {
	bw.buf = binary.AppendUvarint(bw.buf, uint64(len(key)))
	bw.buf = binary.AppendUvarint(bw.buf, uint64(len(value)))
	bw.buf = append(bw.buf, key...)
	bw.buf = append(bw.buf, value...)
	bw.entries++
}
func (bw *blockWriter) size() int {
	return len(bw.buf)
}
func (bw *blockWriter) empty() bool {
	return bw.entries == 0
}
func (bw *blockWriter) reset() {
	bw.buf = bw.buf[:0]
	bw.entries = 0
}
func (bw *blockWriter) finish() []byte {
	crc := crc32.Checksum(bw.buf, crcTable)
	return binary.LittleEndian.AppendUint32(bw.buf, crc)
}

type blockReader struct {
	entries []blockEntry
}

func newBlockReader(raw []byte) (*blockReader, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("sstable: block too short: %w", ErrCorrupt)
	}

	body := raw[:len(raw)-4]
	wantCRC := binary.LittleEndian.Uint32(raw[len(raw)-4:])
	if crc32.Checksum(body, crcTable) != wantCRC {
		return nil, fmt.Errorf("sstable: block checksum mismatch: %w", ErrCorrupt)
	}

	var entries []blockEntry

	rem := body
	for len(rem) > 0 {
		kLen, n := binary.Uvarint(rem)
		if n <= 0 {
			return nil, ErrCorrupt
		}
		rem = rem[n:]

		vLen, m := binary.Uvarint(rem)
		if m <= 0 {
			return nil, ErrCorrupt
		}
		rem = rem[m:]

		if kLen > uint64(len(rem)) {
			return nil, ErrCorrupt
		}
		key := rem[:kLen:kLen]
		rem = rem[kLen:]

		if vLen > uint64(len(rem)) {
			return nil, ErrCorrupt
		}
		val := rem[:vLen:vLen]
		rem = rem[vLen:]

		entries = append(entries, blockEntry{key: key, value: val})
	}

	return &blockReader{entries: entries}, nil
}

func DecodeBlock(raw []byte) error {
	_, err := newBlockReader(raw)
	return err
}
