// +build !linux

package runc

import (
	"os/exec"
)

func (r *Runc) command(args ...string) *exec.Cmd {
	command := r.Command
	if command == "" {
		command = DefaultCommand
	}
	return exec.Command(command, append(r.args(), args...)...)
}
