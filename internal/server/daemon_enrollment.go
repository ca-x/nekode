package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/config"
	"github.com/ca-x/nekode/internal/storage"
)

const daemonEnrollmentDir = "daemon_enrollments"
const daemonInstallCodeTTL = time.Hour

type daemonEnrollmentStore struct {
	dir string
	mu  sync.Mutex
}

type daemonEnrollment struct {
	ID                string `json:"id"`
	TokenHash         string `json:"tokenHash"`
	TokenPrefix       string `json:"tokenPrefix"`
	InstallCodeHash   string `json:"installCodeHash,omitempty"`
	InstallCodePrefix string `json:"installCodePrefix,omitempty"`
	InstallCodeExpiry int64  `json:"installCodeExpiresUnix,omitempty"`
	InstallCodeUsed   int64  `json:"installCodeUsedUnix,omitempty"`
	DisplayName       string `json:"displayName,omitempty"`
	ComputerID        string `json:"computerId,omitempty"`
	DaemonID          string `json:"daemonId,omitempty"`
	Hostname          string `json:"hostname,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
	ExpiresUnix       int64  `json:"expiresUnix,omitempty"`
	ConnectedUnix     int64  `json:"connectedUnix,omitempty"`
	LastHeartbeatUnix int64  `json:"lastHeartbeatUnix,omitempty"`
	RevokedUnix       int64  `json:"revokedUnix,omitempty"`
	Status            string `json:"status"`
}

type daemonEnrollmentCreate struct {
	DisplayName string `json:"displayName"`
	ComputerID  string `json:"computerId"`
	Hostname    string `json:"hostname"`
	ExpiresUnix int64  `json:"expiresUnix"`
}

type daemonEnrollmentResponse struct {
	ID                string `json:"id"`
	TokenPrefix       string `json:"tokenPrefix"`
	Token             string `json:"token,omitempty"`
	InstallCommand    string `json:"installCommand,omitempty"`
	InstallScriptURL  string `json:"installScriptUrl,omitempty"`
	StatusURL         string `json:"statusUrl"`
	DisplayName       string `json:"displayName,omitempty"`
	ComputerID        string `json:"computerId,omitempty"`
	DaemonID          string `json:"daemonId,omitempty"`
	Hostname          string `json:"hostname,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
	ExpiresUnix       int64  `json:"expiresUnix,omitempty"`
	ConnectedUnix     int64  `json:"connectedUnix,omitempty"`
	LastHeartbeatUnix int64  `json:"lastHeartbeatUnix,omitempty"`
	RevokedUnix       int64  `json:"revokedUnix,omitempty"`
	Status            string `json:"status"`
}

func newDaemonEnrollmentStore(dir string) *daemonEnrollmentStore {
	return &daemonEnrollmentStore{dir: dir}
}

func (s *daemonEnrollmentStore) create(req daemonEnrollmentCreate) (daemonEnrollment, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return daemonEnrollment{}, "", "", err
	}
	token, err := newDaemonEnrollmentToken()
	if err != nil {
		return daemonEnrollment{}, "", "", err
	}
	installCode, err := newDaemonInstallCode()
	if err != nil {
		return daemonEnrollment{}, "", "", err
	}
	now := time.Now().Unix()
	enrollment := daemonEnrollment{
		ID:                storage.NewID("den"),
		TokenHash:         hashDaemonEnrollmentToken(token),
		TokenPrefix:       tokenPrefix(token),
		InstallCodeHash:   hashDaemonEnrollmentToken(installCode),
		InstallCodePrefix: tokenPrefix(installCode),
		InstallCodeExpiry: daemonInstallCodeExpiry(now, req.ExpiresUnix),
		DisplayName:       strings.TrimSpace(req.DisplayName),
		ComputerID:        strings.TrimSpace(req.ComputerID),
		Hostname:          strings.TrimSpace(req.Hostname),
		CreatedUnix:       now,
		ExpiresUnix:       req.ExpiresUnix,
		Status:            "pending",
	}
	if enrollment.ComputerID == "" {
		enrollment.ComputerID = "computer-" + enrollment.ID
	}
	if err := s.save(enrollment); err != nil {
		return daemonEnrollment{}, "", "", err
	}
	return enrollment, token, installCode, nil
}

func (s *daemonEnrollmentStore) get(id string) (daemonEnrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id)
}

