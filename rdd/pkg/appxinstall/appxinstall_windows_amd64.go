// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build windows && amd64

package appxinstall

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/management/deployment"
)

// Install registers the APPX package at packagePath as an in-place upgrade
// of the currently-installed package, via
// Windows.Management.Deployment.PackageManager.AddPackageByUriAsync. This
// replaces spawning `Add-AppxPackage` through powershell.exe, which may be
// unavailable or constrained by policy in locked-down environments.
//
// packagePath must be an absolute path to a local .appx file; it is
// converted to a file:// URI internally.
func Install(packagePath string, opts Options) error {
	absPath, err := filepath.Abs(packagePath)
	if err != nil {
		return fmt.Errorf("resolving absolute path for %q: %w", packagePath, err)
	}

	packageUri, err := foundation.CreateUri(pathToFileURI(absPath))
	if err != nil {
		return fmt.Errorf("creating file URI for %q: %w", absPath, err)
	}
	defer packageUri.Release()

	addOptions, err := deployment.NewAddPackageOptions()
	if err != nil {
		return fmt.Errorf("creating AddPackageOptions: %w", err)
	}
	defer addOptions.Release()

	if err := addOptions.SetForceAppShutdown(opts.ForceAppShutdown); err != nil {
		return fmt.Errorf("setting ForceAppShutdown: %w", err)
	}
	// The package being replaced is the one running rdd.exe itself; make sure
	// that gets shut down too, not just unrelated apps that happen to share
	// the package family.
	if err := addOptions.SetForceTargetAppShutdown(opts.ForceAppShutdown); err != nil {
		return fmt.Errorf("setting ForceTargetAppShutdown: %w", err)
	}

	manager, err := deployment.NewPackageManager()
	if err != nil {
		return fmt.Errorf("creating PackageManager: %w", err)
	}
	defer manager.Release()

	manager9, err := manager.AsPackageManager9()
	if err != nil {
		return fmt.Errorf("querying IPackageManager9: %w", err)
	}
	defer manager9.Release()

	operation, err := manager9.AddPackageByUriAsync(&packageUri.IUriRuntimeClass, &addOptions.IAddPackageOptions)
	if err != nil {
		return fmt.Errorf("starting AddPackageByUriAsync: %w", err)
	}
	defer operation.Release()

	result, err := operation.Await()
	if err != nil {
		return fmt.Errorf("installing %q: %w", absPath, err)
	}
	defer result.Release()

	if errorText, textErr := result.ErrorText(); textErr == nil && errorText != "" {
		return fmt.Errorf("installing %q: %s", absPath, errorText)
	}

	return nil
}

// pathToFileURI converts an absolute Windows filesystem path (e.g.
// `C:\Users\me\update.appx`) to a file:// URI understood by
// Windows.Foundation.Uri (e.g. `file:///C:/Users/me/update.appx`).
func pathToFileURI(path string) string {
	pathname := filepath.ToSlash(path)
	if !strings.HasPrefix(pathname, "/") {
		pathname = "/" + pathname
	}
	return (&url.URL{Scheme: "file", Path: pathname}).String()
}
