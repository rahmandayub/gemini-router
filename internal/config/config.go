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

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if len(cfg.Gemini.APIKeys) == 0 {
		return fmt.Errorf("at least one API key is required")
	}

	if cfg.Gemini.BaseURL == "" {
		return fmt.Errorf("gemini base_url is required")
	}

	if cfg.Server.Port <= 0 {
		cfg.Server.Port = 18080
	}

	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}

	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}

	return nil
}
