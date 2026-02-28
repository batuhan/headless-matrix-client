package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.mau.fi/gomuks/pkg/hicli"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	errs "github.com/batuhan/easymatrix/internal/errors"
)

const beeperPrivateAPIAuthHeader = "Bearer BEEPER-PRIVATE-API-PLEASE-DONT-USE"

type manageStateOutput struct {
	ClientState        *jsoncmd.ClientState `json:"client_state"`
	HomeserverHost     string               `json:"homeserver_host,omitempty"`
	IsBeeperHomeserver bool                 `json:"is_beeper_homeserver"`
}

func (s *Server) manageUI(w http.ResponseWriter, r *http.Request) error {
	if r.URL.Path != "/manage" && r.URL.Path != "/manage/" {
		return errs.NotFound("Not found")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(manageHTML))
	return nil
}

func (s *Server) manageState(w http.ResponseWriter, r *http.Request) error {
	state, err := s.getManageState()
	if err != nil {
		return err
	}
	return writeJSON(w, state)
}

func (s *Server) getManageState() (manageStateOutput, error) {
	cli, err := s.requireManageClient()
	if err != nil {
		return manageStateOutput{}, err
	}
	state := manageStateOutput{
		ClientState: cli.State(),
	}
	if cli.Client != nil && cli.Client.HomeserverURL != nil {
		host := strings.ToLower(strings.TrimSpace(cli.Client.HomeserverURL.Hostname()))
		state.HomeserverHost = host
		state.IsBeeperHomeserver = isAllowedBeeperHomeserverHost(host)
	}
	return state, nil
}

func (s *Server) manageDiscoverHomeserver(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		UserID string `json:"userID"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	userID := id.UserID(strings.TrimSpace(req.UserID))
	if userID == "" {
		return errs.Validation(map[string]any{"userID": "userID is required"})
	}
	if _, _, err := userID.Parse(); err != nil {
		return errs.Validation(map[string]any{"userID": "must be a valid Matrix user ID"})
	}
	cli, err := s.requireManageClient()
	if err != nil {
		return err
	}
	var discovery mautrix.ClientWellKnown
	err = runHiCommand(
		r.Context(),
		cli,
		jsoncmd.ReqDiscoverHomeserver,
		&jsoncmd.DiscoverHomeserverParams{UserID: userID},
		&discovery,
	)
	if err != nil {
		return err
	}
	return writeJSON(w, &discovery)
}

func (s *Server) manageLoginFlows(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		HomeserverURL string `json:"homeserverURL"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.HomeserverURL = strings.TrimSpace(req.HomeserverURL)
	if req.HomeserverURL == "" {
		return errs.Validation(map[string]any{"homeserverURL": "homeserverURL is required"})
	}
	cli, err := s.requireManageClient()
	if err != nil {
		return err
	}
	var loginFlows mautrix.RespLoginFlows
	err = runHiCommand(
		r.Context(),
		cli,
		jsoncmd.ReqGetLoginFlows,
		&jsoncmd.GetLoginFlowsParams{HomeserverURL: req.HomeserverURL},
		&loginFlows,
	)
	if err != nil {
		return err
	}
	return writeJSON(w, &loginFlows)
}

func (s *Server) manageLoginPassword(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		HomeserverURL string `json:"homeserverURL"`
		Username      string `json:"username"`
		Password      string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.HomeserverURL = strings.TrimSpace(req.HomeserverURL)
	req.Username = strings.TrimSpace(req.Username)
	if req.HomeserverURL == "" {
		return errs.Validation(map[string]any{"homeserverURL": "homeserverURL is required"})
	}
	if req.Username == "" {
		return errs.Validation(map[string]any{"username": "username is required"})
	}
	if req.Password == "" {
		return errs.Validation(map[string]any{"password": "password is required"})
	}
	cli, err := s.requireManageClient()
	if err != nil {
		return err
	}
	err = runHiCommand(
		r.Context(),
		cli,
		jsoncmd.ReqLogin,
		&jsoncmd.LoginParams{
			HomeserverURL: req.HomeserverURL,
			Username:      req.Username,
			Password:      req.Password,
		},
		nil,
	)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already logged in") {
			return err
		}
	}
	state, err := s.getManageState()
	if err != nil {
		return err
	}
	return writeJSON(w, state)
}

