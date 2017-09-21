package runc

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMonitorCustomSignal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.Command("sleep", "10")
	monitor := DefaultMonitor(unix.SIGTERM, time.Second)
	ec, err := monitor.Start(ctx, cmd)
	if err != nil {
		t.Errorf("Failed to start command: %v", err)
	}
	e := <-ec
	if e.Signal != unix.SIGTERM {
		t.Errorf("Got signal (%v), expected (%v)", e.Signal, unix.SIGTERM)
	}
}

func TestMonitorKill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.Command("sleep", "10")
	monitor := &defaultMonitor{}
	ec, err := monitor.Start(ctx, cmd)
	if err != nil {
		t.Errorf("Failed to start command: %v", err)
	}
	e := <-ec
	if e.Signal != unix.SIGKILL {
		t.Errorf("Got signal (%v), expected (%v)", e.Signal, unix.SIGTERM)
	}
}
