package gomuksruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.mau.fi/gomuks/pkg/gomuks"
	"go.mau.fi/gomuks/pkg/hicli"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
	"go.mau.fi/util/dbutil"
	"golang.org/x/net/http2"
	"maunium.net/go/mautrix"

	"github.com/batuhan/easymatrix/internal/config"
)

type Runtime struct {
	cfg     config.Config
	dataDir string
	gmx     *gomuks.Gomuks
}

func New(cfg config.Config) (*Runtime, error) {
	if cfg.StateDir != "" {
		stateDir, err := filepath.Abs(cfg.StateDir)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve state dir: %w", err)
		}
		cfg.StateDir = stateDir
	}
	dataDir, err := resolveDataDir(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	return &Runtime{cfg: cfg, dataDir: dataDir}, nil
}

func withConfiguredGomuksRoot(root string, fn func() error) error {
	if root == "" {
		return fn()
	}

	oldRoot, hadRoot := os.LookupEnv("GOMUKS_ROOT")
	if err := os.Setenv("GOMUKS_ROOT", root); err != nil {
		return fmt.Errorf("failed to set GOMUKS_ROOT: %w", err)
	}
	defer func() {
		if hadRoot {
			_ = os.Setenv("GOMUKS_ROOT", oldRoot)
		} else {
			_ = os.Unsetenv("GOMUKS_ROOT")
		}
	}()

	return fn()
}

func resolveDataDir(root string) (string, error) {
	gmx := gomuks.NewGomuks()
	if err := withConfiguredGomuksRoot(root, func() error {
		gmx.InitDirectories()
		return nil
	}); err != nil {
		return "", err
	}

	dataDir, err := filepath.Abs(gmx.DataDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve gomuks data dir: %w", err)
	}
	return dataDir, nil
}

func startClientWithoutExit(gmx *gomuks.Gomuks) error {
	hicli.HTMLSanitizerImgSrcTemplate = "_gomuks/media/%s/%s?encrypted=false"
	rawDB, err := dbutil.NewFromConfig("gomuks", dbutil.Config{
		PoolConfig: gmx.GetDBConfig(),
	}, dbutil.ZeroLogger(gmx.Log.With().Str("component", "hicli").Str("db_section", "main").Logger()))
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	clientCtx := gmx.Log.WithContext(context.Background())
	gmx.Client = hicli.New(
		rawDB,
		nil,
		gmx.Log.With().Str("component", "hicli").Logger(),
		[]byte("meow"),
		gmx.HandleEvent,
	)
	gmx.Client.LogoutFunc = gmx.Logout

	httpClient := gmx.Client.Client.Client
	if runtime.GOOS == "js" {
		gmx.Client.Client.UserAgent = ""
		httpClient.Transport = nil
	} else if transport, ok := httpClient.Transport.(*http.Transport); ok {
		transport.ForceAttemptHTTP2 = false
		if !gmx.Config.Matrix.DisableHTTP2 {
			h2, err := http2.ConfigureTransports(transport)
			if err != nil {
				return fmt.Errorf("failed to configure HTTP/2: %w", err)
			}
			h2.ReadIdleTimeout = 30 * time.Second
		}
	}

	userID, err := gmx.Client.DB.Account.GetFirstUserID(clientCtx)
	if err != nil {
		return fmt.Errorf("failed to get first user ID: %w", err)
	}
	if err := gmx.Client.Start(clientCtx, userID, nil); err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}
	return nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if r.gmx != nil {
		return nil
	}
	gmx := gomuks.NewGomuks()
	gmx.DisableAuth = true

	if err := withConfiguredGomuksRoot(r.cfg.StateDir, func() error {
		gmx.InitDirectories()
		return nil
	}); err != nil {
		return err
	}
	dataDir, err := filepath.Abs(gmx.DataDir)
	if err != nil {
		return fmt.Errorf("failed to resolve gomuks data dir: %w", err)
	}
	r.dataDir = dataDir

	if err := gmx.LoadConfig(); err != nil {
		return fmt.Errorf("failed to load gomuks config: %w", err)
	}
	gmx.SetupLog()
	if err := startClientWithoutExit(gmx); err != nil {
		return err
	}
	r.gmx = gmx
	if err := r.bootstrapSessionFromEnv(ctx, gmx); err != nil {
		r.gmx = nil
		gmx.DirectStop()
		return err
	}
	gmx.Log.Info().Str("state_dir", r.cfg.StateDir).Msg("gomuks runtime started")
	return nil
}