func (s *Server) manageLoginCustom(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		HomeserverURL string           `json:"homeserverURL"`
		Request       mautrix.ReqLogin `json:"request"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.HomeserverURL = strings.TrimSpace(req.HomeserverURL)
	if req.HomeserverURL == "" {
		return errs.Validation(map[string]any{"homeserverURL": "homeserverURL is required"})
	}
	if strings.TrimSpace(string(req.Request.Type)) == "" {
		return errs.Validation(map[string]any{"request": "request.type is required"})
	}
	cli, err := s.requireManageClient()
	if err != nil {
		return err
	}
	err = runHiCommand(
		r.Context(),
		cli,
		jsoncmd.ReqLoginCustom,
		&jsoncmd.LoginCustomParams{
			HomeserverURL: req.HomeserverURL,
			Request:       &req.Request,
		},
		nil,
	)
	if err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "already logged in") {
			return err
		}
	}
	state, err := s.getManageState()
	if err != nil {
		return err
	}
	return writeJSON(w, state)
}

func (s *Server) manageVerify(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		RecoveryKey string `json:"recoveryKey"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.RecoveryKey = strings.TrimSpace(req.RecoveryKey)
	if req.RecoveryKey == "" {
		return errs.Validation(map[string]any{"recoveryKey": "recoveryKey is required"})
	}
	cli, err := s.requireManageClient()
	if err != nil {
		return err
	}
	err = runHiCommand(
		r.Context(),
		cli,
		jsoncmd.ReqVerify,
		&jsoncmd.VerifyParams{RecoveryKey: req.RecoveryKey},
		nil,
	)
	if err != nil {
		return err
	}
	state, err := s.getManageState()
	if err != nil {
		return err
	}
	return writeJSON(w, state)
}

