// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package binlinks publishes the binaries bundled with the Rancher Desktop
// application into the instance bin directory (~/.rd<instance>/bin) as
// symlinks, so a user can put that directory on PATH. It acts only when rdd
// runs from inside the application bundle, so a standalone CLI install never
// disturbs links the application created.
package binlinks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

// LinkBundledBinaries publishes the application's bundled binaries into the
// instance bin directory as symlinks. It is a no-op unless rdd runs from inside
// the application bundle. Publishing is best-effort: the returned error is for
// the caller to log and must not block startup.
func LinkBundledBinaries() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	if !inAppBundle(execPath, runtime.GOOS) {
		return nil
	}
	return linkBinaries(execPath, filepath.Join(instance.ShortDir(), "bin"))
}

// inAppBundle reports whether execPath is the bundled rdd binary for the given
// OS, as opposed to a standalone CLI install. The application stages its
// per-platform resources under Resources/<goos>/bin/rdd. The leading separator
// anchors the match, so an unrelated path ending in "Resources/<goos>/bin/rdd"
// does not qualify.
func inAppBundle(execPath, goos string) bool {
	return strings.HasSuffix(execPath, "/Resources/"+goos+"/bin/rdd")
}

// linkBinaries recreates binDir with symlinks to the bundled binaries and a
// kubectl link to rdd. Recreating it drops stale links from a previous install;
// reading the source directory before removing binDir keeps the existing links
// when the read fails.
func linkBinaries(execPath, binDir string) error {
	srcDir := filepath.Dir(execPath)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read bundle directory %q: %w", srcDir, err)
	}

	if err := os.RemoveAll(binDir); err != nil {
		return fmt.Errorf("remove %q: %w", binDir, err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", binDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if err := os.Symlink(filepath.Join(srcDir, name), filepath.Join(binDir, name)); err != nil {
			return fmt.Errorf("link %q: %w", name, err)
		}
	}

	// No separate kubectl binary is bundled; rdd provides it. Link kubectl to
	// rdd so kubectl on PATH reaches rdd.
	if err := os.Symlink(execPath, filepath.Join(binDir, "kubectl")); err != nil {
		return fmt.Errorf("link kubectl: %w", err)
	}
	return nil
}
