package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ListenAddr          string
	AccessToken         string
	StateDir            string
	AllowQueryTokenAuth bool
}

const (
	defaultListenAddr = "127.0.0.1:23373"
)

func Load() (Config, error) {
	cfg := Config{
		ListenAddr:          getenvDefault("BEEPER_API_LISTEN", defaultListenAddr),
		AccessToken:         os.Getenv("BEEPER_ACCESS_TOKEN"),
		AllowQueryTokenAuth: os.Getenv("BEEPER_ALLOW_QUERY_TOKEN") == "true",
	}
	stateDir := os.Getenv("BEEPER_STATE_DIR")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("failed to resolve home dir: %w", err)
		}
		stateDir = filepath.Join(home, ".local", "share", "gomuks-beeper-api")
	}
	cfg.StateDir = stateDir
	return cfg, nil
}

func getenvDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
