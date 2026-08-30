package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/batuhan/easymatrix/internal/compat"
	errs "github.com/batuhan/easymatrix/internal/errors"
)

// encryptedFileRegistry caches EncryptedFile info for mxc URLs so
// the serve endpoint can decrypt media fetched from the homeserver.
var (
	encryptedFileMu       sync.RWMutex
	encryptedFileRegistry = make(map[string]*event.EncryptedFileInfo)
)

func registerEncryptedFile(mxcURL string, file *event.EncryptedFileInfo) {
	if file == nil || mxcURL == "" {
		return
	}
	encryptedFileMu.Lock()
	encryptedFileRegistry[mxcURL] = file
	encryptedFileMu.Unlock()
}

func lookupEncryptedFile(mxcURL string) *event.EncryptedFileInfo {
	encryptedFileMu.RLock()
	defer encryptedFileMu.RUnlock()
	return encryptedFileRegistry[mxcURL]
}

const maxUploadSizeBytes = int64(500 * 1024 * 1024)

var safeUploadIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// uploadAssetResponse is a custom type that omits zero-value fields
// (the library type includes "error":"" which Swift treats as non-nil error).
type uploadAssetResponse struct {
	UploadID string  `json:"uploadID"`
	SrcURL   string  `json:"srcURL,omitempty"`
	FileName string  `json:"fileName,omitempty"`
	MimeType string  `json:"mimeType,omitempty"`
	FileSize float64 `json:"fileSize,omitempty"`
	Width    float64 `json:"width,omitempty"`
	Height   float64 `json:"height,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type uploadMetadata struct {
	UploadID string  `json:"uploadID"`
	FilePath string  `json:"filePath"`
	FileName string  `json:"fileName"`
	MimeType string  `json:"mimeType"`
	FileSize int64   `json:"fileSize"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

func (s *Server) downloadAsset(w http.ResponseWriter, r *http.Request) error {
	var input compat.DownloadAssetInput
	if err := decodeJSON(r, &input); err != nil {
		return err
	}
	if strings.TrimSpace(input.URL) == "" {
		return writeJSON(w, compat.DownloadAssetOutput{Error: "URL is required"})
	}

	filePath, err := s.resolveAssetURL(r.Context(), input.URL)
	if err != nil {
		return writeJSON(w, compat.DownloadAssetOutput{Error: err.Error()})
	}
	return writeJSON(w, compat.DownloadAssetOutput{SrcURL: fileURLFromPath(filePath)})
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) error {
	assetURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if assetURL == "" {
		return errs.Validation(map[string]any{"url": "url is required"})
	}
	filePath, err := s.resolveServePath(r.Context(), assetURL)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(filePath); statErr != nil {
		return errs.NotFound("Asset not found")
	}
	http.ServeFile(w, r, filePath)
	return nil
}

func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) error {
	contentType := r.Header.Get("Content-Type")
	var (
		data     []byte
		fileName string
		mimeType string
		err      error
	)

	if strings.Contains(contentType, "multipart/form-data") {
		data, fileName, mimeType, err = s.parseMultipartUpload(r)
	} else {
		data, fileName, mimeType, err = s.parseBase64Upload(r)
	}
	if err != nil {
		return writeJSON(w, compat.UploadAssetOutput{Error: err.Error()})
	}

	if int64(len(data)) > maxUploadSizeBytes {
		return writeJSON(w, compat.UploadAssetOutput{Error: "Upload too large"})
	}
	if fileName == "" {
		fileName = "file"
	}
	fileName = filepath.Base(fileName)
	if fileName == "." || fileName == "/" || fileName == "" {
		fileName = "file"
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(fileName))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	uploadID := randomID()
	uploadDir := filepath.Join(s.uploadRootDir(), uploadID)
	if err = os.MkdirAll(uploadDir, 0o700); err != nil {
		return errs.Internal(fmt.Errorf("failed to create upload dir: %w", err))
	}
	filePath := filepath.Join(uploadDir, fileName)
	if err = os.WriteFile(filePath, data, 0o600); err != nil {
		return errs.Internal(fmt.Errorf("failed to write upload: %w", err))
	}

	meta := uploadMetadata{
		UploadID: uploadID,
		FilePath: filePath,
		FileName: fileName,
		MimeType: mimeType,
		FileSize: int64(len(data)),
	}
	if width, height := imageDimensions(filePath); width > 0 && height > 0 {
		meta.Width = width
		meta.Height = height
	}
	if err = s.writeUploadMetadata(meta); err != nil {
		return errs.Internal(err)
	}

	return writeJSON(w, uploadAssetResponse{
		UploadID: uploadID,
		SrcURL:   fileURLFromPath(filePath),
		FileName: fileName,
		MimeType: mimeType,
		FileSize: float64(len(data)),
		Width:    float64(meta.Width),
		Height:   float64(meta.Height),
		Duration: meta.Duration,
	})
}

