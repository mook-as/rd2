// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package composeproject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Spawn the command in a way that ensures the process will be killed when the
// parent process exits.
func (c *concreteCommandExecutor) spawn() error {
	// On Windows, we create a job object and assign the child process to it.
	// This will automatically terminate the child when the parent exits.
	// However, we don't have access to the hooks needed to call
	// UpdateProcThreadAttribute before the child is created, so we have to assign
	// it to the job after it has already started.

	// This will need to be updated after golang 1.28 ships, or whichever release
	// is after https://github.com/golang/go/issues/80415 gets merged.  That
	// enables the process to be assigned to the job before starting.

	var success bool

	hJob, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create job object: %w", err)
	}
	defer func() {
		if success {
			c.cleanup = func() {
				_ = windows.CloseHandle(hJob)
				hJob = windows.InvalidHandle
			}
		} else {
			_ = windows.CloseHandle(hJob)
		}
	}()

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	_, err = windows.SetInformationJobObject(
		hJob,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return fmt.Errorf("failed to set job object information: %w", err)
	}

	if err = c.Cmd.Start(); err != nil {
		return err
	}

	var assignError error
	err = c.Cmd.Process.WithHandle(func(hProcess uintptr) {
		// Assign process to Job Object
		err := windows.AssignProcessToJobObject(hJob, windows.Handle(hProcess))
		if err != nil {
			assignError = fmt.Errorf("failed to assign process to job object: %w", err)
		}
	})
	if err != nil || assignError != nil {
		// At this point, the process is running outside the job.  Abort it.
		return errors.Join(err, assignError, c.Cmd.Process.Kill())
	}

	success = true

	return nil
}

// kill implements [command].
func (c *concreteCommandExecutor) kill(ctx context.Context) error {
	// Since we have a job, we can just close that to kill the child.
	if cleanup := c.cleanup; cleanup != nil {
		cleanup()
	} else {
		// Should not happen; but fall back to using the process directly.
		process := c.Process
		if process == nil {
			return nil
		}
		if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
	}
	killCtx, cancel := context.WithTimeout(ctx, killTimeout)
	defer cancel()
	select {
	case <-c.done:
	case <-killCtx.Done():
		// This shouldn't happen; it's here to ensure `killTimeout` gets used.
		return fmt.Errorf("timed out waiting for process to exit: %w", killCtx.Err())
	}
	return nil
}
