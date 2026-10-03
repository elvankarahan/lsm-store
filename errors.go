package lsm

import "errors"

var (
	ErrNotFound      = errors.New("lsm: key not found")
	ErrClosed        = errors.New("lsm: database is closed")
	ErrCorrupt       = errors.New("lsm: data is corrupt")
	ErrEmptyKey      = errors.New("lsm: key is empty")
	ErrKeyTooLarge   = errors.New("lsm: key is too large")
	ErrValueTooLarge = errors.New("lsm: value is too large")
)

const (
	MaxKeySize   = 64 << 10 // 64 KiB
	MaxValueSize = 1 << 20  // 1 MiB
)
