package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/elvan/lsm-store"
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

const (
	HeaderSize    = 8        // 4 bytes CRC32C + 4 bytes length
	MaxRecordSize = 64 << 20 // 64 MiB sanity limit
)

type Syncer interface {
	Sync() error
}

type Reader struct {
	r   io.Reader
	hdr [HeaderSize]byte
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

func (r *Reader) Next() ([]byte, error) {
	_, err := io.ReadFull(r.r, r.hdr[:])
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, fmt.Errorf("wal: read header: %w", err)
	}

	crc := binary.LittleEndian.Uint32(r.hdr[0:4])
	length := binary.LittleEndian.Uint32(r.hdr[4:8])
	if length > MaxRecordSize {
		return nil, fmt.Errorf("wal: record length %d too large: %w", length, lsm.ErrCorrupt)
	}

	payload := make([]byte, length)
	_, err = io.ReadFull(r.r, payload)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, fmt.Errorf("wal: read payload: %w", err)
	}

	if crc32.Checksum(payload, crcTable) != crc {
		return nil, fmt.Errorf("wal: checksum mismatch: %w", lsm.ErrCorrupt)
	}
	return payload, nil
}

type Writer struct {
	bw     *bufio.Writer
	syncer Syncer
	hdr    [HeaderSize]byte
}

func NewWriter(w io.Writer) *Writer {
	wr := &Writer{bw: bufio.NewWriter(w)}
	if s, ok := w.(Syncer); ok {
		wr.syncer = s
	}
	return wr
}

func (w *Writer) WriteRecord(payload []byte) error {
	if len(payload) > MaxRecordSize {
		return fmt.Errorf("wal: %w", lsm.ErrValueTooLarge)
	}
	crc := crc32.Checksum(payload, crcTable)
	binary.LittleEndian.PutUint32(w.hdr[0:4], crc)
	binary.LittleEndian.PutUint32(w.hdr[4:8], uint32(len(payload)))
	_, err := w.bw.Write(w.hdr[:])
	if err != nil {
		return fmt.Errorf("wal: write header: %w", err)
	}
	_, err = w.bw.Write(payload)
	if err != nil {
		return fmt.Errorf("wal: write payload: %w", err)
	}
	return nil
}

func (w *Writer) Flush() error {
	err := w.bw.Flush()
	if err != nil {
		return fmt.Errorf("wal: flush: %w", err)
	}
	return nil
}

func (w *Writer) Sync() error {
	err := w.Flush()
	if err != nil {
		return err
	}
	if w.syncer != nil {
		err = w.syncer.Sync()
		if err != nil {
			return err
		}
	}
	return nil
}
