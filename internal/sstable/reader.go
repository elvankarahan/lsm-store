package sstable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/elvan/lsm-store/internal/keys"
)

var ErrNotFound = errors.New("sstable: not found")

type IndexEntry struct {
	LastKey []byte
	Offset  uint64
	Length  uint64
}

type Reader struct {
	r      io.ReaderAt
	size   int64
	footer Footer
	index  []IndexEntry
}

func NewReader(r io.ReaderAt, size int64) (*Reader, error) {
	if size < FooterLen {
		return nil, ErrCorrupt
	}

	var footerBuf [FooterLen]byte
	if _, err := r.ReadAt(footerBuf[:], size-FooterLen); err != nil {
		return nil, fmt.Errorf("sstable: read footer: %w", err)
	}
	footer, err := DecodeFooter(footerBuf[:])
	if err != nil {
		return nil, err
	}
	if footer.IndexOffset+footer.IndexLength > uint64(size-FooterLen) {
		return nil, ErrCorrupt
	}

	indexRaw := make([]byte, footer.IndexLength)
	if _, err := r.ReadAt(indexRaw[:], int64(footer.IndexOffset)); err != nil {
		return nil, fmt.Errorf("sstable: read index offset: %w", err)
	}

	indexBlock, err := newBlockReader(indexRaw)
	if err != nil {
		return nil, err
	}

	var indexEntry []IndexEntry

	for _, entry := range indexBlock.entries {
		offset, n := binary.Uvarint(entry.value)
		if n <= 0 {
			return nil, fmt.Errorf("sstable: corrupt index offset: %w", ErrCorrupt)
		}
		length, m := binary.Uvarint(entry.value[n:])
		if m <= 0 {
			return nil, fmt.Errorf("sstable: corrupt index length: %w", ErrCorrupt)
		}

		indexEntry = append(indexEntry, IndexEntry{
			LastKey: entry.key,
			Offset:  offset,
			Length:  length,
		})
	}

	return &Reader{
		r:      r,
		size:   size,
		footer: footer,
		index:  indexEntry,
	}, nil

}

func (r *Reader) readBlock(idx int) (*blockReader, error) {
	ie := r.index[idx]
	raw := make([]byte, ie.Length)
	_, err := r.r.ReadAt(raw, int64(ie.Offset))
	if err != nil {
		return nil, err
	}
	return newBlockReader(raw)
}

func (r *Reader) Get(key []byte) ([]byte, error) {
	if len(r.index) == 0 {
		return nil, ErrNotFound
	}
	idx, _ := slices.BinarySearchFunc(r.index, key, func(e IndexEntry, target []byte) int {
		return keys.Compare(e.LastKey, target)
	})

	if idx == len(r.index) {
		return nil, ErrNotFound
	}

	br, err := r.readBlock(idx)
	if err != nil {
		return nil, err
	}

	for _, e := range br.entries {
		c := keys.Compare(e.key, key)
		if c == 0 {
			return e.value, nil
		}
		if c > 0 {
			return nil, ErrNotFound
		}
	}
	return nil, ErrNotFound
}
func (r *Reader) NewIterator() *Iterator {
	return &Iterator{reader: r, blockIdx: -1}
}
