package bridge

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type UnitPlan struct {
	Path    string `json:"path"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

func UnitFor(goos, home, exe string, uid int) (UnitPlan, error) {
	for _, s := range []string{home, exe} {
		if strings.ContainsAny(s, "\x00\r\n") {
			return UnitPlan{}, fmt.Errorf("invalid unit path")
		}
	}
	log := filepath.Join(Root(home), "service.log")
	path := filepath.Join(home, ".local", "bin") + ":/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
	if goos == "darwin" {
		q := func(s string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
		return UnitPlan{filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), Label, "launchd", `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + Label + `</string><key>ProgramArguments</key><array><string>` + q(exe) + `</string><string>bridge</string><string>run</string></array><key>WorkingDirectory</key><string>` + q(home) + `</string><key>EnvironmentVariables</key><dict><key>HOME</key><string>` + q(home) + `</string><key>ANX_AS</key><string></string><key>PATH</key><string>` + q(path) + `</string></dict><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>StandardOutPath</key><string>` + q(log) + `</string><key>StandardErrorPath</key><string>` + q(log) + `</string></dict></plist>`}, nil
	}
	if goos == "linux" {
		q := func(s string) string { return strconv.Quote(strings.ReplaceAll(s, "%", "%%")) }
		return UnitPlan{filepath.Join(home, ".config", "systemd", "user", Unit), Unit, "systemd-user", "[Unit]\nDescription=Agent Nexus host bridge\nAfter=network-online.target\n[Service]\nExecStart=" + q(exe) + " bridge run\nWorkingDirectory=" + q(home) + "\nEnvironment=" + q("HOME="+home) + "\nEnvironment=\"ANX_AS=\"\nEnvironment=" + q("PATH="+path) + "\nRestart=on-failure\nRestartSec=5\n[Install]\nWantedBy=default.target\n"}, nil
	}
	return UnitPlan{}, fmt.Errorf("bridge service supports macOS and Linux")
}
