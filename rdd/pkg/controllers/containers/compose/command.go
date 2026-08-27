// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/ringbuf"
)

const killTimeout = 5 * time.Second

// command is an interface for a running process, used for testing.
type command interface {
	// args returns the command-line arguments for the process.
	args() []string
	// kill terminates the process; no-op if there is no process, or it is done.
	kill(ctx context.Context) error
	// wait waits for the process to exit and returns any error from the process.
	wait(context.Context) error
	// output returns the stderr output from the process.  This may only be called
	// after the process has exited.
	output() string
}

// commandExecutor starts a process.
type commandExecutor func(ctx context.Context, workingDir, exe string, args ...string) (command, error)

// concreteCommandExecutor is a concrete implementation of command that wraps an
// *exec.Cmd.
type concreteCommandExecutor struct {
	*exec.Cmd
	result error
	done   chan struct{}
	buffer interface{ Bytes() []byte }
	// Cleanup function to call when the owning process exits, if any.
	cleanup func()
}

// args implements [command].
func (c *concreteCommandExecutor) args() []string {
	return c.Args
}

// wait implements [command].
func (c *concreteCommandExecutor) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.result
	}
}

// output implements [command].
func (c *concreteCommandExecutor) output() string {
	select {
	case <-c.done:
		return string(c.buffer.Bytes())
	default:
		panic("output() called before process exited")
	}
}

// defaultCommandExecutor is the default implementation of commandExecutor that
// uses exec.CommandContext to start a process.
//
// Docker is configured to use the RDD docker context.
func defaultCommandExecutor(ctx context.Context, workingDir, exe string, args ...string) (command, error) {
	buf := ringbuf.New(4096)
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = workingDir
	// Remove DOCKER_HOST= because that interferes with DOCKER_CONTEXT.
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(v string) bool {
		key, _, found := strings.Cut(v, "=")
		return found && strings.EqualFold(key, "DOCKER_HOST")
	}), "DOCKER_CONTEXT="+instance.Name())
	cmd.Stderr = buf
	c := &concreteCommandExecutor{
		Cmd:    cmd,
		done:   make(chan struct{}),
		buffer: buf,
	}
	if err := c.spawn(); err != nil {
		return nil, err
	}
	// Call cmd.Wait now to ensure we're doing it somewhere, so the `kill`
	// implementation can assume c.done will be closed when the process exits.
	go func() {
		c.result = cmd.Wait()
		close(c.done)
		if cleanup := c.cleanup; cleanup != nil {
			cleanup()
		}
	}()
	return c, nil
}
