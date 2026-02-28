package gomuksruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/gomuks/pkg/gomuks"
	"go.mau.fi/gomuks/pkg/hicli"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"

	"github.com/batuhan/easymatrix/internal/config"
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
	if err := r.bootstrapSessionFromEnv(ctx, gmx); err != nil {
		return err
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

func (r *Runtime) bootstrapSessionFromEnv(ctx context.Context, gmx *gomuks.Gomuks) error {
	hasPasswordLogin := strings.TrimSpace(r.cfg.BeeperUsername) != "" && strings.TrimSpace(r.cfg.BeeperPassword) != ""
	hasRecoveryKey := strings.TrimSpace(r.cfg.BeeperRecoveryKey) != ""
	if !hasPasswordLogin && !hasRecoveryKey {
		return nil
	}

	cli := gmx.Client
	if cli == nil || cli.Client == nil {
		return errors.New("gomuks client is not initialized")
	}

	state := cli.State()
	if hasPasswordLogin && !state.IsLoggedIn {
		err := runHiCommand(
			ctx,
			cli,
			jsoncmd.ReqLogin,
			&jsoncmd.LoginParams{
				HomeserverURL: r.cfg.BeeperHomeserverURL,
				Username:      r.cfg.BeeperUsername,
				Password:      r.cfg.BeeperPassword,
			},
			nil,
		)
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already logged in") {
			return fmt.Errorf("failed to login using env credentials: %w", err)
		}
		gmx.Log.Info().Str("homeserver_url", r.cfg.BeeperHomeserverURL).Msg("beeper password login completed from environment")
	}

	state = cli.State()
	if hasRecoveryKey && !state.IsVerified {
		if !state.IsLoggedIn {
			return errors.New("BEEPER_RECOVERY_KEY was provided, but no logged-in session is available")
		}
		if err := runHiCommand(
			ctx,
			cli,
			jsoncmd.ReqVerify,
			&jsoncmd.VerifyParams{RecoveryKey: r.cfg.BeeperRecoveryKey},
			nil,
		); err != nil {
			return fmt.Errorf("failed to verify using env recovery key: %w", err)
		}
		gmx.Log.Info().Msg("beeper verification completed from environment")
	}

	return nil
}

func runHiCommand(ctx context.Context, cli *hicli.HiClient, cmd jsoncmd.Name, params any, out any) error {
	var payload json.RawMessage
	if params == nil {
		payload = []byte(`{}`)
	} else {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to encode %s params: %w", cmd, err)
		}
		payload = raw
	}

	resp := cli.SubmitJSONCommand(ctx, &hicli.JSONCommand{
		Command: cmd,
		Data:    payload,
	})
	if resp == nil {
		return fmt.Errorf("gomuks returned empty response for %s", cmd)
	}
	if resp.Command == jsoncmd.RespError {
		var message string
		if err := json.Unmarshal(resp.Data, &message); err != nil || strings.TrimSpace(message) == "" {
			message = string(resp.Data)
		}
		message = strings.TrimSpace(message)
		if message == "" {
			message = "unknown error"
		}
		return fmt.Errorf("gomuks %s failed: %s", cmd, message)
	}
	if resp.Command != jsoncmd.RespSuccess {
		return fmt.Errorf("gomuks returned unexpected response type %s for %s", resp.Command, cmd)
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("failed to decode %s response: %w", cmd, err)
	}
	return nil
}
