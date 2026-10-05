package lsm_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/elvan/lsm-store"
)

func TestBasicCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// 1. Get non-existent
	if _, err := db.Get([]byte("missing")); !errors.Is(err, lsm.ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}

	// 2. Put and Get
	if err := db.Put([]byte("key1"), []byte("val1")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	val, err := db.Get([]byte("key1"))
	if err != nil || string(val) != "val1" {
		t.Fatalf("Get(key1) = (%q, %v), want (\"val1\", nil)", val, err)
	}

	// 3. Overwrite
	if err := db.Put([]byte("key1"), []byte("val2")); err != nil {
		t.Fatalf("Put overwrite failed: %v", err)
	}
	val, err = db.Get([]byte("key1"))
	if err != nil || string(val) != "val2" {
		t.Fatalf("Get(key1) = (%q, %v), want (\"val2\", nil)", val, err)
	}

	// 4. Delete
	if err := db.Delete([]byte("key1")); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := db.Get([]byte("key1")); !errors.Is(err, lsm.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}

	// 5. Delete non-existent key succeeds without error
	if err := db.Delete([]byte("non-existent")); err != nil {
		t.Fatalf("Delete missing key failed: %v", err)
	}
}

func TestKeyAndValueValidation(t *testing.T) {
	dir := t.TempDir()
	db, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Empty key
	if err := db.Put(nil, []byte("val")); !errors.Is(err, lsm.ErrEmptyKey) {
		t.Errorf("Put(nil) = %v, want ErrEmptyKey", err)
	}
	if err := db.Put([]byte(""), []byte("val")); !errors.Is(err, lsm.ErrEmptyKey) {
		t.Errorf("Put(\"\") = %v, want ErrEmptyKey", err)
	}
	if _, err := db.Get(nil); !errors.Is(err, lsm.ErrEmptyKey) {
		t.Errorf("Get(nil) = %v, want ErrEmptyKey", err)
	}
	if err := db.Delete(nil); !errors.Is(err, lsm.ErrEmptyKey) {
		t.Errorf("Delete(nil) = %v, want ErrEmptyKey", err)
	}

	// Large key
	largeKey := bytes.Repeat([]byte("k"), lsm.MaxKeySize+1)
	if err := db.Put(largeKey, []byte("val")); !errors.Is(err, lsm.ErrKeyTooLarge) {
		t.Errorf("Put(largeKey) = %v, want ErrKeyTooLarge", err)
	}
	if _, err := db.Get(largeKey); !errors.Is(err, lsm.ErrKeyTooLarge) {
		t.Errorf("Get(largeKey) = %v, want ErrKeyTooLarge", err)
	}

	// Large value
	largeVal := bytes.Repeat([]byte("v"), lsm.MaxValueSize+1)
	if err := db.Put([]byte("k"), largeVal); !errors.Is(err, lsm.ErrValueTooLarge) {
		t.Errorf("Put(largeVal) = %v, want ErrValueTooLarge", err)
	}
}

func TestClosedDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Subsequent operations must fail with ErrClosed
	if err := db.Put([]byte("k"), []byte("v")); !errors.Is(err, lsm.ErrClosed) {
		t.Errorf("Put after Close = %v, want ErrClosed", err)
	}
	if _, err := db.Get([]byte("k")); !errors.Is(err, lsm.ErrClosed) {
		t.Errorf("Get after Close = %v, want ErrClosed", err)
	}
	if err := db.Delete([]byte("k")); !errors.Is(err, lsm.ErrClosed) {
		t.Errorf("Delete after Close = %v, want ErrClosed", err)
	}
	if err := db.Close(); !errors.Is(err, lsm.ErrClosed) {
		t.Errorf("Second Close = %v, want ErrClosed", err)
	}
}

func TestDirectoryLocking(t *testing.T) {
	dir := t.TempDir()
	db1, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatalf("First Open failed: %v", err)
	}

	// Second Open on same directory must fail
	_, err = lsm.Open(dir, lsm.DefaultOptions())
	if err == nil {
		t.Fatal("Second Open succeeded, expected locking error")
	}

	// Close first DB
	if err := db1.Close(); err != nil {
		t.Fatalf("db1.Close failed: %v", err)
	}

	// Now second Open must succeed
	db2, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatalf("Second Open after first closed failed: %v", err)
	}
	_ = db2.Close()
}

func TestReopenRecovery(t *testing.T) {
	dir := t.TempDir()

	// 1. Open, write data, close
	db1, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		k := []byte(fmt.Sprintf("key-%03d", i))
		v := []byte(fmt.Sprintf("val-%03d", i))
		if err := db1.Put(k, v); err != nil {
			t.Fatal(err)
		}
	}
	// Delete every even key
	for i := 0; i < 100; i += 2 {
		k := []byte(fmt.Sprintf("key-%03d", i))
		if err := db1.Delete(k); err != nil {
			t.Fatal(err)
		}
	}
	if err := db1.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. Reopen and verify
	db2, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer db2.Close()

	for i := range 100 {
		k := []byte(fmt.Sprintf("key-%03d", i))
		v, err := db2.Get(k)
		if i%2 == 0 {
			if !errors.Is(err, lsm.ErrNotFound) {
				t.Errorf("Get(%s) = (%q, %v), want ErrNotFound", k, v, err)
			}
		} else {
			want := fmt.Sprintf("val-%03d", i)
			if err != nil || string(v) != want {
				t.Errorf("Get(%s) = (%q, %v), want %q", k, v, err, want)
			}
		}
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	dir := t.TempDir()
	db, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const n = 1000
	var wg sync.WaitGroup

	// Reader goroutines
	done := make(chan struct{})
	errs := make(chan error, 8)
	for r := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				k := []byte(fmt.Sprintf("key-%03d", id))
				_, _ = db.Get(k)
			}
		}(r)
	}

	// Writer loop
	for i := range n {
		k := []byte(fmt.Sprintf("key-%03d", i%10))
		v := []byte(fmt.Sprintf("val-%05d", i))
		if err := db.Put(k, v); err != nil {
			select {
			case errs <- err:
			default:
			}
			break
		}
	}
	close(done)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// Helper process for crash recovery testing.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	dir := args[0]

	db, err := lsm.Open(dir, lsm.Options{SyncWrites: true})
	if err != nil {
		os.Exit(3)
	}

	for i := range 500 {
		k := []byte(fmt.Sprintf("crash-key-%03d", i))
		v := []byte(fmt.Sprintf("crash-val-%03d", i))
		if err := db.Put(k, v); err != nil {
			os.Exit(4)
		}
	}

	// Crash immediately without calling db.Close()
	os.Exit(0)
}

func TestCrashRecovery(t *testing.T) {
	dir := t.TempDir()

	// Run child process to simulate crash
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", dir)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\nOutput: %s", err, string(out))
	}

	// Parent reopens the database
	db, err := lsm.Open(dir, lsm.DefaultOptions())
	if err != nil {
		t.Fatalf("Reopen after crash failed: %v", err)
	}
	defer db.Close()

	// Verify all 500 keys are intact
	for i := range 500 {
		k := []byte(fmt.Sprintf("crash-key-%03d", i))
		want := fmt.Sprintf("crash-val-%03d", i)
		v, err := db.Get(k)
		if err != nil || string(v) != want {
			t.Fatalf("Get(%s) = (%q, %v), want %q", k, v, err, want)
		}
	}
}