func (s *Server) parseMultipartUpload(r *http.Request) ([]byte, string, string, error) {
	if err := r.ParseMultipartForm(maxUploadSizeBytes); err != nil {
		return nil, "", "", fmt.Errorf("invalid multipart form: %w", err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, "", "", fmt.Errorf("missing file field")
	}
	defer file.Close()

	limitedReader := io.LimitReader(file, maxUploadSizeBytes+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to read uploaded file: %w", err)
	}
	if int64(len(data)) > maxUploadSizeBytes {
		return nil, "", "", fmt.Errorf("upload too large")
	}
	fileName := r.FormValue("fileName")
	if fileName == "" {
		fileName = header.Filename
	}
	mimeType := r.FormValue("mimeType")
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
	}
	return data, fileName, mimeType, nil
}

func (s *Server) parseBase64Upload(r *http.Request) ([]byte, string, string, error) {
	var input compat.UploadAssetInput
	if err := decodeJSON(r, &input); err != nil {
		return nil, "", "", err
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, "", "", fmt.Errorf("content is required")
	}
	decoded, err := base64.StdEncoding.DecodeString(input.Content)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(input.Content)
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid base64 content")
	}
	return decoded, strings.TrimSpace(input.FileName.Or("")), strings.TrimSpace(input.MimeType.Or("")), nil
}

func (s *Server) resolveServePath(ctx context.Context, raw string) (string, error) {
	if strings.HasPrefix(raw, "file://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", errs.Validation(map[string]any{"url": "invalid file url"})
		}
		path := filepath.Clean(parsed.Path)
		if !s.isAllowedServePath(path) {
			return "", errs.Forbidden("Access denied: path is outside allowed directories")
		}
		return path, nil
	}
	path, err := s.resolveAssetURL(ctx, raw)
	if err != nil {
		return "", errs.NotFound(err.Error())
	}
	return path, nil
}

func (s *Server) resolveAssetURL(ctx context.Context, raw string) (string, error) {
	normalized := strings.TrimSpace(raw)
	if strings.HasPrefix(normalized, "localmxc://") {
		normalized = "mxc://" + strings.TrimPrefix(normalized, "localmxc://")
	}
	parsedMXC := id.ContentURIString(normalized).ParseOrIgnore()
	if !parsedMXC.IsValid() {
		return "", fmt.Errorf("URL must be mxc:// or localmxc://")
	}

	cacheDir := s.assetCacheDir()
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create asset cache dir: %w", err)
	}
	sum := sha256.Sum256([]byte(normalized))
	cachePath := filepath.Join(cacheDir, hex.EncodeToString(sum[:]))
	if _, err := os.Stat(cachePath); err == nil {
		// If encryption info exists for this URL, the cached file may have been
		// stored before the key was registered (encrypted blob cached as-is).
		// Validate the cache: if the file doesn't look like decrypted media, discard it.
		if encFile := lookupEncryptedFile(normalized); encFile != nil {
			if !looksDecrypted(cachePath) {
				_ = os.Remove(cachePath)
			} else {
				return cachePath, nil
			}
		} else {
			return cachePath, nil
		}
	}

	resp, err := s.rt.Client().Client.Download(ctx, parsedMXC)
	if err != nil {
		return "", fmt.Errorf("failed to download asset: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read downloaded asset: %w", err)
	}

	// Decrypt if we have encryption info for this mxc URL.
	if encFile := lookupEncryptedFile(normalized); encFile != nil {
		if decryptErr := encFile.DecryptInPlace(data); decryptErr != nil {
			return "", fmt.Errorf("failed to decrypt asset: %w", decryptErr)
		}
	}

	tempPath := cachePath + ".tmp"
	if err = os.WriteFile(tempPath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to save downloaded asset: %w", err)
	}
	if err = os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("failed to finalize cached asset: %w", err)
	}
	return cachePath, nil
}

