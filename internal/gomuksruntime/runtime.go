package gomuksruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.mau.fi/gomuks/pkg/gomuks"
	"go.mau.fi/gomuks/pkg/hicli"

	"github.com/batuhan/gomuks-beeper-api/internal/config"
)

type Runtime struct {
	cfg config.Config
	gmx *gomuks.Gomuks
}

func New(cfg config.Config) (*Runtime, error) {
	stateDir, err := filepath.Abs(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve state dir: %w", err)
	}
	cfg.StateDir = stateDir
	return &Runtime{cfg: cfg}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if r.gmx != nil {
		return nil
	}
	gmx := gomuks.NewGomuks()
	gmx.DisableAuth = true

	oldRoot, hadRoot := os.LookupEnv("GOMUKS_ROOT")
	if err := os.Setenv("GOMUKS_ROOT", r.cfg.StateDir); err != nil {
		return fmt.Errorf("failed to set GOMUKS_ROOT: %w", err)
	}
	gmx.InitDirectories()
	if hadRoot {
		_ = os.Setenv("GOMUKS_ROOT", oldRoot)
	} else {
		_ = os.Unsetenv("GOMUKS_ROOT")
	}

	if err := gmx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load gomuks config: %w", err)
	}
	gmx.SetupLog()
	if code := gmx.StartClientWithoutExit(ctx); code != 0 {
		return fmt.Errorf("failed to start gomuks client (exit code %d)", code)
	}
	gmx.Log.Info().Str("state_dir", r.cfg.StateDir).Msg("gomuks runtime started")
	r.gmx = gmx
	return nil
}

func (r *Runtime) Stop() {
	if r.gmx != nil {
		r.gmx.DirectStop()
	}
}

func (r *Runtime) Client() *hicli.HiClient {
	if r.gmx == nil {
		return nil
	}
	return r.gmx.Client
}

func (r *Runtime) StateDir() string {
	return r.cfg.StateDir
}

func (r *Runtime) SubscribeEvents(handler func(any)) (func(), error) {
	if handler == nil {
		return nil, errors.New("handler is required")
	}
	if r.gmx == nil || r.gmx.EventBuffer == nil {
		return nil, errors.New("gomuks runtime is not started")
	}

	listenerID, _ := r.gmx.EventBuffer.Subscribe(0, nil, func(evt *gomuks.BufferedEvent) {
		if evt == nil {
			return
		}
		handler(evt.Data)
	})
	return func() {
		if r.gmx != nil && r.gmx.EventBuffer != nil {
			r.gmx.EventBuffer.Unsubscribe(listenerID)
		}
	}, nil
}
