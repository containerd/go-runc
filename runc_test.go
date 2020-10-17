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
	"errors"
	"io/ioutil"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestParseVersion(t *testing.T) {

	testParseVersion := func(t *testing.T, input string, expected Version) {
		actual, err := parseVersion([]byte(input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if expected != actual {
			t.Fatalf("expected: %v, actual: %v", expected, actual)
		}
	}

	t.Run("Full", func(t *testing.T) {
		input := `runc version 1.0.0-rc3
commit: 17f3e2a07439a024e54566774d597df9177ee216
spec: 1.0.0-rc5-dev
`
		expected := Version{
			Runc:   "1.0.0-rc3",
			Commit: "17f3e2a07439a024e54566774d597df9177ee216",
			Spec:   "1.0.0-rc5-dev",
		}
		testParseVersion(t, input, expected)
	})

	t.Run("WithoutCommit", func(t *testing.T) {
		input := `runc version 1.0.0-rc9
spec: 1.0.1-dev
`
		expected := Version{
			Runc:   "1.0.0-rc9",
			Commit: "",
			Spec:   "1.0.1-dev",
		}
		testParseVersion(t, input, expected)
	})

	t.Run("Oneline", func(t *testing.T) {
		input := `runc version 1.0.0-rc8+dev
`
		expected := Version{
			Runc:   "1.0.0-rc8+dev",
			Commit: "",
			Spec:   "",
		}
		testParseVersion(t, input, expected)
	})

	t.Run("Garbage", func(t *testing.T) {
		input := `Garbage
spec: nope
`
		expected := Version{
			Runc:   "",
			Commit: "",
			Spec:   "",
		}
		testParseVersion(t, input, expected)
	})
}

func TestParallelCmds(t *testing.T) {
	rc := &Runc{
		// we don't need a real runc, we just want to test running a caller of cmdOutput in parallel
		Command: "/bin/true",
	}
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 256; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// We just want to fail if there is a race condition detected by
			// "-race", so we ignore the (expected) error here.
			_, _ = rc.Version(ctx)
		}()
	}
	wg.Wait()
}

func TestRuncRunExit(t *testing.T) {
	ctx := context.Background()
	okRunc := &Runc{
		Command: "/bin/true",
	}

	status, err := okRunc.Run(ctx, "fake-id", "fake-bundle", &CreateOpts{})
	if err != nil {
		t.Fatalf("Unexpected error from Run: %s", err)
	}
	if status != 0 {
		t.Fatalf("Expected exit status 0 from Run, got %d", status)
	}

	failRunc := &Runc{
		Command: "/bin/false",
	}

	status, err = failRunc.Run(ctx, "fake-id", "fake-bundle", &CreateOpts{})
	if err == nil {
		t.Fatal("Expected error from Run, but got nil")
	}
	if status != 1 {
		t.Fatalf("Expected exit status 1 from Run, got %d", status)
	}
	extractedStatus := extractStatus(err)
	if extractedStatus != status {
		t.Fatalf("Expected extracted exit status %d from Run, got %d", status, extractedStatus)
	}
}

func TestRuncExecExit(t *testing.T) {
	ctx := context.Background()
	okRunc := &Runc{
		Command: "/bin/true",
	}
	err := okRunc.Exec(ctx, "fake-id", specs.Process{}, &ExecOpts{})
	if err != nil {
		t.Fatalf("Unexpected error from Exec: %s", err)
	}
	status := extractStatus(err)
	if status != 0 {
		t.Fatalf("Expected exit status 0 from Exec, got %d", status)
	}

	failRunc := &Runc{
		Command: "/bin/false",
	}

	err = failRunc.Exec(ctx, "fake-id", specs.Process{}, &ExecOpts{})
	if err == nil {
		t.Fatal("Expected error from Exec, but got nil")
	}
	status = extractStatus(err)
	if status != 1 {
		t.Fatalf("Expected exit status 1 from Exec, got %d", status)
	}

	io, err := NewSTDIO()
	if err != nil {
		t.Fatalf("Unexpected error from NewSTDIO: %s", err)
	}
	err = failRunc.Exec(ctx, "fake-id", specs.Process{}, &ExecOpts{
		IO: io,
	})
	if err == nil {
		t.Fatal("Expected error from Exec, but got nil")
	}
	status = extractStatus(err)
	if status != 1 {
		t.Fatalf("Expected exit status 1 from Exec, got %d", status)
	}
}