func (s *Server) writeUploadMetadata(meta uploadMetadata) error {
	metaPath := filepath.Join(filepath.Dir(meta.FilePath), "metadata.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode upload metadata: %w", err)
	}
	if err = os.WriteFile(metaPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write upload metadata: %w", err)
	}
	return nil
}

func (s *Server) loadUploadMetadataByID(uploadID string) (uploadMetadata, error) {
	if !safeUploadIDPattern.MatchString(uploadID) {
		return uploadMetadata{}, errs.Validation(map[string]any{"uploadID": "invalid uploadID"})
	}
	metaPath := filepath.Join(s.uploadRootDir(), uploadID, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return uploadMetadata{}, errs.NotFound("Upload not found")
	}
	var meta uploadMetadata
	if err = json.Unmarshal(data, &meta); err != nil {
		return uploadMetadata{}, errs.Internal(fmt.Errorf("failed to parse upload metadata: %w", err))
	}
	if meta.FilePath == "" {
		return uploadMetadata{}, errs.NotFound("Upload has expired")
	}
	if _, err = os.Stat(meta.FilePath); err != nil {
		return uploadMetadata{}, errs.NotFound("Upload has expired")
	}
	return meta, nil
}

func imageDimensions(filePath string) (int, int) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func fileURLFromPath(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func (s *Server) uploadRootDir() string {
	return filepath.Join(s.rt.StateDir(), "api-uploads")
}

func (s *Server) assetCacheDir() string {
	return filepath.Join(s.rt.StateDir(), "assets")
}

func (s *Server) isAllowedServePath(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	uploadRoot, err := filepath.Abs(s.uploadRootDir())
	if err != nil {
		return false
	}
	assetRoot, err := filepath.Abs(s.assetCacheDir())
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, uploadRoot+string(os.PathSeparator)) ||
		strings.HasPrefix(absPath, assetRoot+string(os.PathSeparator))
}

// looksDecrypted performs a quick sanity check on a cached file.
// Encrypted media blobs won't start with any known media magic bytes.
func looksDecrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 12)
	n, _ := io.ReadFull(f, header)
	if n < 4 {
		return false
	}
	h := header[:n]
	switch {
	case h[0] == 0xFF && h[1] == 0xD8 && h[2] == 0xFF: // JPEG
		return true
	case h[0] == 0x89 && h[1] == 'P' && h[2] == 'N' && h[3] == 'G': // PNG
		return true
	case h[0] == 'G' && h[1] == 'I' && h[2] == 'F': // GIF
		return true
	case h[0] == 'R' && h[1] == 'I' && h[2] == 'F' && h[3] == 'F': // WEBP/WAV
		return true
	case n >= 8 && string(h[4:8]) == "ftyp": // MP4/MOV/M4A
		return true
	case n >= 4 && h[0] == 0x1A && h[1] == 0x45 && h[2] == 0xDF && h[3] == 0xA3: // WEBM/MKV
		return true
	case h[0] == 'O' && h[1] == 'g' && h[2] == 'g' && h[3] == 'S': // OGG
		return true
	case h[0] == 'I' && h[1] == 'D' && h[2] == '3': // MP3 (ID3)
		return true
	case h[0] == 0xFF && (h[1]&0xE0) == 0xE0: // MP3 (sync)
		return true
	case n >= 4 && h[0] == '%' && h[1] == 'P' && h[2] == 'D' && h[3] == 'F': // PDF
		return true
	}
	return false
}

func randomID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
