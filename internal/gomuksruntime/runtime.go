package gomuksruntime

import (
	"context"
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
	hasLoginToken := strings.TrimSpace(r.cfg.BeeperLoginToken) != ""
	hasPasswordLogin := strings.TrimSpace(r.cfg.BeeperUsername) != "" && strings.TrimSpace(r.cfg.BeeperPassword) != ""
	hasRecoveryKey := strings.TrimSpace(r.cfg.BeeperRecoveryKey) != ""
	if !hasLoginToken && !hasPasswordLogin && !hasRecoveryKey {
		return nil
	}

	cli := gmx.Client
	if cli == nil || cli.Client == nil {
		return errors.New("gomuks client is not initialized")
	}

	state := cli.State()
	if hasLoginToken && !state.IsLoggedIn {
		err := r.LoginCustom(ctx, &jsoncmd.LoginCustomParams{
			HomeserverURL: r.cfg.BeeperHomeserverURL,
			Request: &mautrix.ReqLogin{
				Type:  mautrix.AuthType("org.matrix.login.jwt"),
				Token: r.cfg.BeeperLoginToken,
			},
		})
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already logged in") {
			return fmt.Errorf("failed to login using env login token: %w", err)
		}
		gmx.Log.Info().Str("homeserver_url", r.cfg.BeeperHomeserverURL).Msg("beeper jwt login completed from environment")
	}

	state = cli.State()
	if hasPasswordLogin && !state.IsLoggedIn {
		err := r.Login(ctx, &jsoncmd.LoginParams{
			HomeserverURL: r.cfg.BeeperHomeserverURL,
			Username:      r.cfg.BeeperUsername,
			Password:      r.cfg.BeeperPassword,
		})
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
		if err := r.Verify(ctx, &jsoncmd.VerifyParams{RecoveryKey: r.cfg.BeeperRecoveryKey}); err != nil {
			return fmt.Errorf("failed to verify using env recovery key: %w", err)
		}
		gmx.Log.Info().Msg("beeper verification completed from environment")
	}

	return nil
}
