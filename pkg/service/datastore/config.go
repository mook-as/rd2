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

// Run starts the embedded etcd server. endpoint.Listen starts the gRPC server
// in a background goroutine and returns once the listener is bound. Some gRPC
// "server preface" warnings from the kube-apiserver's etcd client are expected
// during the initial connection burst — these resolve via automatic retry and
// do not affect functionality.
func (s *Server) Run(ctx context.Context) error {
	_, err := endpoint.Listen(ctx, s.config.EndpointConfig)
	return err
}
