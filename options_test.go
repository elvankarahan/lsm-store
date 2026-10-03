package lsm

import (
	"errors"
	"strings"
	"testing"
)

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()

	checks := []struct {
		name string
		got  int64
		want int64
	}{
		{"MemTableSize", int64(o.MemTableSize), 4 << 20},
		{"BlockSize", int64(o.BlockSize), 4 << 10},
		{"BloomBitsPerKey", int64(o.BloomBitsPerKey), 10},
		{"L0CompactionTrigger", int64(o.L0CompactionTrigger), 4},
		{"L1MaxBytes", o.L1MaxBytes, 10 << 20},
		{"LevelSizeMultiplier", int64(o.LevelSizeMultiplier), 10},
		{"TargetFileSize", o.TargetFileSize, 2 << 20},
		{"MaxLevels", int64(o.MaxLevels), 7},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("DefaultOptions().%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if o.SyncWrites {
		t.Error("DefaultOptions().SyncWrites = true, want false")
	}
	if o.Logger == nil {
		t.Error("DefaultOptions().Logger is nil, want a non-nil (discarding) logger")
	}
}

func TestWithDefaultsFillsZeroValues(t *testing.T) {
	var zero Options
	got := zero.withDefaults()
	want := DefaultOptions()

	if got.MemTableSize != want.MemTableSize ||
		got.BlockSize != want.BlockSize ||
		got.BloomBitsPerKey != want.BloomBitsPerKey ||
		got.L0CompactionTrigger != want.L0CompactionTrigger ||
		got.L1MaxBytes != want.L1MaxBytes ||
		got.LevelSizeMultiplier != want.LevelSizeMultiplier ||
		got.TargetFileSize != want.TargetFileSize ||
		got.MaxLevels != want.MaxLevels ||
		got.SyncWrites != want.SyncWrites {
		t.Errorf("Options{}.withDefaults() = %+v, want %+v", got, want)
	}
	if got.Logger == nil {
		t.Error("Options{}.withDefaults().Logger is nil")
	}
}

func TestWithDefaultsKeepsUserValues(t *testing.T) {
	in := Options{MemTableSize: 1 << 10, SyncWrites: true, MaxLevels: 3}
	got := in.withDefaults()

	if got.MemTableSize != 1<<10 {
		t.Errorf("MemTableSize = %d, want %d", got.MemTableSize, 1<<10)
	}
	if !got.SyncWrites {
		t.Error("SyncWrites = false, want true")
	}
	if got.MaxLevels != 3 {
		t.Errorf("MaxLevels = %d, want 3", got.MaxLevels)
	}
	if got.BlockSize != DefaultOptions().BlockSize {
		t.Errorf("BlockSize = %d, want default %d", got.BlockSize, DefaultOptions().BlockSize)
	}
	// withDefaults must not mutate its receiver.
	if in.BlockSize != 0 {
		t.Error("withDefaults mutated the original Options")
	}
}

func TestSentinelErrors(t *testing.T) {
	errs := map[string]error{
		"ErrNotFound":      ErrNotFound,
		"ErrClosed":        ErrClosed,
		"ErrCorrupt":       ErrCorrupt,
		"ErrEmptyKey":      ErrEmptyKey,
		"ErrKeyTooLarge":   ErrKeyTooLarge,
		"ErrValueTooLarge": ErrValueTooLarge,
	}
	for name, err := range errs {
		if err == nil {
			t.Fatalf("%s is nil", name)
		}
		if !strings.HasPrefix(err.Error(), "lsm: ") {
			t.Errorf("%s message %q should start with %q", name, err.Error(), "lsm: ")
		}
		for otherName, other := range errs {
			if name != otherName && errors.Is(err, other) {
				t.Errorf("errors.Is(%s, %s) = true, want distinct errors", name, otherName)
			}
		}
	}
}

func TestSizeLimits(t *testing.T) {
	if MaxKeySize != 64<<10 {
		t.Errorf("MaxKeySize = %d, want %d", MaxKeySize, 64<<10)
	}
	if MaxValueSize != 1<<20 {
		t.Errorf("MaxValueSize = %d, want %d", MaxValueSize, 1<<20)
	}
}
