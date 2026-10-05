package lsm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/elvan/lsm-store/internal/keys"
	"github.com/elvan/lsm-store/internal/memtable"
	"github.com/elvan/lsm-store/internal/wal"
)

type DB struct {
	dir      string
	opts     Options
	lockFile *os.File
	walFile  *os.File
	wal      *wal.Writer

	mem     *memtable.MemTable
	seq     atomic.Uint64
	writeMu sync.Mutex

	closed atomic.Bool
}

func Open(dir string, opts Options) (_ *DB, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	lockFile, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lsm: database %q is locked by another process: %w", dir, err)
	}

	db := &DB{dir: dir, opts: opts, lockFile: lockFile, mem: memtable.New()}
	defer func() {
		if err != nil {
			if db.walFile != nil {
				_ = db.walFile.Close()
			}
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			_ = lockFile.Close()
		}
	}()

	walPath := filepath.Join(dir, "wal.log")
	maxSeq, goodSize, err := db.replay(walPath)
	if err != nil {
		if errors.Is(err, wal.ErrCorrupt) {
			return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		return nil, fmt.Errorf("lsm: replay wal: %w", err)
	}
	db.seq.Store(maxSeq)

	wf, err := os.OpenFile(walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("lsm: open wal: %w", err)
	}
	db.walFile = wf
	// Cut off any torn tail so new records don't land behind garbage.
	if err = wf.Truncate(goodSize); err != nil {
		return nil, fmt.Errorf("lsm: truncate wal: %w", err)
	}
	db.wal = wal.NewWriter(wf)
	return db, nil
}

func (db *DB) replay(path string) (maxSeq uint64, goodSize int64, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, nil // fresh database
	}
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	reader := wal.NewReader(f)
	for {
		payload, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, 0, err
		}
		batch, err := wal.DecodeBatch(payload)
		if err != nil {
			return 0, 0, err
		}
		for i, entry := range batch.Entries {
			entrySeq := batch.Seq + uint64(i)
			db.mem.Add(entrySeq, entry.Kind, entry.Key, entry.Value)
			maxSeq = max(maxSeq, entrySeq)
		}
		goodSize += int64(wal.HeaderSize + len(payload))
	}
	return maxSeq, goodSize, nil
}

func (db *DB) write(kind keys.Kind, key, value []byte) error {
	db.writeMu.Lock()
	defer db.writeMu.Unlock()
	if db.closed.Load() {
		return ErrClosed
	}
	seq := db.seq.Add(1)
	b := wal.Batch{
		Seq: seq,
	}
	switch kind {
	case keys.KindSet:
		b.Put(key, value)
	case keys.KindDelete:
		b.Delete(key)
	}
	payload := b.Encode(nil)
	err := db.wal.WriteRecord(payload)
	if err != nil {
		return err
	}
	if db.opts.SyncWrites {
		err = db.wal.Sync()
		if err != nil {
			return err
		}
	}
	db.mem.Add(seq, kind, key, value)

	return nil
}

func (db *DB) Put(key, value []byte) error {
	if db.closed.Load() {
		return ErrClosed
	}
	if len(key) == 0 {
		return ErrEmptyKey
	}
	if len(key) > MaxKeySize {
		return ErrKeyTooLarge
	}
	if len(value) > MaxValueSize {
		return ErrValueTooLarge
	}
	return db.write(keys.KindSet, key, value)
}

func (db *DB) Get(key []byte) ([]byte, error) {
	if db.closed.Load() {
		return nil, ErrClosed
	}
	if len(key) == 0 {
		return nil, ErrEmptyKey
	}
	if len(key) > MaxKeySize {
		return nil, ErrKeyTooLarge
	}
	seq := db.seq.Load()
	val, kind, ok := db.mem.Get(key, seq)
	if !ok || kind == keys.KindDelete {
		return nil, ErrNotFound
	}
	return bytes.Clone(val), nil
}

func (db *DB) Delete(key []byte) error {
	if db.closed.Load() {
		return ErrClosed
	}
	if len(key) == 0 {
		return ErrEmptyKey
	}
	if len(key) > MaxKeySize {
		return ErrKeyTooLarge
	}
	return db.write(keys.KindDelete, key, nil)
}

func (db *DB) Close() error {
	if db.closed.Swap(true) {
		return ErrClosed
	}
	db.writeMu.Lock()
	errSync := db.wal.Sync()
	errWf := db.walFile.Close()
	errFlock := syscall.Flock(int(db.lockFile.Fd()), syscall.LOCK_UN)
	db.writeMu.Unlock()
	errLf := db.lockFile.Close()
	return errors.Join(errSync, errWf, errFlock, errLf)
}
