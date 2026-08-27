// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// PidfdSignalProcessGroup is the flag for [unix.PidfdSendSignal] to send the
// signal to the process group.  Introduced in Linux 6.9, released in (and EOLed
// in) 2024; that covers all kernels we want to support, include SLES 16 and WSL.
const PidfdSignalProcessGroup = 4

// Spawn the command in a way that ensures the process will be killed when the
// parent process exits.  If the working directory is not set, a temporary one
// will be created.
func (c *concreteCommandExecutor) spawn() error {
	// On Linux, we use Pdeathsig to send a signal to the child when we exit.
	// However, that means we need to lock the thread.

	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		c.Cmd.SysProcAttr = &syscall.SysProcAttr{
			Pdeathsig: syscall.SIGTERM,
			Setpgid:   true,
		}
		c.cleanup = func() {
			close(done)
		}
		var err error
		if c.Cmd.Dir == "" {
			err = &os.PathError{Op: "chdir"}
		} else {
			err = c.Cmd.Start()
		}
		if pathErr, ok := errors.AsType[*os.PathError](err); ok && pathErr.Op == "chdir" {
			// The working directory is invalid
			var temporaryWorkingDir string
			temporaryWorkingDir, err = os.MkdirTemp("", "rdd-compose-*")
			if err != nil {
				errs <- fmt.Errorf("failed to create temporary working directory: %w", err)
				close(errs)
				return
			}
			defer func() {
				_ = os.RemoveAll(temporaryWorkingDir)
			}()
			c.Cmd.Dir = temporaryWorkingDir
			err = c.Cmd.Start()
		}
		errs <- err
		close(errs)
		if err == nil {
			<-done
		}
	}()

	return <-errs
}

// kill implements [command].
func (c *concreteCommandExecutor) kill(ctx context.Context) error {
	process := c.Process
	if process == nil {
		return nil
	}
	killCtx, cancel := context.WithTimeout(ctx, killTimeout)
	defer cancel()
	var innerErr error
	err := process.WithHandle(func(handle uintptr) {
		innerErr = unix.PidfdSendSignal(int(handle), syscall.SIGTERM, nil, PidfdSignalProcessGroup)
		if innerErr != nil {
			return
		}
		select {
		case <-c.done:
		case <-killCtx.Done():
			innerErr = unix.PidfdSendSignal(int(handle), syscall.SIGKILL, nil, PidfdSignalProcessGroup)
		}
	})
	// err may be [os.ErrNoHandle] on obsolete versions of Linux we don't care
	// about; just return the error if that happens.
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		err = innerErr
	}
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, killTimeout)
	defer waitCancel()
	select {
	case <-c.done:
	case <-waitCtx.Done():
		return fmt.Errorf("timed out waiting for process to be reaped: %w", waitCtx.Err())
	}
	return nil
}
