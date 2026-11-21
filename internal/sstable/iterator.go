package sstable

import (
	"slices"

	"github.com/elvan/lsm-store/internal/keys"
)

type Iterator struct {
	reader   *Reader
	blockIdx int
	curBlock *blockReader
	entryIdx int
	err      error
}

func (it *Iterator) First() {
	if len(it.reader.index) == 0 {
		it.curBlock = nil
		return
	}

	br, err := it.reader.readBlock(0)
	if err != nil {
		it.curBlock = nil
		it.err = err
		return
	}
	it.curBlock = br
	it.blockIdx = 0
	it.entryIdx = 0
}

func (it *Iterator) SeekGE(key []byte) {
	idx, _ := slices.BinarySearchFunc(it.reader.index, key, func(e IndexEntry, target []byte) int {
		return keys.Compare(e.LastKey, target)
	})

	if idx == len(it.reader.index) {
		it.curBlock = nil
		return
	}
	br, err := it.reader.readBlock(idx)
	if err != nil {
		it.curBlock = nil
		it.err = err
		return
	}
	it.curBlock = br
	it.blockIdx = idx

	for i, e := range br.entries {
		if keys.Compare(e.key, key) >= 0 {
			it.entryIdx = i
			return
		}
	}
}

func (it *Iterator) Next() {
	if it.curBlock == nil {
		return
	}
	it.entryIdx++
	if it.entryIdx < len(it.curBlock.entries) {
		return
	}
	it.blockIdx++

	if it.blockIdx >= len(it.reader.index) {
		it.curBlock = nil
		return
	}

	br, err := it.reader.readBlock(it.blockIdx)
	if err != nil {
		it.curBlock = nil
		it.err = err
		return
	}
	it.curBlock = br
	it.entryIdx = 0

}

func (it *Iterator) Valid() bool {
	return it.curBlock != nil && it.entryIdx < len(it.curBlock.entries)
}

func (it *Iterator) Key() []byte {
	return it.curBlock.entries[it.entryIdx].key
}

func (it *Iterator) Value() []byte {
	return it.curBlock.entries[it.entryIdx].value
}

func (it *Iterator) Close() error {
	return it.err
}
