// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package wsl wraps the wsl.exe commands rdd uses to manage the WSL2 distros
// that back Lima instances on Windows. Every command is a no-op off Windows,
// where Lima creates no WSL2 distros.
package wsl

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// wsl.exe can hang when the WSL subsystem is degraded, so each call is
// time-bounded. --unregister is slower than --terminate.
const (
	terminateTimeout  = 10 * time.Second
	unregisterTimeout = 30 * time.Second
)

// DistroName returns the WSL2 distro name Lima registers for instName. Lima's
// WSL2 driver names each distro "lima-<instance>".
func DistroName(instName string) string {
	return "lima-" + instName
}

// Terminate runs `wsl.exe --terminate` to shut the distro down, releasing the
// kernel state that can make a following --unregister deadlock. No-op off
// Windows.
func Terminate(ctx context.Context, distroName string) error {
	return run(ctx, terminateTimeout, "--terminate", distroName)
}

// Unregister runs `wsl.exe --unregister`, dropping the WSL2 registration and
// the distro's ext4.vhdx so Lima imports a fresh distro on the next start.
// Terminate the distro first. No-op off Windows.
func Unregister(ctx context.Context, distroName string) error {
	return run(ctx, unregisterTimeout, "--unregister", distroName)
}

func run(ctx context.Context, timeout time.Duration, verb, distroName string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	wslCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := exec.CommandContext(wslCtx, "wsl.exe", verb, distroName).Run(); err != nil {
		return fmt.Errorf("wsl.exe %s %q: %w", verb, distroName, err)
	}
	return nil
}
