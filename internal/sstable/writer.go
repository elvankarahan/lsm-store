package sstable

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/elvan/lsm-store/internal/keys"
)

const defaultBlockSize = 4 << 10

type Writer struct {
	w          io.Writer
	blockSize  int
	offset     uint64
	dataBlock  blockWriter
	indexBlock blockWriter
	lastKey    []byte
	closed     bool
}

func NewWriter(w io.Writer, blockSize int) *Writer {
	if blockSize <= 0 {
		blockSize = defaultBlockSize
	}
	return &Writer{
		w:         w,
		blockSize: blockSize,
	}
}

func (w *Writer) Add(key, value []byte) error {
	if w.closed {
		return fmt.Errorf("writer closed")
	}

	if len(w.lastKey) > 0 && keys.Compare(w.lastKey, key) >= 0 {
		return fmt.Errorf("writer: order is wrong")
	}

	w.dataBlock.add(key, value)
	w.lastKey = append(w.lastKey[:0], key...)
	if w.dataBlock.size() >= w.blockSize {
		err := w.flushDataBlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}

	w.closed = true
	err := w.flushDataBlock()
	if err != nil {
		return err
	}

	indexRaw := w.indexBlock.finish()
	_, err = w.w.Write(indexRaw)
	if err != nil {
		return err
	}
	indexOffset := w.offset
	indexLength := uint64(len(indexRaw))

	footer := Footer{IndexOffset: indexOffset, IndexLength: indexLength}

	footerRaw := footer.Encode(nil)
	_, err = w.w.Write(footerRaw)
	if err != nil {
		return err
	}

	return nil
}

func (w *Writer) flushDataBlock() error {
	if w.dataBlock.empty() {
		return nil
	}

	raw := w.dataBlock.finish()
	_, err := w.w.Write(raw)
	if err != nil {
		return err
	}

	var handle []byte

	handle = binary.AppendUvarint(handle, w.offset)
	handle = binary.AppendUvarint(handle, uint64(len(raw)))

	w.indexBlock.add(w.lastKey, handle)
	w.offset += uint64(len(raw))
	w.dataBlock.reset()

	return nil
}
