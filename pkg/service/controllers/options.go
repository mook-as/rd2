// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
// SPDX-FileCopyrightText: The KCP Authors

package controllers

import (
	"github.com/spf13/pflag"
)

// Options holds the controller configuration options.
type Options struct {
	Controllers string // Controller selection specification (--controllers flag)
}

type completedOptions struct {
	Controllers string
}

// CompletedOptions holds the completed controller configuration options.
type CompletedOptions struct {
	*completedOptions
}

// AddFlags adds the flags for the controller options to the given FlagSet.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.StringVar(&o.Controllers, "controllers", "*", "Controllers to enable. Use '*' for all, or specify comma-separated list. API groups: 'rdd' (configmapreplicaset, notary), 'app' (demo). Prefix with '-' to exclude, e.g., '*,-demo'")
}

// Complete returns the completed configuration.
func (o *Options) Complete() CompletedOptions {
	return CompletedOptions{
		&completedOptions{
			Controllers: o.Controllers,
		},
	}
}

// Validate validates the options.
func (c *CompletedOptions) Validate() []error {
	return nil
}

// NewOptions creates a new Options instance.
func NewOptions() *Options {
	return &Options{}
}
