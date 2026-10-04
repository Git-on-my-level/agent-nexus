package hostidentity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var workspaceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type Host struct {
	ID             string `json:"id"`
	KeyID          string `json:"key_id"`
	Slug           string `json:"slug"`
	WorkspaceID    string `json:"workspace_id"`
	WorkspaceSlug  string `json:"workspace_slug,omitempty"`
	BaseURL        string `json:"base_url"`
	PrivateKeyPath string `json:"private_key_path"`
}

func Root(home string) string        { return RootAt(filepath.Join(home, ".config", "anx")) }
func RootAt(configDir string) string { return filepath.Join(configDir, "hosts") }

func WorkspaceKey(workspaceID, baseURL string) string {
	if workspaceIDPattern.MatchString(workspaceID) {
		return workspaceID
	}
	sum := sha256.Sum256([]byte(strings.TrimRight(baseURL, "/")))
	return hex.EncodeToString(sum[:12])
}

func Dir(home, workspaceID, baseURL string) string {
	return DirAt(filepath.Join(home, ".config", "anx"), workspaceID, baseURL)
}
func DirAt(configDir, workspaceID, baseURL string) string {
	return filepath.Join(RootAt(configDir), WorkspaceKey(workspaceID, baseURL))
}

func Save(home string, host Host, key ed25519.PrivateKey) error {
	return SaveAt(filepath.Join(home, ".config", "anx"), host, key)
}
func SaveAt(configDir string, host Host, key ed25519.PrivateKey) error {
	dir := DirAt(configDir, host.WorkspaceID, host.BaseURL)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	host.PrivateKeyPath = filepath.Join(dir, "host.ed25519")
	keyFile, err := os.OpenFile(host.PrivateKeyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = keyFile.WriteString(base64.StdEncoding.EncodeToString(key) + "\n"); err != nil {
		keyFile.Close()
		return err
	}
	if err = keyFile.Close(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(host, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, "host.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func Load(home, baseURL string) (Host, bool, error) {
	return LoadAt(filepath.Join(home, ".config", "anx"), baseURL)
}
func LoadAt(configDir, baseURL string) (Host, bool, error) {
	hosts, err := ListAt(configDir)
	if err != nil {
		return Host{}, false, err
	}
	var found Host
	for _, h := range hosts {
		if baseURL != "" && strings.TrimRight(h.BaseURL, "/") != strings.TrimRight(baseURL, "/") {
			continue
		}
		if found.ID != "" {
			return Host{}, false, fmt.Errorf("multiple enrolled hosts for %s", baseURL)
		}
		found = h
	}
	return found, found.ID != "", nil
}

// ListAt reads host records only; workspace discovery never loads private keys.
func ListAt(configDir string) ([]Host, error) {
	dirs, err := os.ReadDir(RootAt(configDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hosts := []Host{}
	for _, entry := range dirs {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(RootAt(configDir), entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "host.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var h Host
		if err := json.Unmarshal(data, &h); err != nil {
			return nil, fmt.Errorf("decode host %s: %w", dir, err)
		}
		if h.ID == "" || strings.TrimSpace(h.BaseURL) == "" {
			return nil, fmt.Errorf("invalid host record %s", dir)
		}
		if h.PrivateKeyPath == "" {
			h.PrivateKeyPath = filepath.Join(dir, "host.ed25519")
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func Key(host Host) (ed25519.PrivateKey, error) {
	dirInfo, err := os.Stat(filepath.Dir(host.PrivateKeyPath))
	if err != nil {
		return nil, err
	}
	if dirInfo.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("host directory permissions must be owner-only")
	}
	info, err := os.Stat(host.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("host key permissions must be owner-only: %s", host.PrivateKeyPath)
	}
	data, err := os.ReadFile(host.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid host key length")
	}
	return ed25519.PrivateKey(raw), nil
}
