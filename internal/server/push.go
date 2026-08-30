package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/batuhan/easymatrix/internal/config"
	errs "github.com/batuhan/easymatrix/internal/errors"
	"github.com/golang-jwt/jwt/v5"
)

type pushDevice struct {
	Token       string    `json:"token"`
	Platform    string    `json:"platform"`
	ServerURL   string    `json:"server_url,omitempty"`
	AccessToken string    `json:"access_token,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type pushDeviceInput struct {
	Token     string `json:"token"`
	Platform  string `json:"platform"`
	ServerURL string `json:"serverURL"`
}

type pushParticipant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type pushService struct {
	mu                sync.RWMutex
	persistMu         sync.Mutex
	devices           map[string]pushDevice
	storePath         string
	provider          *apnsProvider
	notificationQueue chan []compatRecord
}

func newPushService(cfg config.Config, stateDir string) *pushService {
	service := &pushService{
		devices:           make(map[string]pushDevice),
		notificationQueue: make(chan []compatRecord, 256),
	}
	if strings.TrimSpace(stateDir) != "" {
		service.storePath = filepath.Join(stateDir, "push", "devices.json")
		if err := service.load(); err != nil {
			log.Printf("failed to load push registrations: %v", err)
		}
	}

	provider, err := newAPNSProvider(cfg)
	if err != nil {
		log.Printf("APNs is disabled: %v", err)
	} else {
		service.provider = provider
		go service.run()
	}
	return service
}

func (p *pushService) canSend() bool {
	return p != nil && p.provider != nil
}

func (p *pushService) register(device pushDevice) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.devices[device.Token] = device
	p.mu.Unlock()
	return p.save()
}

func (p *pushService) delete(token string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	delete(p.devices, token)
	p.mu.Unlock()
	return p.save()
}

func (p *pushService) registeredDevices() []pushDevice {
	p.mu.RLock()
	defer p.mu.RUnlock()
	output := make([]pushDevice, 0, len(p.devices))
	for _, device := range p.devices {
		output = append(output, device)
	}
	return output
}

func (p *pushService) load() error {
	raw, err := os.ReadFile(p.storePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var devices []pushDevice
	if err = json.Unmarshal(raw, &devices); err != nil {
		return err
	}
	for _, device := range devices {
		p.devices[device.Token] = device
	}
	return nil
}

func (p *pushService) save() error {
	if p.storePath == "" {
		return nil
	}
	p.persistMu.Lock()
	defer p.persistMu.Unlock()

	p.mu.RLock()
	devices := make([]pushDevice, 0, len(p.devices))
	for _, device := range p.devices {
		devices = append(devices, device)
	}
	p.mu.RUnlock()

	raw, err := json.Marshal(devices)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p.storePath), 0o700); err != nil {
		return err
	}
	temporaryPath := p.storePath + ".tmp"
	if err = os.WriteFile(temporaryPath, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, p.storePath)
}

func (p *pushService) enqueueMessages(entries []compatRecord) {
	if !p.canSend() || len(entries) == 0 {
		return
	}
	select {
	case p.notificationQueue <- entries:
	default:
		log.Printf("dropping APNs delivery batch because the queue is full")
	}
}

func (p *pushService) run() {
	for entries := range p.notificationQueue {
		p.notifyMessages(entries)
	}
}

func (p *pushService) notifyMessages(entries []compatRecord) {
	if !p.canSend() {
		return
	}
	for _, entry := range entries {
		for _, device := range p.registeredDevices() {
			payload, shouldSend := makePushPayload(entry)
			if !shouldSend {
				continue
			}
			addPushAvatarURL(payload, entry, device)
			if err := p.provider.send(device.Token, payload); err != nil {
				log.Printf("APNs delivery failed: %v", err)
			}
		}
	}
}

func makePushPayload(entry compatRecord) (map[string]any, bool) {
	return makePushPayloadAt(entry, time.Now())
}

func makePushPayloadAt(entry compatRecord, now time.Time) (map[string]any, bool) {
	if isSender, _ := entry["isSender"].(bool); isSender {
		return nil, false
	}
	messageType, _ := entry["type"].(string)
	if messageType == "REACTION" || messageType == "MEMBER_JOIN" || messageType == "MEMBER_INVITE" {
		return nil, false
	}

	messageID, _ := entry["id"].(string)
	chatID, _ := entry["chatID"].(string)
	senderName, _ := entry["senderName"].(string)
	body, _ := entry["text"].(string)
	chatTitle, _ := entry["chatTitle"].(string)
	isGroupChat, _ := entry["isGroupChat"].(bool)
	senderID, _ := entry["senderID"].(string)
	lowercasedBody := strings.ToLower(body)
	if strings.Contains(lowercasedBody, "joined the chat") ||
		strings.Contains(lowercasedBody, "was invited to the chat") {
		return nil, false
	}
	if timestamp, ok := pushMessageTimestamp(entry); ok && now.Sub(timestamp) > time.Minute {
		return nil, false
	}
	if strings.TrimSpace(body) == "" {
		body = "Sent an attachment"
	}
	if strings.TrimSpace(senderName) == "" {
		senderName = "Relay"
	}

	alert := map[string]string{"title": senderName, "body": body}
	if isGroupChat && strings.TrimSpace(chatTitle) != "" {
		alert["title"] = chatTitle
		alert["subtitle"] = senderName
	}
	payload := map[string]any{
		"aps": map[string]any{
			"alert":           alert,
			"sound":           "default",
			"thread-id":       chatID,
			"category":        "MESSAGE",
			"mutable-content": 1,
		},
		"chatID":      chatID,
		"messageID":   messageID,
		"senderID":    senderID,
		"senderName":  senderName,
		"chatTitle":   chatTitle,
		"isGroupChat": isGroupChat,
	}
	if participants, ok := entry["pushParticipants"].([]pushParticipant); ok && len(participants) > 0 {
		payload["groupParticipants"] = participants
	}
	return payload, true
}

func pushMessageTimestamp(entry compatRecord) (time.Time, bool) {
	switch value := entry["timestamp"].(type) {
	case string:
		timestamp, err := time.Parse(time.RFC3339Nano, value)
		return timestamp, err == nil
	case time.Time:
		return value, true
	default:
		return time.Time{}, false
	}
}

func addPushAvatarURL(payload map[string]any, entry compatRecord, device pushDevice) {
	avatarSourceURL, _ := entry["pushAvatarURL"].(string)
	avatarSourceURL = strings.TrimSpace(avatarSourceURL)
	if avatarSourceURL == "" || strings.TrimSpace(device.AccessToken) == "" {
		return
	}

	serverURL, err := url.Parse(strings.TrimSpace(device.ServerURL))
	if err != nil || (serverURL.Scheme != "https" && serverURL.Scheme != "http") || serverURL.Host == "" {
		return
	}

	serverURL.Path = strings.TrimRight(serverURL.Path, "/") + "/v1/assets/serve"
	query := serverURL.Query()
	query.Set("url", avatarSourceURL)
	query.Set("assetAccessSignature", assetAccessSignature(device.AccessToken, avatarSourceURL))
	serverURL.RawQuery = query.Encode()
	payload["avatarURL"] = serverURL.String()
}

func (s *Server) registerPushDevice(w http.ResponseWriter, r *http.Request) error {
	var input pushDeviceInput
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	input.Token = strings.ToLower(strings.TrimSpace(input.Token))
	decoded, err := hex.DecodeString(input.Token)
	if err != nil || len(decoded) == 0 {
		return errs.Validation(map[string]any{"token": "must be a hexadecimal APNs device token"})
	}
	if input.Platform == "" {
		input.Platform = "apple"
	}
	input.ServerURL = strings.TrimRight(strings.TrimSpace(input.ServerURL), "/")
	if input.ServerURL != "" {
		serverURL, parseErr := url.Parse(input.ServerURL)
		if parseErr != nil || (serverURL.Scheme != "https" && serverURL.Scheme != "http") || serverURL.Host == "" {
			return errs.Validation(map[string]any{"serverURL": "must be an absolute HTTP or HTTPS URL"})
		}
	}
	if err = s.push.register(pushDevice{
		Token:       input.Token,
		Platform:    input.Platform,
		ServerURL:   input.ServerURL,
		AccessToken: parseAuthTokenFromRequest(r),
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		return err
	}
	return writeJSON(w, map[string]any{"success": true, "enabled": s.push.canSend()})
}

func (s *Server) deletePushDevice(w http.ResponseWriter, r *http.Request) error {
	token := strings.ToLower(strings.TrimSpace(r.PathValue("token")))
	if token == "" {
		return errs.Validation(map[string]any{"token": "is required"})
	}
	if err := s.push.delete(token); err != nil {
		return err
	}
	return writeJSON(w, map[string]bool{"success": true})
}

type apnsProvider struct {
	key         *ecdsa.PrivateKey
	keyID       string
	teamID      string
	topic       string
	baseURL     string
	client      *http.Client
	tokenMu     sync.Mutex
	cachedToken string
	tokenAt     time.Time
}

func newAPNSProvider(cfg config.Config) (*apnsProvider, error) {
	if cfg.APNSKeyPath == "" || cfg.APNSKeyID == "" || cfg.APNSTeamID == "" || cfg.APNSTopic == "" {
		return nil, fmt.Errorf("set APNS_KEY_PATH, APNS_KEY_ID, APNS_TEAM_ID, and APNS_TOPIC")
	}
	raw, err := os.ReadFile(cfg.APNSKeyPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("APNS_KEY_PATH does not contain a PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("APNs key is not an ECDSA private key")
	}
	baseURL := "https://api.push.apple.com"
	if strings.EqualFold(cfg.APNSEnvironment, "sandbox") {
		baseURL = "https://api.sandbox.push.apple.com"
	}
	return &apnsProvider{
		key:     key,
		keyID:   cfg.APNSKeyID,
		teamID:  cfg.APNSTeamID,
		topic:   cfg.APNSTopic,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (p *apnsProvider) bearerToken() (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.cachedToken != "" && time.Since(p.tokenAt) < 50*time.Minute {
		return p.cachedToken, nil
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": p.teamID,
		"iat": now.Unix(),
	})
	token.Header["kid"] = p.keyID
	signed, err := token.SignedString(p.key)
	if err != nil {
		return "", err
	}
	p.cachedToken = signed
	p.tokenAt = now
	return signed, nil
}

func (p *apnsProvider) send(deviceToken string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	bearer, err := p.bearerToken()
	if err != nil {
		return err
	}
	request, err := http.NewRequest(
		http.MethodPost,
		p.baseURL+"/3/device/"+deviceToken,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "bearer "+bearer)
	request.Header.Set("apns-topic", p.topic)
	request.Header.Set("apns-push-type", "alert")
	request.Header.Set("apns-priority", "10")
	request.Header.Set("content-type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 4*1024))
	return fmt.Errorf("APNs returned %d: %s", response.StatusCode, strings.TrimSpace(string(detail)))
}