func (r *Runtime) Stop() {
	if r.gmx != nil {
		r.gmx.DirectStop()
		r.gmx = nil
	}
}

func (r *Runtime) Client() *hicli.HiClient {
	if r.gmx == nil {
		return nil
	}
	return r.gmx.Client
}

func (r *Runtime) EventBuffer() *gomuks.EventBuffer {
	if r.gmx == nil {
		return nil
	}
	return r.gmx.EventBuffer
}

func (r *Runtime) SubmitJSONCommand(ctx context.Context, cmd jsoncmd.Name, params any, out any) error {
	cli := r.Client()
	if cli == nil || cli.Client == nil {
		return fmt.Errorf("gomuks runtime is not initialized")
	}

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

func (r *Runtime) StateDir() string {
	return r.dataDir
}

func (r *Runtime) bootstrapSessionFromEnv(ctx context.Context, gmx *gomuks.Gomuks) error {
	hasLoginToken := strings.TrimSpace(r.cfg.MatrixLoginToken) != ""
	hasPasswordLogin := strings.TrimSpace(r.cfg.MatrixUsername) != "" && strings.TrimSpace(r.cfg.MatrixPassword) != ""
	hasRecoveryKey := strings.TrimSpace(r.cfg.MatrixRecoveryKey) != ""
	if !hasLoginToken && !hasPasswordLogin && !hasRecoveryKey {
		return nil
	}

	cli := gmx.Client
	if cli == nil || cli.Client == nil {
		return errors.New("gomuks client is not initialized")
	}

	state := cli.State()
	if hasLoginToken && !state.IsLoggedIn {
		err := r.SubmitJSONCommand(ctx, jsoncmd.ReqLoginCustom, &jsoncmd.LoginCustomParams{
			HomeserverURL: r.cfg.MatrixHomeserverURL,
			Request: &mautrix.ReqLogin{
				Type:  mautrix.AuthType("org.matrix.login.jwt"),
				Token: r.cfg.MatrixLoginToken,
			},
		}, nil)
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already logged in") {
			return fmt.Errorf("failed to login using env login token: %w", err)
		}
		gmx.Log.Info().Str("homeserver_url", r.cfg.MatrixHomeserverURL).Msg("matrix jwt login completed from environment")
	}

	state = cli.State()
	if hasPasswordLogin && !state.IsLoggedIn {
		err := r.SubmitJSONCommand(ctx, jsoncmd.ReqLogin, &jsoncmd.LoginParams{
			HomeserverURL: r.cfg.MatrixHomeserverURL,
			Username:      r.cfg.MatrixUsername,
			Password:      r.cfg.MatrixPassword,
		}, nil)
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already logged in") {
			return fmt.Errorf("failed to login using env credentials: %w", err)
		}
		gmx.Log.Info().Str("homeserver_url", r.cfg.MatrixHomeserverURL).Msg("matrix password login completed from environment")
	}

	state = cli.State()
	if hasRecoveryKey && !state.IsVerified {
		if !state.IsLoggedIn {
			return errors.New("MATRIX_RECOVERY_KEY was provided, but no logged-in session is available")
		}
		if err := r.SubmitJSONCommand(ctx, jsoncmd.ReqVerify, &jsoncmd.VerifyParams{RecoveryKey: r.cfg.MatrixRecoveryKey}, nil); err != nil {
			return fmt.Errorf("failed to verify using env recovery key: %w", err)
		}
		gmx.Log.Info().Msg("matrix verification completed from environment")
	}

	return nil
}
