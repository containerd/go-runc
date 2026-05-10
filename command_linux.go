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
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func (r *Runc) command(context context.Context, args ...string) *exec.Cmd {
	command := r.Command
	if command == "" {
		command = DefaultCommand
	}
	if r.CommandFile != nil {
		// Use the file's name as argv[0] and for the initial path; it may be
		// a path that no longer exists on disk. finalizeCommand replaces
		// cmd.Path with /proc/self/fd/<n> before the process is started.
		if name := r.CommandFile.Name(); name != "" {
			command = name
		}
	}
	cmd := exec.CommandContext(context, command, append(r.args(), args...)...)
	if r.CommandFile != nil {
		// Suppress any path-lookup error: the binary will be exec'd via
		// /proc/self/fd/<n> (set by finalizeCommand) so accessibility of the
		// original path at this point doesn't matter.
		cmd.Err = nil
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: r.Setpgid,
	}
	cmd.Env = filterEnv(os.Environ(), "NOTIFY_SOCKET") // NOTIFY_SOCKET introduces a special behavior in runc but should only be set if invoked from systemd
	if r.PdeathSignal != 0 {
		cmd.SysProcAttr.Pdeathsig = r.PdeathSignal
	}

	return cmd
}

// finalizeCommand sets cmd.Path to /proc/self/fd/<n> and appends
// r.CommandFile to cmd.ExtraFiles so that the runc binary is executed
// from the open file descriptor rather than by filesystem path. This is
// semantically equivalent to execveat(fd, "", argv, env, AT_EMPTY_PATH).
//
// Appending the file last preserves the FD positions of any ExtraFiles
// already present (e.g. for --preserve-fds or --status-fd), which are
// therefore unaffected by use of CommandFile.
//
// Must be called after all modifications to cmd.ExtraFiles are complete
// and before cmd.Start(). startCommand calls it automatically.
func (r *Runc) finalizeCommand(cmd *exec.Cmd) {
	if r.CommandFile == nil {
		return
	}
	fdNum := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, r.CommandFile)
	cmd.Path = fmt.Sprintf("/proc/self/fd/%d", fdNum)
}

func filterEnv(in []string, names ...string) []string {
	out := make([]string, 0, len(in))
loop0:
	for _, v := range in {
		for _, k := range names {
			if strings.HasPrefix(v, k+"=") {
				continue loop0
			}
		}
		out = append(out, v)
	}
	return out
}
