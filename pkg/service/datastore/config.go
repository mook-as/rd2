// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package datastore

import (
	"context"

	"github.com/k3s-io/kine/pkg/endpoint"
)

type Config struct {
	EndpointConfig endpoint.Config
}

type completedConfig struct {
	*Config
}

type CompletedConfig struct {
	*completedConfig
}

func NewConfig(o CompletedOptions) (*Config, error) {
	return &Config{
		EndpointConfig: o.EndpointConfig,
	}, nil
}

func (c *Config) Complete() CompletedConfig {
	return CompletedConfig{&completedConfig{
		Config: c,
	}}
}

type Server struct {
	config CompletedConfig
}

func NewServer(config CompletedConfig) *Server {
	if config.Config == nil {
		return nil
	}
	return &Server{
		config: config,
	}
}

// Run starts the embedded etcd server. It blocks until it is ready for up to a minute.
func (s *Server) Run(ctx context.Context) error {
	_, err := endpoint.Listen(ctx, s.config.EndpointConfig)
	// TODO: is the endpoint guaranteed to be ready by the time Listen() returns?
	return err
}
