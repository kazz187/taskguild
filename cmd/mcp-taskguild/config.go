package main

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

const envPrefix = "MCPTG"

type Config struct {
	TaskGuildAddr string `envconfig:"TASKGUILD_ADDR" default:"http://localhost:8080"`
}

func NewConfig() (*Config, error) {
	c := &Config{}
	err := envconfig.Process(envPrefix, c)
	if err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}
	return c, nil
}
