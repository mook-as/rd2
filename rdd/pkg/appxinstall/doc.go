// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package appxinstall installs a downloaded APPX update package in place of
// the currently-running one, without needing to shell out to PowerShell
// (which may run in a locked-down execution mode in some corporate
// environments).
//
// On windows/amd64 this calls into
// Windows.Management.Deployment.PackageManager via
// github.com/deploymenttheory/go-bindings-winrt. That library does not yet
// provide windows/arm64 bindings, so on that platform (and on non-Windows
// platforms, for the sake of building/testing elsewhere) Install returns
// ErrUnsupported; callers should fall back to another installation method
// in that case.
package appxinstall
