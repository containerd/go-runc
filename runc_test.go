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
	"sync"
	"testing"
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
