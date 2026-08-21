//go:build linux

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
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPidfdSocket(t *testing.T) {
	t.Run("received pidfd refers to the process it was opened for", func(t *testing.T) {
		s, err := NewPidfdSocket(testSocketPath(t))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		go sendFd(t, s.Path(), "standard", openPidfd(t, os.Getpid()))

		pidfd, err := s.ReceivePidfd()
		if err != nil {
			t.Fatal(err)
		}
		defer pidfd.Close()

		if pid := pidfdTargetPid(t, pidfd); pid != os.Getpid() {
			t.Fatalf("expected a pidfd for pid %d, got one for %d", os.Getpid(), pid)
		}
		// Signal 0 is a no-op which only checks the pidfd can be signalled through.
		if err := unix.PidfdSendSignal(int(pidfd.Fd()), 0, nil, 0); err != nil {
			t.Fatalf("cannot signal through the received pidfd: %v", err)
		}
	})

	t.Run("temp socket is removed on close", func(t *testing.T) {
		s, err := NewTempPidfdSocket()
		if err != nil {
			t.Fatal(err)
		}
		ensureSocketCleanup(t, s, s.Path())
	})
}

func TestPidfdSocketArgs(t *testing.T) {
	socket, err := NewTempPidfdSocket()
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()

	t.Run("create passes --pidfd-socket when set", func(t *testing.T) {
		o := &CreateOpts{PidfdSocket: socket}
		args, err := o.args()
		if err != nil {
			t.Fatal(err)
		}
		assertArgs(t, args, []string{"--pidfd-socket", socket.Path()})
	})

	t.Run("create omits --pidfd-socket when unset", func(t *testing.T) {
		o := &CreateOpts{}
		args, err := o.args()
		if err != nil {
			t.Fatal(err)
		}
		assertArgs(t, args, nil)
	})

	t.Run("exec passes --pidfd-socket when set", func(t *testing.T) {
		o := &ExecOpts{PidfdSocket: socket}
		args, err := o.args()
		if err != nil {
			t.Fatal(err)
		}
		assertArgs(t, args, []string{"--pidfd-socket", socket.Path()})
	})

	t.Run("exec omits --pidfd-socket when unset", func(t *testing.T) {
		o := &ExecOpts{}
		args, err := o.args()
		if err != nil {
			t.Fatal(err)
		}
		assertArgs(t, args, nil)
	})
}

func assertArgs(t *testing.T, got, expected []string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("expected args %v, got %v", expected, got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("expected args %v, got %v", expected, got)
		}
	}
}

// pidfdTargetPid returns the pid the given pidfd refers to, as reported by
// procfs.
func pidfdTargetPid(t *testing.T, pidfd *os.File) int {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", pidfd.Fd()))
	if err != nil {
		t.Fatalf("failed to read fdinfo of the received fd: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Pid:") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pid:")))
		if err != nil {
			t.Fatalf("failed to parse %q from fdinfo: %v", line, err)
		}
		return pid
	}
	t.Fatalf("received fd is not a pidfd, fdinfo:\n%s", data)
	return 0
}

// testSocketPath returns a path to bind a unix socket to. t.TempDir() is not
// used because it derives the directory name from the test's name, which is
// long enough here to exceed the 108 byte sun_path limit on some versions of
// Go.
func testSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gorunc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "sock")
	if len(path) >= 108 {
		t.Fatalf("socket path %q does not fit sun_path", path)
	}
	return path
}

// openPidfd opens a pidfd for pid. Pidfds need a v5.3 or newer kernel, and
// pidfd_open may also be blocked by a sandbox, neither of which the library
// itself requires, so the test is skipped rather than failed in those cases.
func openPidfd(t *testing.T, pid int) int {
	t.Helper()
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EPERM) {
			t.Skipf("pidfd_open is unavailable: %v", err)
		}
		t.Fatalf("failed to open a pidfd: %v", err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	return fd
}

// sendFd stands in for runc: it connects to the socket and sends fds along with
// name as the payload, the way runc's init process does. "standard" is the name
// runc uses for the pidfd of an init process, "setns" for an exec'd one.
func sendFd(t *testing.T, path, name string, fds ...int) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Error(err)
		return
	}
	defer conn.Close()

	uc, ok := conn.(*net.UnixConn)
	if !ok {
		t.Error("expected a unix connection")
		return
	}
	var rights []byte
	if len(fds) > 0 {
		rights = unix.UnixRights(fds...)
	}
	if _, _, err := uc.WriteMsgUnix([]byte(name), rights, nil); err != nil {
		t.Error(err)
	}
}
