package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-nexus-cli/internal/config"
)

type askResumeRegistration struct {
	AskRef       string `json:"ask_ref"`
	Acknowledged bool   `json:"acknowledged"`
	AskID        string `json:"ask_id"`
	BaseURL      string `json:"base_url"`
	ActorID      string `json:"actor_id"`
	Command      string `json:"command"`
	Dir          string `json:"dir"`
	ThreadID     string `json:"thread_id"`
	Subscription string `json:"subscription_id"`
	Instance     string `json:"instance_id"`
	Attempts     int    `json:"attempts"`
	State        string `json:"state"`
	Wakeup       string `json:"wakeup_id,omitempty"`
}

func (a *App) askRegistryDir(cfg config.Resolved) (string, error) {
	dir := cfg.ConfigDir
	if dir == "" {
		home, err := a.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config", "anx")
	}
	digest := sha256.Sum256([]byte(strings.TrimRight(cfg.BaseURL, "/") + "\n" + cfg.ActorID))
	dir = filepath.Join(dir, "answer-resumes", hex.EncodeToString(digest[:16]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("resume registry is not a real directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
func askRegistryPath(dir, id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}
func writeAskRegistration(path string, entry askResumeRegistration) error {
	if st, err := os.Lstat(path); err == nil && (!st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("unsafe registry entry")
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".resume-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func readAskRegistration(path string) (askResumeRegistration, error) {
	var entry askResumeRegistration
	st, err := os.Lstat(path)
	if err != nil {
		return entry, err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 64<<10 {
		return entry, fmt.Errorf("registry entry must be a regular 0600 file, at most 64 KiB")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	err = json.Unmarshal(raw, &entry)
	return entry, err
}
func (a *App) resumeCommand(explicit, askID string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if hook := strings.TrimSpace(a.Getenv("ANX_RESUME_CMD")); hook != "" {
		return hook
	}
	// Verified agentctl launch contract: AGENTCTL_EXECUTION_ID, not EXEC_ID.
	if execution := strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")); execution != "" {
		return "agentctl continue " + shellSingleQuote(execution) + " --request-key " + shellSingleQuote("anx-"+askID) + " --prompt-stdin --wait --content"
	}
	return ""
}
