package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

func commandRegistryDigest(commands []map[string]any) string {
	if len(commands) == 0 {
		return ""
	}

	entries := make([]string, 0, len(commands))
	for _, command := range commands {
		commandID := strings.TrimSpace(anyString(command["command_id"]))
		method := strings.ToUpper(strings.TrimSpace(anyString(command["method"])))
		path := strings.TrimSpace(anyString(command["path"]))
		if commandID == "" || method == "" || path == "" {
			continue
		}
		entries = append(entries, commandID+"|"+method+"|"+path)
	}
	if len(entries) == 0 {
		return ""
	}

	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(sum[:])
}

func loadCommandRegistryDigest(opts handlerOptions) (string, error) {
	_, commands, err := loadMetaCommandsPayload(opts)
	if err != nil {
		return "", fmt.Errorf("load meta commands payload: %w", err)
	}
	digest := commandRegistryDigest(commands)
	if strings.TrimSpace(digest) == "" {
		return "", fmt.Errorf("command registry digest is empty")
	}
	return digest, nil
}
