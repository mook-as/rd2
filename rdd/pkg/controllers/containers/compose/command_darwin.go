// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package compose

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Spawn the command in a way that ensures the process will be killed when the
// parent process exits.
func (c *concreteCommandExecutor) spawn() error {
	// On darwin, we set the controlling TTY to a PTY, and stash the master end.
	// When the child exits, we close the master end.  If we exit before the child,
	// then the OS closes the master end for us, which sends SIGHUP to the child.

	var success bool
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return fmt.Errorf("failed to open PTY master: %w", err)
	}
	defer func() {
		if success {
			c.cleanup = func() {
				_ = master.Close()
			}
		} else {
			_ = master.Close()
		}
	}()

	if err := unix.IoctlSetInt(int(master.Fd()), unix.TIOCPTYGRANT, 0); err != nil {
		return fmt.Errorf("ioctl TIOCPTYGRANT failed: %w", err)
	}
	if err := unix.IoctlSetInt(int(master.Fd()), unix.TIOCPTYUNLK, 0); err != nil {
		return fmt.Errorf("ioctl TIOCPTYUNLK failed: %w", err)
	}
	var nameBuf [128]byte
	_, _, errno := unix.Syscall(
		//nolint:staticcheck // unix.SYS_IOCTL is deprecated without replacement.
		unix.SYS_IOCTL,
		master.Fd(),
		unix.TIOCPTYGNAME,
		uintptr(unsafe.Pointer(&nameBuf[0])),
	)
	if errno != 0 {
		return fmt.Errorf("ioctl TIOCPTYGNAME failed: %w", errno)
	}

	slavePath, _, found := strings.Cut(string(nameBuf[:]), "\x00")
	if !found {
		return fmt.Errorf("ioctl TIOCPTYGNAME not null terminated: %q", nameBuf)
	}
	slave, err := os.OpenFile(slavePath, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return fmt.Errorf("failed to open PTY slave %q: %w", slavePath, err)
	}

	// Establish the slave as the controlling terminal for the child.
	ttyFD := len(c.Cmd.ExtraFiles) + 3 // child fd is 3+i per .ExtraFiles docs.
	c.Cmd.ExtraFiles = append(c.Cmd.ExtraFiles, slave)
	c.Cmd.SysProcAttr = &unix.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    ttyFD,
	}

	err = c.Cmd.Start()
	// We must close our handle to the slave end in RDD as soon as the
	// child starts, so that when the child exits, the last write handle
	// to the PTY is closed and the master end gets EOF.
	_ = slave.Close()
	if err != nil {
		return err
	}

	go func() {
		_, _ = io.Copy(io.Discard, master)
	}()

	success = true
	return nil
}

// kill implements [command].
func (c *concreteCommandExecutor) kill(ctx context.Context) error {
	process := c.Process
	if process == nil {
		return nil
	}
	// On macOS, since we're using PTYs, we can just close the master end; this
	// causes the child to receive a SIGHUP.
	if cleanup := c.cleanup; cleanup != nil {
		cleanup()
	} else {
		// No cleanup; should not happen.
		err := process.Signal(unix.SIGTERM)
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	killCtx, cancel := context.WithTimeout(ctx, killTimeout)
	defer cancel()
	select {
	case <-c.done:
	case <-killCtx.Done():
		// In case the child is ignoring SIGHUP
		if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	<-c.done
	return nil
}