func (s *Server) manageBeeperStartLogin(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Domain string `json:"domain"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	data, status, err := beeperAPIPost(r.Context(), req.Domain, "/user/login", map[string]any{})
	if err != nil {
		return err
	}
	if status >= 300 {
		return writeJSONStatus(w, status, dataOrFallback(data, map[string]any{"error": "beeper login start failed"}))
	}
	return writeJSON(w, dataOrFallback(data, map[string]any{}))
}

func (s *Server) manageBeeperRequestCode(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Domain  string `json:"domain"`
		Request string `json:"request"`
		Email   string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.Request = strings.TrimSpace(req.Request)
	req.Email = strings.TrimSpace(req.Email)
	if req.Request == "" {
		return errs.Validation(map[string]any{"request": "request is required"})
	}
	if req.Email == "" {
		return errs.Validation(map[string]any{"email": "email is required"})
	}
	data, status, err := beeperAPIPost(r.Context(), req.Domain, "/user/login/email", map[string]any{
		"request": req.Request,
		"email":   req.Email,
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return writeJSONStatus(w, status, dataOrFallback(data, map[string]any{"error": "beeper email code request failed"}))
	}
	return writeJSON(w, dataOrFallback(data, map[string]any{}))
}

func (s *Server) manageBeeperSubmitCode(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Domain   string `json:"domain"`
		Request  string `json:"request"`
		Response string `json:"response"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.Request = strings.TrimSpace(req.Request)
	req.Response = strings.TrimSpace(req.Response)
	if req.Request == "" {
		return errs.Validation(map[string]any{"request": "request is required"})
	}
	if req.Response == "" {
		return errs.Validation(map[string]any{"response": "response is required"})
	}
	data, status, err := beeperAPIPost(r.Context(), req.Domain, "/user/login/response", map[string]any{
		"request":  req.Request,
		"response": strings.ReplaceAll(req.Response, " ", ""),
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return writeJSONStatus(w, status, dataOrFallback(data, map[string]any{"error": "beeper code submission failed"}))
	}
	return writeJSON(w, dataOrFallback(data, map[string]any{}))
}

func (s *Server) requireManageClient() (*hicli.HiClient, error) {
	cli := s.rt.Client()
	if cli == nil || cli.Client == nil {
		return nil, errs.Internal(fmt.Errorf("gomuks runtime is not initialized"))
	}
	return cli, nil
}

func runHiCommand(ctx context.Context, cli *hicli.HiClient, cmd jsoncmd.Name, params any, out any) error {
	var payload json.RawMessage
	if params == nil {
		payload = []byte(`{}`)
	} else {
		raw, err := json.Marshal(params)
		if err != nil {
			return errs.Internal(fmt.Errorf("failed to encode %s params: %w", cmd, err))
		}
		payload = raw
	}
	resp := cli.SubmitJSONCommand(ctx, &hicli.JSONCommand{
		Command: cmd,
		Data:    payload,
	})
	if resp == nil {
		return errs.Internal(fmt.Errorf("gomuks returned empty response for %s", cmd))
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
		return errs.Internal(fmt.Errorf("gomuks %s failed: %s", cmd, message))
	}
	if resp.Command != jsoncmd.RespSuccess {
		return errs.Internal(fmt.Errorf("gomuks returned unexpected response type %s for %s", resp.Command, cmd))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return errs.Internal(fmt.Errorf("failed to decode %s response: %w", cmd, err))
	}
	return nil
}

func beeperAPIPost(ctx context.Context, rawDomain, endpoint string, payload any) (map[string]any, int, error) {
	domain, err := normalizeBeeperDomain(rawDomain)
	if err != nil {
		return nil, 0, errs.Validation(map[string]any{"domain": err.Error()})
	}
	if payload == nil {
		payload = map[string]any{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, errs.Internal(fmt.Errorf("failed to encode request: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api."+domain+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, errs.Internal(fmt.Errorf("failed to create request: %w", err))
	}
	req.Header.Set("Authorization", beeperPrivateAPIAuthHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, errs.Internal(fmt.Errorf("beeper API request failed: %w", err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if len(respBody) == 0 {
		return nil, resp.StatusCode, nil
	}
	var decoded map[string]any
	if err = json.Unmarshal(respBody, &decoded); err != nil {
		return map[string]any{"raw": string(respBody)}, resp.StatusCode, nil
	}
	return decoded, resp.StatusCode, nil
}

func normalizeBeeperDomain(raw string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "matrix.")
	domain = strings.TrimPrefix(domain, "api.")
	domain = strings.TrimSuffix(domain, "/")
	switch domain {
	case "beeper.com", "beeper-staging.com", "beeper-dev.com":
		return domain, nil
	default:
		return "", fmt.Errorf("must be one of: beeper.com, beeper-staging.com, beeper-dev.com")
	}
}

func dataOrFallback(data map[string]any, fallback map[string]any) map[string]any {
	if data != nil {
		return data
	}
	return fallback
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}

const manageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>EasyMatrix Setup</title>
  <style>
    :root {
      --bg: #f5f7fa;
      --panel: #ffffff;
      --text: #1f2937;
      --muted: #6b7280;
      --border: #d1d5db;
      --primary: #0f766e;
      --danger: #b91c1c;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Segoe UI", Tahoma, Geneva, Verdana, sans-serif;
      color: var(--text);
      background: linear-gradient(180deg, #eaf2f6 0%, #f8fafc 100%);
    }
    .wrap {
      max-width: 980px;
      margin: 24px auto;
      padding: 0 16px 24px;
      display: grid;
      gap: 14px;
    }
    .card {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 14px;
      box-shadow: 0 1px 2px rgba(15, 23, 42, 0.06);
    }
    h1, h2 {
      margin: 0 0 10px;
    }
    h1 { font-size: 20px; }
    h2 { font-size: 16px; }
    .muted { color: var(--muted); }
    .row {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 10px;
      margin-bottom: 10px;
    }
    input, select, button, textarea {
      width: 100%;
      font: inherit;
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 8px 10px;
      background: #fff;
    }
    button {
      cursor: pointer;
      background: var(--primary);
      border-color: var(--primary);
      color: #fff;
      font-weight: 600;
    }
    button.secondary {
      background: #f1f5f9;
      color: #0f172a;
      border-color: #cbd5e1;
    }
    .inline {
      display: flex;
      gap: 8px;
      align-items: center;
    }
    .inline > * { margin: 0; }
    .status {
      padding: 10px;
      border-radius: 8px;
      border: 1px solid #bbf7d0;
      background: #f0fdf4;
      color: #166534;
      min-height: 40px;
      white-space: pre-wrap;
    }
    .status.error {
      border-color: #fecaca;
      background: #fef2f2;
      color: var(--danger);
    }
    pre {
      margin: 0;
      padding: 10px;
      overflow: auto;
      background: #0b1220;
      color: #dbeafe;
      border-radius: 8px;
      font-size: 12px;
      line-height: 1.4;
      max-height: 260px;
    }
    label {
      display: block;
      font-size: 13px;
      color: #334155;
      margin-bottom: 4px;
    }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <h1>EasyMatrix Setup</h1>
      <div class="muted">Login and verify without launching full gomuks UI.</div>
    </div>

    <div class="card">
      <h2>Client State</h2>
      <div class="inline" style="margin-bottom: 10px;">
        <button id="refresh-state" class="secondary" style="width: auto;">Refresh</button>
        <span id="state-badges" class="muted"></span>
      </div>
      <pre id="state-json">Loading...</pre>
    </div>

    <div class="card">
      <h2>Beeper Email Login</h2>
      <div class="muted" style="margin-bottom: 8px;">Same flow gomuks uses: request code, submit code, then JWT login.</div>
      <div class="row">
        <div>
          <label for="beeper-domain">Beeper Domain</label>
          <select id="beeper-domain">
            <option value="beeper.com">beeper.com</option>
            <option value="beeper-staging.com">beeper-staging.com</option>
            <option value="beeper-dev.com">beeper-dev.com</option>
          </select>
        </div>
        <div>
          <label for="beeper-email">Email</label>
          <input id="beeper-email" type="email" placeholder="you@example.com">
        </div>
      </div>
      <div class="row">
        <div>
          <button id="beeper-request">Request Code</button>
        </div>
        <div>
          <label for="beeper-code">6-digit Code</label>
          <input id="beeper-code" placeholder="123456">
        </div>
        <div>
          <button id="beeper-submit">Submit Code + Login</button>
        </div>
      </div>
      <pre id="beeper-result"></pre>
    </div>

    <div class="card">
      <h2>Password Login</h2>
      <div class="row">
        <div>
          <label for="pw-hs">Homeserver URL</label>
          <input id="pw-hs" placeholder="https://matrix.beeper.com" value="https://matrix.beeper.com">
        </div>
        <div>
          <label for="pw-user">Username / UserID</label>
          <input id="pw-user" placeholder="@user:beeper.com">
        </div>
        <div>
          <label for="pw-pass">Password</label>
          <input id="pw-pass" type="password" placeholder="Password">
        </div>
      </div>
      <button id="pw-login">Login With Password</button>
    </div>

    <div class="card">
      <h2>Verification</h2>
      <div class="row">
        <div>
          <label for="verify-key">Recovery key or passphrase</label>
          <input id="verify-key" placeholder="Recovery key or passphrase">
        </div>
        <div>
          <label>&nbsp;</label>
          <button id="verify-submit">Verify</button>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>Discover + Login Flows</h2>
      <div class="row">
        <div>
          <label for="discover-user">User ID for .well-known</label>
          <input id="discover-user" placeholder="@user:beeper.com">
        </div>
        <div>
          <label>&nbsp;</label>
          <button id="discover-run" class="secondary">Discover Homeserver</button>
        </div>
      </div>
      <div class="row">
        <div>
          <label for="flows-hs">Homeserver URL</label>
          <input id="flows-hs" placeholder="https://matrix.beeper.com" value="https://matrix.beeper.com">
        </div>
        <div>
          <label>&nbsp;</label>
          <button id="flows-run" class="secondary">Get Login Flows</button>
        </div>
      </div>
      <pre id="flows-result"></pre>
    </div>

    <div id="status" class="status">Ready.</div>
  </div>

  <script>
    let beeperRequestID = "";

    function setStatus(message, isError) {
      const el = document.getElementById("status");
      el.textContent = message;
      el.classList.toggle("error", Boolean(isError));
    }

    function pretty(value) {
      return JSON.stringify(value, null, 2);
    }

    async function api(path, payload) {
      const init = { method: payload ? "POST" : "GET", headers: {} };
      if (payload) {
        init.headers["Content-Type"] = "application/json";
        init.body = JSON.stringify(payload);
      }
      const resp = await fetch(path, init);
      let data = null;
      try {
        data = await resp.json();
      } catch (_) {
        data = null;
      }
      if (!resp.ok) {
        const msg = (data && (data.message || data.error || JSON.stringify(data))) || ("HTTP " + resp.status);
        throw new Error(msg);
      }
      return data;
    }

    async function refreshState() {
      const data = await api("/manage/state");
      document.getElementById("state-json").textContent = pretty(data);
      const cs = data && data.client_state ? data.client_state : {};
      const flags = [
        "initialized=" + Boolean(cs.is_initialized),
        "logged_in=" + Boolean(cs.is_logged_in),
        "verified=" + Boolean(cs.is_verified),
        "beeper_hs=" + Boolean(data.is_beeper_homeserver)
      ];
      document.getElementById("state-badges").textContent = flags.join(" | ");
      return data;
    }

    async function run(action) {
      try {
        setStatus("Working...", false);
        await action();
        setStatus("Done.", false);
      } catch (err) {
        setStatus(String(err), true);
      }
    }

    document.getElementById("refresh-state").addEventListener("click", function () {
      run(refreshState);
    });

    document.getElementById("beeper-request").addEventListener("click", function () {
      run(async function () {
        const domain = document.getElementById("beeper-domain").value;
        const email = document.getElementById("beeper-email").value.trim();
        if (!email) {
          throw new Error("Email is required.");
        }
        const start = await api("/manage/beeper/start-login", { domain: domain });
        if (!start || !start.request) {
          throw new Error("Beeper start-login did not return request.");
        }
        beeperRequestID = String(start.request);
        await api("/manage/beeper/request-code", {
          domain: domain,
          request: beeperRequestID,
          email: email
        });
        document.getElementById("beeper-result").textContent = pretty({ request: beeperRequestID, sent: true });
      });
    });

    document.getElementById("beeper-submit").addEventListener("click", function () {
      run(async function () {
        const domain = document.getElementById("beeper-domain").value;
        if (!beeperRequestID) {
          throw new Error("Request a code first.");
        }
        const rawCode = document.getElementById("beeper-code").value;
        const code = rawCode.replace(/[^0-9]/g, "").slice(0, 6);
        if (code.length !== 6) {
          throw new Error("Enter a 6-digit code.");
        }
        const submit = await api("/manage/beeper/submit-code", {
          domain: domain,
          request: beeperRequestID,
          response: code
        });
        if (!submit || !submit.token) {
          throw new Error("Beeper code submission did not return token.");
        }
        const loginResp = await api("/manage/login-custom", {
          homeserverURL: "https://matrix." + domain,
          request: {
            type: "org.matrix.login.jwt",
            token: String(submit.token)
          }
        });
        document.getElementById("beeper-result").textContent = pretty({
          request: beeperRequestID,
          login: loginResp
        });
        await refreshState();
      });
    });

    document.getElementById("pw-login").addEventListener("click", function () {
      run(async function () {
        const homeserverURL = document.getElementById("pw-hs").value.trim();
        const username = document.getElementById("pw-user").value.trim();
        const password = document.getElementById("pw-pass").value;
        await api("/manage/login-password", {
          homeserverURL: homeserverURL,
          username: username,
          password: password
        });
        await refreshState();
      });
    });

    document.getElementById("verify-submit").addEventListener("click", function () {
      run(async function () {
        const recoveryKey = document.getElementById("verify-key").value.trim();
        await api("/manage/verify", { recoveryKey: recoveryKey });
        await refreshState();
      });
    });

    document.getElementById("discover-run").addEventListener("click", function () {
      run(async function () {
        const userID = document.getElementById("discover-user").value.trim();
        const result = await api("/manage/discover-homeserver", { userID: userID });
        document.getElementById("flows-result").textContent = pretty(result);
        const hs = result && result["m.homeserver"] && result["m.homeserver"].base_url;
        if (hs) {
          document.getElementById("pw-hs").value = hs;
          document.getElementById("flows-hs").value = hs;
        }
      });
    });

    document.getElementById("flows-run").addEventListener("click", function () {
      run(async function () {
        const homeserverURL = document.getElementById("flows-hs").value.trim();
        const result = await api("/manage/login-flows", { homeserverURL: homeserverURL });
        document.getElementById("flows-result").textContent = pretty(result);
      });
    });

    (async function init() {
      try {
        await refreshState();
        setStatus("Ready.", false);
      } catch (err) {
        setStatus(String(err), true);
      }
      setInterval(function () {
        refreshState().catch(function () {});
      }, 3000);
    })();
  </script>
</body>
</html>`
