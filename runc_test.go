package runc

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestParseVersion(t *testing.T) {
	const data = `runc version 1.0.0-rc3
commit: 17f3e2a07439a024e54566774d597df9177ee216
spec: 1.0.0-rc5-dev`

	v, err := parseVersion([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if v.Runc != "1.0.0-rc3" {
		t.Errorf("expected runc version 1.0.0-rc3 but received %s", v.Runc)
	}
	if v.Commit != "17f3e2a07439a024e54566774d597df9177ee216" {
		t.Errorf("expected commit 17f3e2a07439a024e54566774d597df9177ee216 but received %s", v.Commit)
	}
	if v.Spec != "1.0.0-rc5-dev" {
		t.Errorf("expected spec version 1.0.0-rc5-dev but received %s", v.Spec)
	}

}

func TestExecCancel(t *testing.T) {
	r := Runc{}

	r.Command = "docker-runc"

	containers, err := r.List(context.Background())
	if err != nil || len(containers) == 0 {
		t.Skip("No running container found", err)
	}

	c := containers[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)

	go func() {
		result <- r.Exec(ctx, c.ID, specs.Process{
			Cwd:  "/",
			Args: []string{"/bin/sleep", "10"},
		}, &ExecOpts{})
	}()

	time.AfterFunc(3*time.Second, cancel)

	err = <-result

	if err == nil {
		t.Error("Expect error")
	}

	if !strings.HasPrefix(
		err.Error(),
		fmt.Sprintf("exec failed with exit code %d", 128+syscall.SIGTERM),
	) {
		t.Error("Wrong return code")
	}
}