func (s *daemonEnrollmentStore) getLocked(id string) (daemonEnrollment, error) {
	path, err := s.path(id)
	if err != nil {
		return daemonEnrollment{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return daemonEnrollment{}, storage.ErrNotFound
	}
	if err != nil {
		return daemonEnrollment{}, err
	}
	var enrollment daemonEnrollment
	if err := json.Unmarshal(data, &enrollment); err != nil {
		return daemonEnrollment{}, err
	}
	if enrollment.ID == "" {
		return daemonEnrollment{}, storage.ErrNotFound
	}
	return enrollment, nil
}

func (s *daemonEnrollmentStore) authenticate(token string) (daemonEnrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token = strings.TrimSpace(token)
	if token == "" {
		return daemonEnrollment{}, storage.ErrNotFound
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return daemonEnrollment{}, storage.ErrNotFound
	}
	if err != nil {
		return daemonEnrollment{}, err
	}
	hash := hashDaemonEnrollmentToken(token)
	now := time.Now().Unix()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		enrollment, err := s.getLocked(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		if enrollment.ExpiresUnix > 0 && enrollment.ExpiresUnix <= now {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(enrollment.TokenHash), []byte(hash)) == 1 {
			return enrollment, nil
		}
	}
	return daemonEnrollment{}, storage.ErrNotFound
}

func (s *daemonEnrollmentStore) consumeInstallCode(id, code string) (daemonEnrollment, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code = strings.TrimSpace(code)
	if code == "" {
		return daemonEnrollment{}, "", storage.ErrNotFound
	}
	enrollment, err := s.getLocked(id)
	if err != nil {
		return daemonEnrollment{}, "", err
	}
	now := time.Now().Unix()
	if enrollment.Status != "" && enrollment.Status != "pending" {
		return daemonEnrollment{}, "", storage.ErrNotFound
	}
	if enrollment.ExpiresUnix > 0 && enrollment.ExpiresUnix <= now {
		return daemonEnrollment{}, "", storage.ErrNotFound
	}
	if enrollment.InstallCodeExpiry > 0 && enrollment.InstallCodeExpiry <= now {
		return daemonEnrollment{}, "", storage.ErrNotFound
	}
	if enrollment.InstallCodeHash == "" {
		return daemonEnrollment{}, "", storage.ErrNotFound
	}
	if subtle.ConstantTimeCompare([]byte(enrollment.InstallCodeHash), []byte(hashDaemonEnrollmentToken(code))) != 1 {
		return daemonEnrollment{}, "", storage.ErrNotFound
	}
	token, err := newDaemonEnrollmentToken()
	if err != nil {
		return daemonEnrollment{}, "", err
	}
	enrollment.TokenHash = hashDaemonEnrollmentToken(token)
	enrollment.TokenPrefix = tokenPrefix(token)
	// The install code stays valid until its TTL expires or the daemon
	// successfully registers (markConnected burns the code). This lets the
	// user retry the install script on flaky networks without re-enrolling.
	if err := s.save(enrollment); err != nil {
		return daemonEnrollment{}, "", err
	}
	return enrollment, token, nil
}

func (s *daemonEnrollmentStore) revoke(id string) (daemonEnrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	enrollment, err := s.getLocked(id)
	if err != nil {
		return daemonEnrollment{}, err
	}
	now := time.Now().Unix()
	enrollment.Status = "revoked"
	enrollment.RevokedUnix = now
	enrollment.TokenHash = ""
	enrollment.InstallCodeHash = ""
	enrollment.InstallCodeUsed = now
	if err := s.save(enrollment); err != nil {
		return daemonEnrollment{}, err
	}
	return enrollment, nil
}

func (s *daemonEnrollmentStore) markConnected(id string, info *daemonv1.ComputerInfo, heartbeat bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	enrollment, err := s.getLocked(id)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	enrollment.Status = "connected"
	if enrollment.ConnectedUnix == 0 {
		enrollment.ConnectedUnix = now
	}
	if heartbeat {
		enrollment.LastHeartbeatUnix = now
	}
	if info != nil {
		if strings.TrimSpace(info.GetComputerId()) != "" {
			enrollment.ComputerID = strings.TrimSpace(info.GetComputerId())
		}
		if strings.TrimSpace(info.GetDaemonId()) != "" {
			enrollment.DaemonID = strings.TrimSpace(info.GetDaemonId())
		}
		if strings.TrimSpace(info.GetHostname()) != "" {
			enrollment.Hostname = strings.TrimSpace(info.GetHostname())
		}
	}
	// First successful connection burns the install code: further retries of
	// the install script will 404 so another host cannot steal the enrollment.
	if enrollment.InstallCodeUsed == 0 {
		enrollment.InstallCodeUsed = now
		enrollment.InstallCodeHash = ""
	}
	return s.save(enrollment)
}

func (s *daemonEnrollmentStore) save(enrollment daemonEnrollment) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path, err := s.path(enrollment.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(enrollment, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *daemonEnrollmentStore) path(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
		return "", storage.ErrNotFound
	}
	return filepath.Join(s.dir, id+".json"), nil
}

func (s *Server) daemonEnrollmentResponse(r *http.Request, enrollment daemonEnrollment, token, installCode string) daemonEnrollmentResponse {
	statusURL := "/api/daemon/enrollments/" + url.PathEscape(enrollment.ID)
	resp := daemonEnrollmentResponse{
		ID:                enrollment.ID,
		TokenPrefix:       enrollment.TokenPrefix,
		Token:             token,
		StatusURL:         statusURL,
		DisplayName:       enrollment.DisplayName,
		ComputerID:        enrollment.ComputerID,
		DaemonID:          enrollment.DaemonID,
		Hostname:          enrollment.Hostname,
		CreatedUnix:       enrollment.CreatedUnix,
		ExpiresUnix:       enrollment.ExpiresUnix,
		ConnectedUnix:     enrollment.ConnectedUnix,
		LastHeartbeatUnix: enrollment.LastHeartbeatUnix,
		RevokedUnix:       enrollment.RevokedUnix,
		Status:            enrollment.Status,
	}
	if installCode != "" {
		resp.InstallScriptURL = s.absoluteURL(r, s.daemonInstallScriptURL(enrollment, installCode, "linux"))
		resp.InstallCommand = s.daemonInstallCommand(r, enrollment, installCode)
	}
	return resp
}

func (s *Server) daemonInstallCommand(r *http.Request, enrollment daemonEnrollment, installCode string) string {
	return `sudo bash -c "$(curl -fsSL ` + shellQuote(s.absoluteURL(r, s.daemonInstallScriptURL(enrollment, installCode, "linux"))) + `)"`
}

func (s *Server) daemonInstallScriptURL(enrollment daemonEnrollment, installCode, platform string) string {
	values := url.Values{}
	values.Set("code", installCode)
	if strings.TrimSpace(platform) != "" {
		values.Set("platform", strings.TrimSpace(platform))
	}
	return "/api/daemon/enrollments/" + url.PathEscape(enrollment.ID) + "/install.sh?" + values.Encode()
}

func newDaemonEnrollmentToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ndt_" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func newDaemonInstallCode() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ndi_" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func daemonInstallCodeExpiry(now, enrollmentExpiry int64) int64 {
	expires := now + int64(daemonInstallCodeTTL/time.Second)
	if enrollmentExpiry > 0 && enrollmentExpiry < expires {
		return enrollmentExpiry
	}
	return expires
}

func hashDaemonEnrollmentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') &&
			!(r >= 'a' && r <= 'z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("@%_+=:,./-", r)
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func (s *Server) absoluteURL(r *http.Request, pathValue string) string {
	pathValue = strings.TrimSpace(pathValue)
	if strings.HasPrefix(pathValue, "http://") || strings.HasPrefix(pathValue, "https://") {
		return pathValue
	}
	if !strings.HasPrefix(pathValue, "/") {
		pathValue = "/" + pathValue
	}
	base := strings.TrimRight(strings.TrimSpace(s.cfg.BaseURL), "/")
	if base == "" || base == config.DefaultBaseURL {
		if derived := deriveExternalBase(r); derived != "" {
			return derived + pathValue
		}
	}
	if base == "" {
		base = config.DefaultBaseURL
	}
	return base + pathValue
}

// deriveExternalBase reconstructs the scheme://host the client used to reach
// this server. When the request came through a reverse proxy, standard
// forwarding headers (X-Forwarded-Proto, X-Forwarded-Host) or the RFC 7239
// Forwarded header take precedence so generated links match the public URL.
func deriveExternalBase(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	scheme := strings.TrimSpace(strings.ToLower(r.Header.Get("X-Forwarded-Proto")))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if i := strings.IndexByte(scheme, ','); i >= 0 {
		scheme = strings.TrimSpace(scheme[:i])
	}
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + host
}

var errDaemonEnrollmentNotReady = fmt.Errorf("daemon enrollment store is not configured")
