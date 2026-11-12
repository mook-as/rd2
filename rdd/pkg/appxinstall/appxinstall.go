// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package appxinstall

import "errors"

// ErrUnsupported is returned by Install when the current platform has no
// implementation available (anything other than windows/amd64; see package
// doc for why windows/arm64 is not yet supported).
var ErrUnsupported = errors.New("appx install is not supported on this platform")

// Options controls how Install drives the package upgrade.
type Options struct {
	// ForceAppShutdown allows the install to terminate running processes of
	// the package being replaced (the equivalent of
	// Add-AppxPackage -ForceApplicationShutdown).
	ForceAppShutdown bool
}
