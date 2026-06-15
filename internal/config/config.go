package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Gemini  GeminiConfig  `yaml:"gemini"`
	Logging LoggingConfig `yaml:"logging"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type GeminiConfig struct {
	BaseURL string   `yaml:"base_url"`
	APIKeys []string `yaml:"api_keys"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	SetDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SetDefaults applies default values to any unset fields on cfg.
// It mutates cfg in place; callers that need an immutable config should
// pass a copy.
func SetDefaults(cfg *Config) {
	if cfg.Server.Port <= 0 {
		cfg.Server.Port = 18080
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
}

// Validate ensures the loaded configuration is internally consistent.
// It does not mutate cfg; defaults should be applied via SetDefaults
// before calling Validate.
func Validate(cfg *Config) error {
	if len(cfg.Gemini.APIKeys) == 0 {
		return fmt.Errorf("at least one API key is required")
	}
	if cfg.Gemini.BaseURL == "" {
		return fmt.Errorf("gemini base_url is required")
	}
	return nil
}
