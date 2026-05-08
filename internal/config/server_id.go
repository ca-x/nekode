package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ca-x/nekode/internal/storage"
)

const serverIDFile = "server_id"

func (c Config) ServerID() (string, error) {
	path := filepath.Join(c.DataDir, serverIDFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return "", err
	}
	id := storage.NewID("srv")
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}
