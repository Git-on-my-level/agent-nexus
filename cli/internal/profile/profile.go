package profile

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ProfileVersion = 1
)

type Profile struct {
	Version              int    `json:"version"`
	Agent                string `json:"agent"`
	BaseURL              string `json:"base_url"`
	JSON                 *bool  `json:"json,omitempty"`
	Username             string `json:"username,omitempty"`
	AgentID              string `json:"agent_id,omitempty"`
	ActorID              string `json:"actor_id,omitempty"`
	KeyID                string `json:"key_id,omitempty"`
	PrivateKeyPath       string `json:"private_key_path,omitempty"`
	AccessToken          string `json:"access_token,omitempty"`
	RefreshToken         string `json:"refresh_token,omitempty"`
	TokenType            string `json:"token_type,omitempty"`
	AccessTokenExpiresAt string `json:"access_token_expires_at,omitempty"`
	Revoked              bool   `json:"revoked,omitempty"`
	CoreInstanceID       string `json:"core_instance_id,omitempty"`
	UpdatedAt            string `json:"updated_at"`
	CreatedAt            string `json:"created_at"`
}

func RootDir(homeDir string) string {
	return filepath.Join(homeDir, ".config", "oar")
}

func ProfilesDir(homeDir string) string {
	return filepath.Join(RootDir(homeDir), "profiles")
}

func KeysDir(homeDir string) string {
	return filepath.Join(RootDir(homeDir), "keys")
}

func DraftsDir(homeDir string) string {
	return filepath.Join(RootDir(homeDir), "drafts")
}

func DefaultAgentPath(homeDir string) string {
	return filepath.Join(RootDir(homeDir), "default-profile")
}

func ProfilePath(homeDir string, agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = "default"
	}
	return filepath.Join(ProfilesDir(homeDir), agent+".json")
}

func KeyPath(homeDir string, agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = "default"
	}
	return filepath.Join(KeysDir(homeDir), agent+".ed25519")
}

func EnsureDirs(homeDir string) error {
	for _, dir := range []string{ProfilesDir(homeDir), KeysDir(homeDir)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
		}
	}
	return nil
}

func SaveDefaultAgent(homeDir string, agent string) error {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return fmt.Errorf("default agent is required")
	}
	rootDir := RootDir(homeDir)
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		return fmt.Errorf("mkdir root dir: %w", err)
	}
	path := DefaultAgentPath(homeDir)
	if err := os.WriteFile(path, []byte(agent+"\n"), 0o600); err != nil {
		return fmt.Errorf("write default agent file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod default agent file: %w", err)
	}
	return nil
}

func LoadDefaultAgent(homeDir string) (string, bool, error) {
	content, err := os.ReadFile(DefaultAgentPath(homeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read default agent file: %w", err)
	}
	agent := strings.TrimSpace(string(content))
	if agent == "" {
		return "", false, nil
	}
	return agent, true, nil
}

// ErrProfileNotFound means no profile JSON exists for the requested agent name.
var ErrProfileNotFound = errors.New("profile not found")

// ErrPersistDefaultMarker wraps failures writing ~/.config/oar/default-profile.
var ErrPersistDefaultMarker = errors.New("persist default profile marker")

// SetActiveAgent writes the default profile marker after verifying the profile file exists.
func SetActiveAgent(homeDir, agentName string) (profilePath string, err error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return "", fmt.Errorf("agent name is required")
	}
	profilePath = ProfilePath(homeDir, agentName)
	_, ok, err := Load(profilePath)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrProfileNotFound
	}
	if err := SaveDefaultAgent(homeDir, agentName); err != nil {
		return "", fmt.Errorf("%w: %w", ErrPersistDefaultMarker, err)
	}
	return profilePath, nil
}

// ClearDefaultAgent removes the persisted default profile marker, if present.
func ClearDefaultAgent(homeDir string) error {
	path := DefaultAgentPath(homeDir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove default profile marker: %w", err)
	}
	return nil
}

func Save(path string, profile Profile) error {
	profile.Agent = strings.TrimSpace(profile.Agent)
	if profile.Agent == "" {
		return fmt.Errorf("profile.agent is required")
	}
	profile.BaseURL = strings.TrimSpace(profile.BaseURL)
	if profile.BaseURL == "" {
		return fmt.Errorf("profile.base_url is required")
	}
	if profile.Version == 0 {
		profile.Version = ProfileVersion
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(profile.CreatedAt) == "" {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir profile dir: %w", err)
	}
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	encoded = append(encoded, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return fmt.Errorf("write profile temp file: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod profile temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename profile file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod profile file: %w", err)
	}
	return nil
}

func Load(path string) (Profile, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, false, nil
		}
		return Profile{}, false, fmt.Errorf("read profile: %w", err)
	}
	var out Profile
	if err := json.Unmarshal(content, &out); err != nil {
		return Profile{}, false, fmt.Errorf("decode profile: %w", err)
	}
	if out.Version == 0 {
		out.Version = ProfileVersion
	}
	return out, true, nil
}

func ListAgents(homeDir string) ([]string, error) {
	dir := ProfilesDir(homeDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}
	agents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		agent := strings.TrimSuffix(name, ".json")
		agent = strings.TrimSpace(agent)
		if agent == "" {
			continue
		}
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	return agents, nil
}

func GenerateEd25519KeyPair() (publicKeyBase64 string, privateKey ed25519.PrivateKey, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate ed25519 key pair: %w", err)
	}
	return base64.StdEncoding.EncodeToString(pub), priv, nil
}

func SavePrivateKey(path string, privateKey ed25519.PrivateKey) error {
	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key length: %d", len(privateKey))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir key dir: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(privateKey)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(payload+"\n"), 0o600); err != nil {
		return fmt.Errorf("write key temp file: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod key temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename key file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod key file: %w", err)
	}
	return nil
}

func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(content)))
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key length: %d", len(decoded))
	}
	return ed25519.PrivateKey(decoded), nil
}

func ParseAccessTokenExpiry(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}
