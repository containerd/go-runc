package runc

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var Monitor ProcessMonitor = DefaultMonitor(unix.SIGTERM, 10*time.Second)

type Exit struct {
	Timestamp time.Time
	Pid       int
	Status    int
	Signal    os.Signal
}

// ProcessMonitor is an interface for process monitoring
//
// It allows daemons using go-runc to have a SIGCHLD handler
// to handle exits without introducing races between the handler
// and go's exec.Cmd
// These methods should match the methods exposed by exec.Cmd to provide
// a consistent experience for the caller
type ProcessMonitor interface {
	Start(context.Context, *exec.Cmd) (chan Exit, error)
	Wait(*exec.Cmd, chan Exit) (int, error)
}

func DefaultMonitor(defaultSignal os.Signal, killTimeout time.Duration) ProcessMonitor {
	return &defaultMonitor{
		defaultSignal: defaultSignal,
		killTimeout:   killTimeout,
	}
}

type defaultMonitor struct {
	defaultSignal os.Signal
	killTimeout   time.Duration
}

func (m *defaultMonitor) Start(ctx context.Context, c *exec.Cmd) (chan Exit, error) {
	if err := c.Start(); err != nil {
		return nil, err
	}
	ec := make(chan Exit, 1)
	waitDone := make(chan struct{}, 1)
	go func() {
		select {
		case <-ctx.Done():
			if m.defaultSignal == nil {
				c.Process.Signal(unix.SIGKILL)
			} else {
				c.Process.Signal(m.defaultSignal)
				if m.killTimeout > 0 {
					select {
					case <-time.After(m.killTimeout):
						c.Process.Kill()
					case <-waitDone:
					}
				}
			}
		case <-waitDone:
		}
	}()
	go func() {
		var status int
		var signal os.Signal
		if err := c.Wait(); err != nil {
			status = 255
			if exitErr, ok := err.(*exec.ExitError); ok {
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					status = ws.ExitStatus()
					signal = ws.Signal()
				}
			}
		}
		ec <- Exit{
			Timestamp: time.Now(),
			Pid:       c.Process.Pid,
			Status:    status,
			Signal:    signal,
		}
		close(ec)
		close(waitDone)
	}()
	return ec, nil
}

func (m *defaultMonitor) Wait(c *exec.Cmd, ec chan Exit) (int, error) {
	e := <-ec
	return e.Status, nil
}
