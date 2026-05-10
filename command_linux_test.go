/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package runc

import (
	"context"
	"os"
	"testing"
)

// TestCommandFileRun verifies that CommandFile can be used in place of
// Command to execute a binary by open file descriptor.
func TestCommandFileRun(t *testing.T) {
	ctx := context.Background()

	f, err := os.Open("/bin/true")
	if err != nil {
		t.Fatalf("open /bin/true: %v", err)
	}
	defer f.Close()

	r := &Runc{CommandFile: f}
	status, err := r.Run(ctx, "fake-id", "fake-bundle", &CreateOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 0 {
		t.Fatalf("want exit 0, got %d", status)
	}

	f2, err := os.Open("/bin/false")
	if err != nil {
		t.Fatalf("open /bin/false: %v", err)
	}
	defer f2.Close()

	r2 := &Runc{CommandFile: f2}
	status, err = r2.Run(ctx, "fake-id", "fake-bundle", &CreateOpts{})
	if err == nil {
		t.Fatal("expected non-nil error from /bin/false, got nil")
	}
	if status != 1 {
		t.Fatalf("want exit 1, got %d", status)
	}
}

// TestCommandFileRunAfterUnlink is the key "path never needed" test: it copies
// a binary to a temporary file, opens it, removes the path, then verifies that
// execution still succeeds via the open file descriptor alone. This mirrors
// the execveat(fd, "", argv, env, AT_EMPTY_PATH) semantics.
func TestCommandFileRunAfterUnlink(t *testing.T) {
	tmp, err := copyBinary(t, "/bin/true")
	if err != nil {
		t.Fatalf("copy /bin/true: %v", err)
	}

	f, err := os.Open(tmp)
	if err != nil {
		t.Fatalf("open temp binary: %v", err)
	}
	defer f.Close()

	// Remove the path — the binary is now only reachable through the FD.
	if err := os.Remove(tmp); err != nil {
		t.Fatalf("remove temp binary: %v", err)
	}

	ctx := context.Background()
	r := &Runc{CommandFile: f}
	status, err := r.Run(ctx, "fake-id", "fake-bundle", &CreateOpts{})
	if err != nil {
		t.Fatalf("unexpected error after unlink: %v", err)
	}
	if status != 0 {
		t.Fatalf("want exit 0 after unlink, got %d", status)
	}
}

// TestFinalizeCommandFDNumbering verifies that finalizeCommand appends the
// binary FD after any pre-existing ExtraFiles, keeping their FD positions
// intact. This ensures --preserve-fds and --status-fd accounting is correct.
func TestFinalizeCommandFDNumbering(t *testing.T) {
	f, err := os.Open("/bin/true")
	if err != nil {
		t.Fatalf("open /bin/true: %v", err)
	}
	defer f.Close()

	r := &Runc{CommandFile: f}
	cmd := r.command(context.Background(), "run")

	// Simulate two pre-existing ExtraFiles (e.g. from --preserve-fds).
	d1, err := os.Open("/dev/null")
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer d1.Close()
	d2, err := os.Open("/dev/null")
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	defer d2.Close()
	cmd.ExtraFiles = []*os.File{d1, d2}

	r.finalizeCommand(cmd)

	// d1 → FD 3, d2 → FD 4, binary → FD 5.
	const wantPath = "/proc/self/fd/5"
	if cmd.Path != wantPath {
		t.Errorf("cmd.Path = %q; want %q", cmd.Path, wantPath)
	}
	if n := len(cmd.ExtraFiles); n != 3 {
		t.Errorf("len(ExtraFiles) = %d; want 3", n)
	}
	if cmd.ExtraFiles[2] != f {
		t.Error("CommandFile not appended last to ExtraFiles")
	}
}

// TestFinalizeCommandNoExtraFiles verifies that finalizeCommand places the
// binary at FD 3 when there are no other ExtraFiles.
func TestFinalizeCommandNoExtraFiles(t *testing.T) {
	f, err := os.Open("/bin/true")
	if err != nil {
		t.Fatalf("open /bin/true: %v", err)
	}
	defer f.Close()

	r := &Runc{CommandFile: f}
	cmd := r.command(context.Background(), "run")

	r.finalizeCommand(cmd)

	const wantPath = "/proc/self/fd/3"
	if cmd.Path != wantPath {
		t.Errorf("cmd.Path = %q; want %q", cmd.Path, wantPath)
	}
	if n := len(cmd.ExtraFiles); n != 1 {
		t.Errorf("len(ExtraFiles) = %d; want 1", n)
	}
	if cmd.ExtraFiles[0] != f {
		t.Error("CommandFile not placed at ExtraFiles[0]")
	}
}

// copyBinary copies the ELF binary at src to a fresh temporary file and
// marks it executable. The caller is responsible for cleanup.
func copyBinary(t *testing.T, src string) (string, error) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "runc-cmdfile-test-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}
