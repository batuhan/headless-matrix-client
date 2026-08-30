package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr          string
	AccessToken         string
	StateDir            string
	AllowQueryTokenAuth bool
	ManageSecret        string
	MatrixHomeserverURL string
	MatrixLoginToken    string
	MatrixUsername      string
	MatrixPassword      string
	MatrixRecoveryKey   string
	APNSKeyPath         string
	APNSKeyID           string
	APNSTeamID          string
	APNSTopic           string
	APNSEnvironment     string
}

const (
	defaultListenAddr          = "127.0.0.1:23373"
	defaultMatrixHomeserverURL = "https://matrix.beeper.com"
)

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	cfg := Config{
		ListenAddr:          resolveListenAddr(),
		AccessToken:         os.Getenv("MATRIX_ACCESS_TOKEN"),
		AllowQueryTokenAuth: os.Getenv("MATRIX_ALLOW_QUERY_TOKEN") == "true",
		ManageSecret:        strings.TrimSpace(os.Getenv("EASYMATRIX_MANAGE_SECRET")),
		MatrixHomeserverURL: getenvDefault("MATRIX_HOMESERVER_URL", defaultMatrixHomeserverURL),
		MatrixLoginToken:    os.Getenv("MATRIX_LOGIN_TOKEN"),
		MatrixUsername:      os.Getenv("MATRIX_USERNAME"),
		MatrixPassword:      os.Getenv("MATRIX_PASSWORD"),
		MatrixRecoveryKey:   os.Getenv("MATRIX_RECOVERY_KEY"),
		APNSKeyPath:         strings.TrimSpace(os.Getenv("APNS_KEY_PATH")),
		APNSKeyID:           strings.TrimSpace(os.Getenv("APNS_KEY_ID")),
		APNSTeamID:          strings.TrimSpace(os.Getenv("APNS_TEAM_ID")),
		APNSTopic:           strings.TrimSpace(os.Getenv("APNS_TOPIC")),
		APNSEnvironment:     getenvDefault("APNS_ENVIRONMENT", "sandbox"),
	}
	if (cfg.MatrixUsername == "") != (cfg.MatrixPassword == "") {
		return Config{}, fmt.Errorf("MATRIX_USERNAME and MATRIX_PASSWORD must be provided together")
	}
	if cfg.MatrixLoginToken != "" && cfg.MatrixUsername != "" {
		return Config{}, fmt.Errorf("MATRIX_LOGIN_TOKEN cannot be combined with MATRIX_USERNAME/MATRIX_PASSWORD")
	}
	cfg.StateDir = resolveStateDir()
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

func resolveListenAddr() string {
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return net.JoinHostPort("0.0.0.0", port)
	}
	if listenAddr := strings.TrimSpace(os.Getenv("MATRIX_API_LISTEN")); listenAddr != "" {
		return listenAddr
	}
	return defaultListenAddr
}

func resolveStateDir() string {
	if root := strings.TrimSpace(os.Getenv("GOMUKS_ROOT")); root != "" {
		return root
	}
	if mountPath := strings.TrimSpace(os.Getenv("RAILWAY_VOLUME_MOUNT_PATH")); mountPath != "" {
		return filepath.Join(mountPath, "gomuks")
	}
	return ""
}
