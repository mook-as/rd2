// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// PIDFD_SIGNAL_PROCESS_GROUP is the flag for [unix.PidfdSendSignal] to send the
// signal to the process group.
const PIDFD_SIGNAL_PROCESS_GROUP = 4

// Spawn the command in a way that ensures the process will be killed when the
// parent process exits.
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
		err := c.Cmd.Start()
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
		innerErr = unix.PidfdSendSignal(int(handle), syscall.SIGTERM, nil, PIDFD_SIGNAL_PROCESS_GROUP)
		if innerErr != nil {
			return
		}
		select {
		case <-c.done:
		case <-killCtx.Done():
			innerErr = unix.PidfdSendSignal(int(handle), syscall.SIGKILL, nil, PIDFD_SIGNAL_PROCESS_GROUP)
		}
	})
	// err may be [os.ErrNoHandle] on obsolete versions of Linux we don't care about.
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		err = innerErr
	}
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-c.done
	return nil
}
