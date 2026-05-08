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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
)

const daemonEnrollmentDir = "daemon_enrollments"

type daemonEnrollmentStore struct {
	dir string
	mu  sync.Mutex
}

type daemonEnrollment struct {
	ID                string `json:"id"`
	TokenHash         string `json:"tokenHash"`
	TokenPrefix       string `json:"tokenPrefix"`
	DisplayName       string `json:"displayName,omitempty"`
	ComputerID        string `json:"computerId,omitempty"`
	DaemonID          string `json:"daemonId,omitempty"`
	Hostname          string `json:"hostname,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
	ExpiresUnix       int64  `json:"expiresUnix,omitempty"`
	ConnectedUnix     int64  `json:"connectedUnix,omitempty"`
	LastHeartbeatUnix int64  `json:"lastHeartbeatUnix,omitempty"`
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
	StatusURL         string `json:"statusUrl"`
	DisplayName       string `json:"displayName,omitempty"`
	ComputerID        string `json:"computerId,omitempty"`
	DaemonID          string `json:"daemonId,omitempty"`
	Hostname          string `json:"hostname,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
	ExpiresUnix       int64  `json:"expiresUnix,omitempty"`
	ConnectedUnix     int64  `json:"connectedUnix,omitempty"`
	LastHeartbeatUnix int64  `json:"lastHeartbeatUnix,omitempty"`
	Status            string `json:"status"`
}

func newDaemonEnrollmentStore(dir string) *daemonEnrollmentStore {
	return &daemonEnrollmentStore{dir: dir}
}

func (s *daemonEnrollmentStore) create(req daemonEnrollmentCreate) (daemonEnrollment, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return daemonEnrollment{}, "", err
	}
	token, err := newDaemonEnrollmentToken()
	if err != nil {
		return daemonEnrollment{}, "", err
	}
	now := time.Now().Unix()
	enrollment := daemonEnrollment{
		ID:          storage.NewID("den"),
		TokenHash:   hashDaemonEnrollmentToken(token),
		TokenPrefix: tokenPrefix(token),
		DisplayName: strings.TrimSpace(req.DisplayName),
		ComputerID:  strings.TrimSpace(req.ComputerID),
		Hostname:    strings.TrimSpace(req.Hostname),
		CreatedUnix: now,
		ExpiresUnix: req.ExpiresUnix,
		Status:      "pending",
	}
	if enrollment.ComputerID == "" {
		enrollment.ComputerID = "computer-" + enrollment.ID
	}
	if err := s.save(enrollment); err != nil {
		return daemonEnrollment{}, "", err
	}
	return enrollment, token, nil
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

func (s *Server) daemonEnrollmentResponse(enrollment daemonEnrollment, token string) daemonEnrollmentResponse {
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
		Status:            enrollment.Status,
	}
	if token != "" {
		resp.InstallCommand = s.daemonInstallCommand(enrollment, token)
	}
	return resp
}

func (s *Server) daemonInstallCommand(enrollment daemonEnrollment, token string) string {
	args := []string{
		"nekode-daemon",
		"--server-grpc", shellQuote(s.cfg.GRPCAddr),
		"--computer-id", shellQuote(enrollment.ComputerID),
		"--token", shellQuote(token),
	}
	if strings.TrimSpace(enrollment.Hostname) != "" {
		args = append(args, "--hostname", shellQuote(enrollment.Hostname))
	}
	return strings.Join(args, " ")
}

func newDaemonEnrollmentToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ndt_" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
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

var errDaemonEnrollmentNotReady = fmt.Errorf("daemon enrollment store is not configured")
