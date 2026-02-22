package config

import (
	"fmt"
	"log/slog"

	"github.com/kelseyhightower/envconfig"
)

type BaseEnv struct {
	Env      string `envconfig:"ENV" default:"local"`
	HTTPHost string `envconfig:"HTTP_HOST" default:""`
	HTTPPort string `envconfig:"HTTP_PORT" default:"3100"`
	LogLevel string `envconfig:"LOG_LEVEL" default:"debug"`
	APIKey   string `envconfig:"API_KEY" required:"true"`
}

type StorageEnv struct {
	Type    string `envconfig:"STORAGE_TYPE" default:"local"`
	BaseDir string `envconfig:"STORAGE_BASE_DIR" default:".taskguild/data"`
	// S3 settings (used when Type == "s3")
	S3Bucket string `envconfig:"S3_BUCKET"`
	S3Prefix string `envconfig:"S3_PREFIX" default:"taskguild/"`
	S3Region string `envconfig:"S3_REGION" default:"ap-northeast-1"`
}

type Env struct {
	BaseEnv
	StorageEnv
}

const namespace = "TASKGUILD"

func LoadEnv() (*Env, error) {
	var env Env
	if err := envconfig.Process(namespace, &env); err != nil {
		return nil, fmt.Errorf("failed to load env: %w", err)
	}
	return &env, nil
}

func (e *BaseEnv) SlogLevel() slog.Level {
	if e == nil {
		return slog.LevelDebug
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(e.LogLevel)); err != nil {
		return slog.LevelDebug
	}
	return level
}

func BaseEnvFromEnv(env *Env) *BaseEnv {
	return &env.BaseEnv
}

func StorageEnvFromEnv(env *Env) *StorageEnv {
	return &env.StorageEnv
}
