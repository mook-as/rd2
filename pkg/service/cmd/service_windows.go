// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package service

import (
	"fmt"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

func killProcess(pid int) error {
	hProcess, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
		false,
		uint32(pid))
	if err != nil {
		return fmt.Errorf("failed to open process %d: %w", pid, err)
	}
	defer func() {
		_ = windows.CloseHandle(hProcess)
	}()
	if err := windows.TerminateProcess(hProcess, 1); err != nil {
		return fmt.Errorf("failed to terminate process %d: %w", pid, err)
	}
	_, err = windows.WaitForSingleObject(hProcess, uint32(10*time.Second/time.Millisecond))
	if err != nil {
		return fmt.Errorf("timed out waiting for process %d to terminate: %w", pid, err)
	}

	return nil
}

func setCommandGroup(*exec.Cmd) {
	// TODO: implement
}
