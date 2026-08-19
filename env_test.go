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
	"reflect"
	"testing"
)

func TestWithExtraEnv(t *testing.T) {
	if ctx := context.Background(); WithExtraEnv(ctx) != ctx {
		t.Error("expected an empty call to return the context unchanged")
	}

	want := func(ctx context.Context, expected ...string) {
		t.Helper()
		if got := extraEnv(ctx); !reflect.DeepEqual(got, expected) {
			t.Errorf("expected %v, got %v", expected, got)
		}
	}

	// A parent holding a slice with spare capacity: appending in place would
	// let contexts derived from it overwrite each other's entries.
	shared := append(make([]string, 0, 4), "A=1")
	parent := context.WithValue(context.Background(), extraEnvKey{}, shared)

	// Derive both before reading either: in-place appends only become visible
	// as corruption once the second one has overwritten the first.
	a, b := WithExtraEnv(parent, "B=2"), WithExtraEnv(parent, "C=3")

	want(a, "A=1", "B=2")
	want(b, "A=1", "C=3")
	want(parent, "A=1")
}

func TestCommandExtraEnv(t *testing.T) {
	t.Setenv("GO_RUNC_TEST", "inherited")

	r := &Runc{}
	plain := r.command(context.Background(), "--version").Env
	env := r.command(WithExtraEnv(context.Background(), "GO_RUNC_TEST=injected"), "--version").Env

	// Appended last, so exec resolves the duplicate in favour of the injection.
	if len(env) != len(plain)+1 || env[len(env)-1] != "GO_RUNC_TEST=injected" {
		t.Fatalf("expected the injected entry appended last, got %v", env[len(plain):])
	}
}
