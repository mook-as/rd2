// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !windows || !amd64

package appxinstall

// Install always fails on this platform; see ErrUnsupported and the package
// doc comment. The go-bindings-winrt library this feature depends on only
// ships windows/amd64 bindings today.
func Install(packagePath string, opts Options) error {
	return ErrUnsupported
}
