package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr          string
	AccessToken         string
	StateDir            string
	AllowQueryTokenAuth bool
	BeeperHomeserverURL string
	BeeperLoginToken    string
	BeeperUsername      string
	BeeperPassword      string
	BeeperRecoveryKey   string
}

const (
	defaultListenAddr          = "127.0.0.1:23373"
	defaultBeeperHomeserverURL = "https://matrix.beeper.com"
)

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	cfg := Config{
		ListenAddr:          getenvDefault("BEEPER_API_LISTEN", defaultListenAddr),
		AccessToken:         os.Getenv("BEEPER_ACCESS_TOKEN"),
		AllowQueryTokenAuth: os.Getenv("BEEPER_ALLOW_QUERY_TOKEN") == "true",
		BeeperHomeserverURL: getenvDefault("BEEPER_HOMESERVER_URL", defaultBeeperHomeserverURL),
		BeeperLoginToken:    os.Getenv("BEEPER_LOGIN_TOKEN"),
		BeeperUsername:      os.Getenv("BEEPER_USERNAME"),
		BeeperPassword:      os.Getenv("BEEPER_PASSWORD"),
		BeeperRecoveryKey:   os.Getenv("BEEPER_RECOVERY_KEY"),
	}
	if (cfg.BeeperUsername == "") != (cfg.BeeperPassword == "") {
		return Config{}, fmt.Errorf("BEEPER_USERNAME and BEEPER_PASSWORD must be provided together")
	}
	if cfg.BeeperLoginToken != "" && cfg.BeeperUsername != "" {
		return Config{}, fmt.Errorf("BEEPER_LOGIN_TOKEN cannot be combined with BEEPER_USERNAME/BEEPER_PASSWORD")
	}
	cfg.StateDir = strings.TrimSpace(os.Getenv("GOMUKS_ROOT"))
	return cfg, nil
}

func getenvDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func loadDotEnv() error {
	err := godotenv.Load()
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("failed to load .env file: %w", err)
}
