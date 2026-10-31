package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultPort = 8750

type Config struct {
	OutputFolder string `json:"output_folder"`
	Format       string `json:"format"`
	Proxy        string `json:"proxy"`
	DailyCap     int    `json:"daily_cap"`
	Port         int    `json:"port"`
}

func Defaults() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return Config{
		OutputFolder: filepath.Join(home, "Downloads"),
		Format:       "original",
		DailyCap:     30,
		Port:         DefaultPort,
	}
}

func Load() (Config, error) {
	cfg := Defaults()
	path, err := filePath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("invalid config, using defaults: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Defaults(), fmt.Errorf("invalid config, using defaults: %w", err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.OutputFolder == "" {
		return fmt.Errorf("output folder is required")
	}
	if c.Format != "original" && c.Format != "mp3" && c.Format != "m4a" && c.Format != "opus" {
		return fmt.Errorf("unsupported format %q", c.Format)
	}
	if c.DailyCap < 1 {
		return fmt.Errorf("daily cap must be positive")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func (c Config) Save() error {
	if err := c.Validate(); err != nil {
		return err
	}
	path, err := filePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func filePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "ytaudio", "config.json"), nil
}
