package embedded

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/batuhan/easymatrix/internal/config"
	"github.com/batuhan/easymatrix/internal/gomuksruntime"
	"github.com/batuhan/easymatrix/internal/server"
)

type Request struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	BodyB64 string              `json:"body_base64,omitempty"`
}

type Response struct {
	Status     int                 `json:"status"`
	StatusText string              `json:"statusText,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	BodyB64    string              `json:"body_base64,omitempty"`
}

type Config struct {
	ListenAddr          string `json:"listenAddr,omitempty"`
	AccessToken         string `json:"accessToken,omitempty"`
	StateDir            string `json:"stateDir,omitempty"`
	AllowQueryTokenAuth bool   `json:"allowQueryTokenAuth,omitempty"`
	BeeperHomeserverURL string `json:"beeperHomeserverUrl,omitempty"`
	BeeperLoginToken    string `json:"beeperLoginToken,omitempty"`
	BeeperUsername      string `json:"beeperUsername,omitempty"`
	BeeperPassword      string `json:"beeperPassword,omitempty"`
	BeeperRecoveryKey   string `json:"beeperRecoveryKey,omitempty"`
}

type Runtime struct {
	cfg     config.Config
	rt      *gomuksruntime.Runtime
	server  *server.Server
	handler http.Handler

	mu      sync.Mutex
	started bool
}

func New(cfg Config) (*Runtime, error) {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	rt, err := gomuksruntime.New(normalized)
	if err != nil {
		return nil, err
	}
	srv := server.New(normalized, rt)
	return &Runtime{
		cfg:     normalized,
		rt:      rt,
		server:  srv,
		handler: srv.Handler(),
	}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	if err := r.rt.Start(ctx); err != nil {
		return err
	}
	r.started = true
	return nil
}

func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return
	}
	r.rt.Stop()
	r.started = false
}

func (r *Runtime) StateDir() string {
	return r.cfg.StateDir
}

func (r *Runtime) Handle(ctx context.Context, req Request) (Response, error) {
	if err := r.Start(ctx); err != nil {
		return Response{}, err
	}

	targetURL := req.URL
	if strings.TrimSpace(targetURL) == "" {
		targetURL = "http://embedded.invalid/"
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return Response{}, fmt.Errorf("invalid request url: %w", err)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Host == "" {
		parsed.Host = "embedded.invalid"
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	body, err := decodeBase64(req.BodyB64)
	if err != nil {
		return Response{}, fmt.Errorf("invalid request body: %w", err)
	}
	httpReq := httptest.NewRequest(method, parsed.String(), bytes.NewReader(body)).WithContext(ctx)
	for key, values := range req.Headers {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	recorder := httptest.NewRecorder()
	r.handler.ServeHTTP(recorder, httpReq)

	res := recorder.Result()
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to read response body: %w", err)
	}
	return Response{
		Status:     res.StatusCode,
		StatusText: res.Status,
		Headers:    cloneHeaders(res.Header),
		BodyB64:    encodeBase64(resBody),
	}, nil
}

func (r *Runtime) OpenRealtime(send func(json.RawMessage) error) (*server.EmbeddedRealtimeConnection, error) {
	if err := r.Start(context.Background()); err != nil {
		return nil, err
	}
	return r.server.OpenEmbeddedRealtime(func(payload any) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return send(raw)
	})
}

func normalizeConfig(input Config) (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, err
	}
	if strings.TrimSpace(input.ListenAddr) != "" {
		cfg.ListenAddr = input.ListenAddr
	}
	if strings.TrimSpace(input.AccessToken) != "" {
		cfg.AccessToken = input.AccessToken
	}
	if strings.TrimSpace(input.StateDir) != "" {
		cfg.StateDir = input.StateDir
	}
	cfg.AllowQueryTokenAuth = input.AllowQueryTokenAuth
	if strings.TrimSpace(input.BeeperHomeserverURL) != "" {
		cfg.BeeperHomeserverURL = input.BeeperHomeserverURL
	}
	if strings.TrimSpace(input.BeeperLoginToken) != "" {
		cfg.BeeperLoginToken = input.BeeperLoginToken
	}
	if strings.TrimSpace(input.BeeperUsername) != "" {
		cfg.BeeperUsername = input.BeeperUsername
	}
	if strings.TrimSpace(input.BeeperPassword) != "" {
		cfg.BeeperPassword = input.BeeperPassword
	}
	if strings.TrimSpace(input.BeeperRecoveryKey) != "" {
		cfg.BeeperRecoveryKey = input.BeeperRecoveryKey
	}
	return cfg, nil
}

func cloneHeaders(header http.Header) map[string][]string {
	if len(header) == 0 {
		return nil
	}
	out := make(map[string][]string, len(header))
	for key, values := range header {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func encodeBase64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func decodeBase64(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(value)
}