func TestRuncStarted(t *testing.T) {
	ctx, timeout := context.WithTimeout(context.Background(), 10*time.Second)
	defer timeout()

	dummyCommand, err := dummySleepRunc()
	if err != nil {
		t.Fatalf("Failed to create dummy sleep runc: %s", err)
	}
	defer os.Remove(dummyCommand)
	sleepRunc := &Runc{
		Command: dummyCommand,
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	started := make(chan int)
	wg.Add(1)
	go func() {
		defer wg.Done()
		interrupt(ctx, t, started)
	}()
	status, err := sleepRunc.Run(ctx, "fake-id", "fake-bundle", &CreateOpts{
		Started: started,
	})
	if err == nil {
		t.Fatal("Expected error from Run, but got nil")
	}
	if status != -1 {
		t.Fatalf("Expected exit status 0 from Run, got %d", status)
	}

	started = make(chan int)
	wg.Add(1)
	go func() {
		defer wg.Done()
		interrupt(ctx, t, started)
	}()
	err = sleepRunc.Exec(ctx, "fake-id", specs.Process{}, &ExecOpts{
		Started: started,
	})
	if err == nil {
		t.Fatal("Expected error from Exec, but got nil")
	}
	status = extractStatus(err)
	if status != -1 {
		t.Fatalf("Expected exit status -1 from Exec, got %d", status)
	}

	started = make(chan int)
	wg.Add(1)
	go func() {
		defer wg.Done()
		interrupt(ctx, t, started)
	}()
	io, err := NewSTDIO()
	if err != nil {
		t.Fatalf("Unexpected error from NewSTDIO: %s", err)
	}
	err = sleepRunc.Exec(ctx, "fake-id", specs.Process{}, &ExecOpts{
		IO:      io,
		Started: started,
	})
	if err == nil {
		t.Fatal("Expected error from Exec, but got nil")
	}
	status = extractStatus(err)
	if status != -1 {
		t.Fatalf("Expected exit status 1 from Exec, got %d", status)
	}
}

func extractStatus(err error) int {
	if err == nil {
		return 0
	}
	var exitError *ExitError
	if errors.As(err, &exitError) {
		return exitError.Status
	}
	return -1
}

// interrupt waits for the pid over the started channel then sends a
// SIGINT to the process.
func interrupt(ctx context.Context, t *testing.T, started <-chan int) {
	select {
	case <-ctx.Done():
		t.Fatal("Timed out waiting for started message")
	case pid, ok := <-started:
		if !ok {
			t.Fatal("Started channel closed without sending pid")
		}
		process, _ := os.FindProcess(pid)
		defer process.Release()
		err := process.Signal(syscall.SIGINT)
		if err != nil {
			t.Fatalf("Failed to send SIGINT to %d: %s", pid, err)
		}
	}
}

// dummySleepRunc creates s simple script that just runs `sleep 10` to replace
// runc for testing process that are longer running.
func dummySleepRunc() (_ string, err error) {
	fh, err := ioutil.TempFile("", "*.sh")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(fh.Name())
		}
	}()
	_, err = fh.Write([]byte("#!/bin/sh\nexec /bin/sleep 10"))
	if err != nil {
		return "", err
	}
	err = fh.Close()
	if err != nil {
		return "", err
	}
	err = os.Chmod(fh.Name(), 0755)
	if err != nil {
		return "", err
	}
	return fh.Name(), nil
}
