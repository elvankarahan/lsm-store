package lsm

import (
	"log/slog"
)

// Options configures a DB. The zero value of any field means "use the default".
type Options struct {
	// MemTableSize is the approximate size in bytes at which the active
	// memtable is frozen and flushed to an L0 SSTable. Default: 4 MiB. (M7)
	MemTableSize int

	// SyncWrites makes every write fsync the WAL before returning.
	// Default: false. (M4)
	SyncWrites bool

	// BlockSize is the target size of an uncompressed SSTable data block.
	// Default: 4 KiB. (M5)
	BlockSize int

	// BloomBitsPerKey controls the bloom filter false-positive rate.
	// 10 bits/key gives about 1%. Default: 10. (M6)
	BloomBitsPerKey int

	// L0CompactionTrigger is the number of L0 files that triggers an
	// L0 -> L1 compaction. Default: 4. (M9)
	L0CompactionTrigger int

	// L1MaxBytes is the target total size of level 1. Default: 10 MiB. (M9)
	L1MaxBytes int64

	// LevelSizeMultiplier: level N+1 may be this many times larger than level N.
	// Default: 10. (M9)
	LevelSizeMultiplier int

	// TargetFileSize is the size at which compaction output is split
	// into a new SSTable. Default: 2 MiB. (M9)
	TargetFileSize int64

	// MaxLevels is the number of levels, including L0. Default: 7. (M9)
	MaxLevels int

	// Logger receives background-job logs (flushes, compactions).
	// Default: a logger that discards everything.
	Logger *slog.Logger
}

// DefaultOptions returns Options with every field set to its default.
func DefaultOptions() Options {
	return Options{
		MemTableSize:        4 * 1024 * 1024,
		SyncWrites:          false,
		BlockSize:           4 * 1024,
		BloomBitsPerKey:     10,
		L0CompactionTrigger: 4,
		L1MaxBytes:          10 * 1024 * 1024,
		LevelSizeMultiplier: 10,
		TargetFileSize:      2 * 1024 * 1024,
		MaxLevels:           7,
		Logger:              slog.New(slog.DiscardHandler),
	}
}

// withDefaults returns a copy of o where every zero-valued field has been
// replaced by its default. Non-zero fields are kept unchanged.
func (o Options) withDefaults() Options {
	cp := DefaultOptions()
	if o.MemTableSize != 0 {
		cp.MemTableSize = o.MemTableSize
	}
	if o.SyncWrites {
		cp.SyncWrites = o.SyncWrites
	}
	if o.BlockSize != 0 {
		cp.BlockSize = o.BlockSize
	}
	if o.BloomBitsPerKey != 0 {
		cp.BloomBitsPerKey = o.BloomBitsPerKey
	}
	if o.L0CompactionTrigger != 0 {
		cp.L0CompactionTrigger = o.L0CompactionTrigger
	}
	if o.L1MaxBytes != 0 {
		cp.L1MaxBytes = o.L1MaxBytes
	}
	if o.LevelSizeMultiplier != 0 {
		cp.LevelSizeMultiplier = o.LevelSizeMultiplier
	}
	if o.TargetFileSize != 0 {
		cp.TargetFileSize = o.TargetFileSize
	}
	if o.MaxLevels != 0 {
		cp.MaxLevels = o.MaxLevels
	}
	if o.Logger != nil {
		cp.Logger = o.Logger
	}
	return cp
}
