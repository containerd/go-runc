package runc

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Runc is the client to the runc cli
type Runc struct {
	Root  string
	Debug bool
}

// List returns all containers created inside the provided runc root directory
func (r *Runc) List() ([]*Container, error) {
	data, err := r.command("list", "--format=json").Output()
	if err != nil {
		return nil, err
	}
	var out []*Container
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// State returns the state for the container provided by id
func (r *Runc) State(id string) (*Container, error) {
	data, err := r.command("state", id).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %s", err, data)
	}
	var c Container
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

type CreateOpts struct {
	IO
	// PidFile is a path to where a pid file should be created
	PidFile      string
	Console      string
	Detach       bool
	NoPivot      bool
	NoNewKeyring bool
}

type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (o IO) setSTDIO(cmd *exec.Cmd) {
	cmd.Stdin = o.Stdin
	cmd.Stdout = o.Stdout
	cmd.Stderr = o.Stderr
}

func (o *CreateOpts) args() (out []string) {
	if o.PidFile != "" {
		out = append(out, "--pid-file", o.PidFile)
	}
	if o.Console != "" {
		out = append(out, "--console", o.Console)
	}
	if o.NoPivot {
		out = append(out, "--no-pivot")
	}
	if o.NoNewKeyring {
		out = append(out, "--no-new-keyring")
	}
	if o.Detach {
		out = append(out, "--detach")
	}
	return out
}

// Create creates a new container and returns its pid if it was created successfully
func (r *Runc) Create(id, bundle string, opts *CreateOpts) error {
	args := []string{"create", "--bundle", bundle}
	if opts != nil {
		args = append(args, opts.args()...)
	}
	cmd := r.command(append(args, id)...)
	if opts != nil {
		opts.setSTDIO(cmd)
	}
	return runOrError(cmd)
}

// Start will start an already created container
func (r *Runc) Start(id string) error {
	return runOrError(r.command("start", id))
}

type ExecOpts struct {
	IO
	Uid    int
	Gid    int
	Cwd    string
	Tty    bool
	Detach bool
}

func (o *ExecOpts) args() (out []string) {
	out = append(out, "--user", fmt.Sprintf("%d:%d", o.Uid, o.Gid))
	if o.Tty {
		out = append(out, "--tty")
	}
	if o.Cwd != "" {
		out = append(out, "--cwd", o.Cwd)
	}
	if o.Detach {
		out = append(out, "--detach")
	}
	return out
}

// Exec executes an additional process inside a container
func (r *Runc) Exec(id string, args []string, opts *ExecOpts) error {
	bargs := []string{"exec"}
	if opts != nil {
		bargs = append(bargs, opts.args()...)
	}
	args = append(bargs, id)
	cmd := r.command(append(bargs, args...)...)
	if opts != nil {
		opts.setSTDIO(cmd)
	}
	return runOrError(cmd)
}

// ExecProcess executres and additional process inside the container based on a full
// OCI Process specification
func (r *Runc) ExecProcess(id string, spec specs.Process, opts *ExecOpts) error {
	f, err := ioutil.TempFile("", "-process")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(spec)
	f.Close()
	if err != nil {
		return err
	}
	args := []string{"exec", "--process", f.Name()}
	if opts != nil {
		args = append(args, opts.args()...)
	}
	cmd := r.command(args...)
	if opts != nil {
		opts.setSTDIO(cmd)
	}
	return runOrError(cmd)
}

// Run runs the create, start, delete lifecycle of the container
// and returns its exit status after it has exited
func (r *Runc) Run(id, bundle string, opts *CreateOpts) (int, error) {
	args := []string{"run", "--bundle", bundle}
	if opts != nil {
		args = append(args, opts.args()...)
	}
	cmd := r.command(append(args, id)...)
	if opts != nil {
		opts.setSTDIO(cmd)
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	status, err := cmd.Process.Wait()
	if err != nil {
		return -1, err
	}
	return status.Sys().(syscall.WaitStatus).ExitStatus(), nil
}

// Delete deletes the container
func (r *Runc) Delete(id string) error {
	return r.command("delete", id).Run()
}

// Kill sends the specified signal to the container
func (r *Runc) Kill(id string, sig int) error {
	return r.command("kill", id, strconv.Itoa(sig)).Run()
}

// Stats return the stats for a container like cpu, memory, and io
func (r *Runc) Stats(id string) (*Stats, error) {
	cmd := r.command("events", "--stats", id)
	rd, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer func() {
		rd.Close()
		cmd.Wait()
	}()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var e Event
	if err := json.NewDecoder(rd).Decode(&e); err != nil {
		return nil, err
	}
	return e.Stats, nil
}

// Events returns an event stream from runc for a container with stats and OOM notifications
func (r *Runc) Events(id string, interval time.Duration) (chan *Event, error) {
	cmd := r.command("events", fmt.Sprintf("--interval=%ds", int(interval.Seconds())), id)
	rd, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		rd.Close()
		return nil, err
	}
	var (
		dec = json.NewDecoder(rd)
		c   = make(chan *Event, 128)
	)
	go func() {
		defer func() {
			close(c)
			rd.Close()
			cmd.Wait()
		}()
		for {
			var e Event
			if err := dec.Decode(&e); err != nil {
				if err == io.EOF {
					return
				}
				logrus.WithError(err).Error("runc: decode event")
				continue
			}
			c <- &e
		}
	}()
	return c, nil
}

func (r *Runc) args() (out []string) {
	if r.Root != "" {
		out = append(out, "--root", r.Root)
	}
	if r.Debug {
		out = append(out, "--debug")
	}
	return out
}

func (r *Runc) command(args ...string) *exec.Cmd {
	return exec.Command("runc", append(r.args(), args...)...)
}
