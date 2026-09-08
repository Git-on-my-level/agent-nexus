package observation

import (
	"bytes"
	"encoding/json"
	"io"
	"net/netip"
	"time"
)

// ConnectionConfig contains approved references, never credential values. The
// config file is installed by the operator, not accepted from public HTTP bodies.
type ConnectionConfig struct {
	Transport         string   `json:"transport"`
	BaseURL           string   `json:"base_url,omitempty"`
	SourceWorkspaceID string   `json:"source_workspace_id,omitempty"`
	CredentialHandle  string   `json:"credential_handle,omitempty"`
	AllowedNetworks   []string `json:"allowed_networks,omitempty"`
	MaxPages          int      `json:"max_pages,omitempty"`
	MaxBytes          int64    `json:"max_bytes,omitempty"`
	Binary            string   `json:"binary,omitempty"`
	Profile           string   `json:"profile,omitempty"`
	KnownHostsFile    string   `json:"known_hosts_file,omitempty"`
	IdentityFile      string   `json:"identity_file,omitempty"`
	Port              int      `json:"port,omitempty"`
}
type PolicyConfig struct {
	IntervalSeconds   int64 `json:"interval_seconds"`
	StaleAfterSeconds int64 `json:"stale_after_seconds"`
	TimeoutSeconds    int64 `json:"timeout_seconds"`
	MaxBackoffSeconds int64 `json:"max_backoff_seconds"`
}
type RegistrationConfig struct {
	CardRef    string           `json:"card_ref"`
	Target     Target           `json:"target"`
	Connection ConnectionConfig `json:"connection"`
	Policy     PolicyConfig     `json:"policy"`
}
type RegistryConfig struct {
	Version       int                  `json:"version"`
	Registrations []RegistrationConfig `json:"registrations"`
}

func LoadRegistrations(reader io.Reader, credentials CredentialResolver) ([]Registration, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, failure(ErrLimit, "operator registry exceeds 1MiB")
	}
	var cfg RegistryConfig
	if err = decodeStrict(raw, &cfg); err != nil {
		return nil, failure(ErrConfiguration, "operator registry violates configuration schema")
	}
	if cfg.Version != 1 || len(cfg.Registrations) < 1 || len(cfg.Registrations) > 1000 {
		return nil, failure(ErrConfiguration, "operator registry requires version 1 and 1..1000 registrations")
	}
	entries := make([]Registration, 0, len(cfg.Registrations))
	seen := map[string]bool{}
	for _, entry := range cfg.Registrations {
		if entry.CardRef == "" || seen[entry.CardRef] || entry.Target.Validate() != nil {
			return nil, failure(ErrConfiguration, "invalid or duplicate operator target")
		}
		seen[entry.CardRef] = true
		p := entry.Policy
		// Check integer bounds before duration conversion to prevent overflow.
		if p.IntervalSeconds < 1 || p.IntervalSeconds > 604800 || p.StaleAfterSeconds < 1 || p.StaleAfterSeconds > 2592000 || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 300 || p.MaxBackoffSeconds < 1 || p.MaxBackoffSeconds > 604800 {
			return nil, failure(ErrConfiguration, "operator refresh policy exceeds bounds")
		}
		policy := RefreshPolicy{Interval: time.Duration(p.IntervalSeconds) * time.Second, StaleAfter: time.Duration(p.StaleAfterSeconds) * time.Second, Timeout: time.Duration(p.TimeoutSeconds) * time.Second, MaxBackoff: time.Duration(p.MaxBackoffSeconds) * time.Second}
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		source, err := entry.Connection.Reader(entry.Target, policy.Timeout, credentials)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Registration{CardRef: entry.CardRef, Target: entry.Target, Policy: policy, Reader: source})
	}
	return entries, nil
}
func (c ConnectionConfig) Reader(t Target, timeout time.Duration, credentials CredentialResolver) (Reader, error) {
	switch c.Transport {
	case "https":
		h := HTTPConfig{BaseURL: c.BaseURL, WorkspaceID: t.WorkspaceID, ConnectionID: t.ConnectionID, SourceWorkspaceID: c.SourceWorkspaceID, CredentialHandle: c.CredentialHandle, ResolveCredential: credentials, MaxPages: c.MaxPages, MaxBytes: c.MaxBytes, Timeout: timeout}
		for _, network := range c.AllowedNetworks {
			p, err := netip.ParsePrefix(network)
			if err != nil {
				return nil, failure(ErrConfiguration, "invalid approved source network")
			}
			h.AllowedNetworks = append(h.AllowedNetworks, p)
		}
		switch t.Source {
		case "github":
			return NewGitHubReader(h)
		case "multica":
			return NewMulticaReader(h)
		}
	case "multica_cli":
		if t.Source != "multica" || c.CredentialHandle != "" {
			return nil, failure(ErrConfiguration, "Multica CLI uses its existing profile only")
		}
		return NewMulticaCLIReader(MulticaCLIConfig{Binary: c.Binary, Profile: c.Profile, BaseURL: c.BaseURL, WorkspaceID: t.WorkspaceID, ConnectionID: t.ConnectionID, SourceWorkspaceID: c.SourceWorkspaceID, Timeout: timeout, MaxBytes: c.MaxBytes})
	case "ssh":
		if t.Source != "ssh_git" || c.CredentialHandle != "" {
			return nil, failure(ErrConfiguration, "SSH Git uses only approved SSH identity file")
		}
		return NewSSHGitReader(SSHConfig{WorkspaceID: t.WorkspaceID, ConnectionID: t.ConnectionID, Host: t.Host, Repository: t.Path, KnownHostsFile: c.KnownHostsFile, IdentityFile: c.IdentityFile, Port: c.Port, Timeout: timeout, MaxBytes: c.MaxBytes})
	}
	return nil, failure(ErrConfiguration, "unsupported registered source transport")
}
func decodeStrict(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return failure(ErrInvalidOutput, "trailing JSON data")
	}
	return nil
}
