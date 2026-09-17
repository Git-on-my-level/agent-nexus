package app

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runPMChannelsDoctor(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("pm channels doctor")
	var telegramURL, discordURL trackedString
	fs.Var(&telegramURL, "telegram-webhook-url", "Telegram ingress URL")
	fs.Var(&discordURL, "discord-webhook-url", "Discord interactions URL")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for anx pm channels doctor")
	}
	checks := make([]doctorCheck, 0, 8)
	hasFailure := false
	add := func(name string, ok bool, message string) {
		if !ok {
			hasFailure = true
		}
		checks = append(checks, doctorCheck{Name: name, OK: ok, Message: message})
	}

	secret := a.Getenv("ANX_PM_TELEGRAM_WEBHOOK_SECRET")
	if len(secret) >= 32 {
		add("telegram_webhook_secret", true, "configured (length >= 32); value not printed")
	} else if secret == "" {
		add("telegram_webhook_secret", false, "ANX_PM_TELEGRAM_WEBHOOK_SECRET is unset")
	} else {
		add("telegram_webhook_secret", false, "ANX_PM_TELEGRAM_WEBHOOK_SECRET must be at least 32 characters")
	}
	if id := strings.TrimSpace(a.Getenv("ANX_PM_TELEGRAM_BOT_ID")); id != "" {
		add("telegram_bot_id", true, "ANX_PM_TELEGRAM_BOT_ID is set")
	} else {
		add("telegram_bot_id", false, "ANX_PM_TELEGRAM_BOT_ID is unset")
	}
	if tok := a.Getenv("ANX_PM_TELEGRAM_BOT_TOKEN"); tok != "" {
		add("telegram_bot_token", true, "ANX_PM_TELEGRAM_BOT_TOKEN is set; value not printed")
	} else {
		add("telegram_bot_token", false, "ANX_PM_TELEGRAM_BOT_TOKEN is unset; outbound Telegram delivery will stay unavailable")
	}

	pub := strings.TrimSpace(a.Getenv("ANX_PM_DISCORD_PUBLIC_KEY"))
	if key, err := hex.DecodeString(pub); err == nil && len(key) == ed25519.PublicKeySize {
		add("discord_public_key", true, "ANX_PM_DISCORD_PUBLIC_KEY is a 32-byte hex key; value not printed")
	} else if pub == "" {
		add("discord_public_key", false, "ANX_PM_DISCORD_PUBLIC_KEY is unset")
	} else {
		add("discord_public_key", false, "ANX_PM_DISCORD_PUBLIC_KEY must be 32-byte hex")
	}
	if id := strings.TrimSpace(a.Getenv("ANX_PM_DISCORD_APPLICATION_ID")); id != "" {
		add("discord_application_id", true, "ANX_PM_DISCORD_APPLICATION_ID is set")
	} else {
		add("discord_application_id", false, "ANX_PM_DISCORD_APPLICATION_ID is unset")
	}
	if tok := a.Getenv("ANX_PM_DISCORD_BOT_TOKEN"); tok != "" {
		add("discord_bot_token", true, "ANX_PM_DISCORD_BOT_TOKEN is set; value not printed")
	} else {
		add("discord_bot_token", false, "ANX_PM_DISCORD_BOT_TOKEN is unset; outbound Discord delivery will stay unavailable")
	}

	tgURL := firstNonEmpty(strings.TrimSpace(telegramURL.value), a.Getenv("ANX_PM_TELEGRAM_WEBHOOK_URL"))
	dcURL := firstNonEmpty(strings.TrimSpace(discordURL.value), a.Getenv("ANX_PM_DISCORD_WEBHOOK_URL"))
	tgOK, tgMsg := probeWebhook(ctx, cfg.Timeout, tgURL, "telegram")
	add("telegram_webhook", tgOK, tgMsg)
	dcOK, dcMsg := probeWebhook(ctx, cfg.Timeout, dcURL, "discord")
	add("discord_webhook", dcOK, dcMsg)

	bindOK, bindMsg := a.probeBindings(ctx, cfg)
	add("bindings", bindOK, bindMsg)

	summary := map[string]any{"checks": checks, "ok": !hasFailure}
	lines := []string{"PM channel doctor"}
	for _, check := range checks {
		state := "PASS"
		if !check.OK {
			state = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", state, check.Name, check.Message))
	}
	result := &commandResult{Data: summary, Text: strings.Join(lines, "\n")}
	if hasFailure {
		return result, errnorm.WithDetails(errnorm.Local("pm_channels_doctor_failed", "channel doctor found failing checks"), summary)
	}
	return result, nil
}

func probeWebhook(ctx context.Context, timeout time.Duration, rawURL, name string) (bool, string) {
	if strings.TrimSpace(rawURL) == "" {
		return false, "no " + name + " webhook URL; pass --" + name + "-webhook-url or set ANX_PM_" + strings.ToUpper(name) + "_WEBHOOK_URL"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false, "invalid URL: " + err.Error()
	}
	client := &http.Client{Timeout: timeout}
	if timeout <= 0 {
		client.Timeout = 10 * time.Second
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, name + " webhook unreachable: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false, fmt.Sprintf("%s webhook returned HTTP %d", name, resp.StatusCode)
	}
	return true, fmt.Sprintf("reachable (HTTP %d); no bot message sent", resp.StatusCode)
}

func (a *App) probeBindings(ctx context.Context, cfg config.Resolved) (bool, string) {
	got, err := a.invokeRawJSON(ctx, cfg, "pm bindings list", http.MethodGet, "/pm/bindings", nil)
	if err != nil {
		if cfg.BaseURL == "" {
			return false, "core base URL is not set; cannot read bindings"
		}
		return false, "could not list bindings: " + err.Error()
	}
	body := commandResultBody(got)
	items, _ := body["items"].([]any)
	return true, fmt.Sprintf("%d binding(s) in the workspace", len(items))
}
