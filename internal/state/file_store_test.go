package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
TC-STATE-STORE-001
Type: Positive
Title: Load returns empty state for missing file
Summary:
Calls Load against a path that does not exist yet.
Missing state file is an expected condition and should not fail.
The returned state should be the zero-value checkpoint.

Validates:
  - missing file does not return an error
  - returned state is empty
  - path can be absent before first save
*/
func TestLoadMissingFileReturnsEmptyState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != (State{}) {
		t.Fatalf("expected empty state, got %+v", got)
	}
}

/*
TC-STATE-STORE-002
Type: Positive
Title: Save then load round-trip persists state with secure mode
Summary:
Saves a state checkpoint to a nested path and reads it back.
The store should create parent directories, write atomically, and
keep file mode owner-readable/writable only.

Validates:
  - save and load round-trip returns same values
  - state file is created with 0600 permissions
  - parent directory creation works for nested path
*/
func TestSaveAndLoadRoundTripWithSecurePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	store := NewFileStore(path)

	want := State{
		Target:      "vyos",
		AppliedUUID: "cfg-123",
		AppliedAt:   time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC),
	}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file permissions got=%#o want=%#o", perm, os.FileMode(0o600))
	}

	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Target != want.Target || got.AppliedUUID != want.AppliedUUID || !got.AppliedAt.Equal(want.AppliedAt) {
		t.Fatalf("loaded state mismatch got=%+v want=%+v", got, want)
	}
}

/*
TC-STATE-STORE-003
Type: Negative
Title: Load rejects nil context
Summary:
Verifies context validation for Load.
Nil context input should fail immediately with a clear error
instead of touching filesystem state.

Validates:
  - nil context returns error
  - error explains context is nil
  - no filesystem read is required for this failure
*/
func TestLoadRejectsNilContext(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))

	_, err := store.Load(nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("error %q does not contain context is nil", err.Error())
	}
}

/*
TC-STATE-STORE-004
Type: Negative
Title: Load rejects canceled context
Summary:
Uses a canceled context and calls Load before any file is read.
The store should fail fast with context cancellation.
No filesystem read should be required for this failure.

Validates:
  - canceled context returns error
  - error includes context canceled
  - operation exits before normal load behavior
*/
func TestLoadRejectsCanceledContext(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "state.json"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Load(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error %q does not contain context canceled", err.Error())
	}
}

/*
TC-STATE-STORE-005
Type: Negative
Title: Save rejects nil context
Summary:
Verifies context validation for Save.
Nil context input must fail before any write operation starts.
This protects caller contracts and avoids undefined behavior.

Validates:
  - nil context returns error
  - error explains context is nil
  - no state file is written
*/
func TestSaveRejectsNilContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)

	err := store.Save(nil, State{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("error %q does not contain context is nil", err.Error())
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written, stat err=%v", statErr)
	}
}

/*
TC-STATE-STORE-006
Type: Negative
Title: Save rejects canceled context
Summary:
Uses a canceled context to ensure Save fails fast.
This protects callers from partial work after cancellation.
No file should be created in this canceled-path failure.

Validates:
  - canceled context returns error
  - error includes context cancellation signal
  - file is not created
*/
func TestSaveRejectsCanceledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewFileStore(path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Save(ctx, State{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error %q does not contain context canceled", err.Error())
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file written, stat err=%v", statErr)
	}
}

/*
TC-STATE-STORE-007
Type: Negative
Title: Load fails for malformed JSON
Summary:
Writes malformed content into the state file and calls Load.
The loader must reject invalid JSON and return a decode error.
This ensures corrupted state files are surfaced clearly.

Validates:
  - malformed json returns error
  - error includes decode context
  - invalid file is not treated as empty state
*/
func TestLoadMalformedJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write malformed state: %v", err)
	}

	store := NewFileStore(path)
	_, err := store.Load(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode state file") {
		t.Fatalf("error %q does not contain decode state file", err.Error())
	}
}

/*
TC-STATE-STORE-008
Type: Negative
Title: Save rejects empty file path
Summary:
Constructs store with blank path and attempts save.
Path validation must reject empty or whitespace-only paths.
This prevents accidental writes to unexpected locations.

Validates:
  - empty path returns error
  - error indicates file path is empty
  - no filesystem mutation is attempted
*/
func TestSaveRejectsEmptyPath(t *testing.T) {
	store := NewFileStore("   ")

	err := store.Save(context.Background(), State{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "file path is empty") {
		t.Fatalf("error %q does not contain file path is empty", err.Error())
	}
}
