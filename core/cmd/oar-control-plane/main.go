package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"organization-autorunner-core/internal/controlplane"
	cpserver "organization-autorunner-core/internal/controlplane/server"
	cpstorage "organization-autorunner-core/internal/controlplane/storage"
	"organization-autorunner-core/internal/controlplaneauth"
)

const (
	defaultHost                      = "127.0.0.1"
	defaultPort                      = 8100
	defaultWorkspaceRoot             = ".oar-control-plane"
	defaultShutdownTimeout           = 15 * time.Second
	defaultBackupMaintenanceInterval = 5 * time.Minute
)

func main() {
	var (
		host                      = envString("OAR_CONTROL_PLANE_HOST", defaultHost)
		port                      = envInt("OAR_CONTROL_PLANE_PORT", defaultPort)
		listenAddress             = envString("OAR_CONTROL_PLANE_LISTEN_ADDR", "")
		workspaceRoot             = envString("OAR_CONTROL_PLANE_WORKSPACE_ROOT", defaultWorkspaceRoot)
		publicBaseURL             = envString("OAR_CONTROL_PLANE_PUBLIC_BASE_URL", "")
		webAuthnRPID              = envString("OAR_CONTROL_PLANE_WEBAUTHN_RPID", "")
		webAuthnOrigin            = envString("OAR_CONTROL_PLANE_WEBAUTHN_ORIGIN", "")
		workspaceURLTemplate      = envString("OAR_CONTROL_PLANE_WORKSPACE_URL_TEMPLATE", "")
		inviteURLTemplate         = envString("OAR_CONTROL_PLANE_INVITE_URL_TEMPLATE", "")
		workspaceGrantIssuer      = envString("OAR_CONTROL_PLANE_WORKSPACE_GRANT_ISSUER", "")
		workspaceGrantAudience    = envString("OAR_CONTROL_PLANE_WORKSPACE_GRANT_AUDIENCE", "")
		workspaceGrantSigningKey  = envString("OAR_CONTROL_PLANE_WORKSPACE_GRANT_SIGNING_KEY", "")
		sessionTTL                = envDuration("OAR_CONTROL_PLANE_SESSION_TTL", 12*time.Hour)
		ceremonyTTL               = envDuration("OAR_CONTROL_PLANE_CEREMONY_TTL", 5*time.Minute)
		launchTTL                 = envDuration("OAR_CONTROL_PLANE_LAUNCH_TTL", 10*time.Minute)
		inviteTTL                 = envDuration("OAR_CONTROL_PLANE_INVITE_TTL", 7*24*time.Hour)
		backupMaintenanceInterval = envDuration("OAR_CONTROL_PLANE_BACKUP_MAINTENANCE_INTERVAL", defaultBackupMaintenanceInterval)
		shutdownTimeout           = envDuration("OAR_CONTROL_PLANE_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	)

	flag.StringVar(&host, "host", host, "host interface to bind")
	flag.IntVar(&port, "port", port, "port to listen on")
	flag.StringVar(&listenAddress, "listen-addr", listenAddress, "full listen address host:port; overrides --host/--port")
	flag.StringVar(&workspaceRoot, "workspace-root", workspaceRoot, "root directory for control-plane sqlite workspace")
	flag.StringVar(&publicBaseURL, "public-base-url", publicBaseURL, "public browser-facing base URL for control-plane routes; used as the default base for workspace URLs, invite URLs, workspace-grant issuer, and WebAuthn origin")
	flag.StringVar(&webAuthnRPID, "webauthn-rpid", webAuthnRPID, "explicit WebAuthn RP ID")
	flag.StringVar(&webAuthnOrigin, "webauthn-origin", webAuthnOrigin, "explicit WebAuthn origin")
	flag.StringVar(&workspaceURLTemplate, "workspace-url-template", workspaceURLTemplate, "workspace base URL template containing optional %s slug placeholder")
	flag.StringVar(&inviteURLTemplate, "invite-url-template", inviteURLTemplate, "invite URL template containing optional %s token placeholder")
	flag.StringVar(&workspaceGrantIssuer, "workspace-grant-issuer", workspaceGrantIssuer, "issuer used for signed workspace grants (defaults to the control-plane listen URL when signing is enabled)")
	flag.StringVar(&workspaceGrantAudience, "workspace-grant-audience", workspaceGrantAudience, "audience used for signed workspace grants")
	flag.DurationVar(&sessionTTL, "session-ttl", sessionTTL, "issued control-plane session TTL")
	flag.DurationVar(&ceremonyTTL, "ceremony-ttl", ceremonyTTL, "passkey ceremony TTL")
	flag.DurationVar(&launchTTL, "launch-ttl", launchTTL, "workspace launch grant TTL")
	flag.DurationVar(&inviteTTL, "invite-ttl", inviteTTL, "organization invite TTL")
	flag.DurationVar(&backupMaintenanceInterval, "backup-maintenance-interval", backupMaintenanceInterval, "scheduled workspace backup maintenance interval")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", shutdownTimeout, "graceful shutdown timeout")
	flag.Parse()

	addr := listenAddress
	if strings.TrimSpace(addr) == "" {
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
	normalizedPublicBaseURL, err := controlplane.NormalizePublicBaseURL(publicBaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid OAR_CONTROL_PLANE_PUBLIC_BASE_URL: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(webAuthnOrigin) == "" && normalizedPublicBaseURL != "" {
		webAuthnOrigin, err = controlplane.PublicBaseOrigin(normalizedPublicBaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid control-plane public base URL origin: %v\n", err)
			os.Exit(1)
		}
	}
	var workspaceGrantSigner *controlplaneauth.WorkspaceHumanGrantSigner
	if strings.TrimSpace(workspaceGrantSigningKey) != "" || strings.TrimSpace(workspaceGrantAudience) != "" || strings.TrimSpace(workspaceGrantIssuer) != "" {
		if strings.TrimSpace(workspaceGrantIssuer) == "" {
			if normalizedPublicBaseURL != "" {
				workspaceGrantIssuer = normalizedPublicBaseURL
			} else {
				workspaceGrantIssuer = "http://" + addr
			}
		}
		privateKey, err := controlplaneauth.ParseEd25519PrivateKeyBase64(workspaceGrantSigningKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid OAR_CONTROL_PLANE_WORKSPACE_GRANT_SIGNING_KEY: %v\n", err)
			os.Exit(1)
		}
		workspaceGrantSigner, err = controlplaneauth.NewWorkspaceHumanGrantSigner(controlplaneauth.WorkspaceHumanGrantSignerConfig{
			Issuer:     workspaceGrantIssuer,
			Audience:   workspaceGrantAudience,
			PrivateKey: privateKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid workspace grant signer configuration: %v\n", err)
			os.Exit(1)
		}
	}

	workspace, err := cpstorage.InitializeWorkspace(context.Background(), workspaceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize control-plane workspace: %v\n", err)
		os.Exit(1)
	}
	defer workspace.Close()

	service := controlplane.NewService(workspace, controlplane.Config{
		PublicBaseURL:        normalizedPublicBaseURL,
		SessionTTL:           sessionTTL,
		CeremonyTTL:          ceremonyTTL,
		LaunchTTL:            launchTTL,
		InviteTTL:            inviteTTL,
		WorkspaceURLTemplate: workspaceURLTemplate,
		InviteURLTemplate:    inviteURLTemplate,
		WorkspaceGrantSigner: workspaceGrantSigner,
	})

	backupMaintenanceCtx, cancelBackupMaintenance := context.WithCancel(context.Background())
	defer cancelBackupMaintenance()
	if backupMaintenanceInterval <= 0 {
		backupMaintenanceInterval = defaultBackupMaintenanceInterval
	}
	go func() {
		if err := service.RunBackupMaintenancePass(backupMaintenanceCtx); err != nil && backupMaintenanceCtx.Err() == nil {
			fmt.Fprintf(os.Stderr, "backup maintenance pass failed: %v\n", err)
		}
		ticker := time.NewTicker(backupMaintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-backupMaintenanceCtx.Done():
				return
			case <-ticker.C:
				if err := service.RunBackupMaintenancePass(backupMaintenanceCtx); err != nil && backupMaintenanceCtx.Err() == nil {
					fmt.Fprintf(os.Stderr, "backup maintenance pass failed: %v\n", err)
				}
			}
		}
	}()

	handler := cpserver.NewHandler(service, cpserver.Config{
		HealthCheck: workspace.Ping,
		WebAuthnConfig: cpserver.WebAuthnConfig{
			RPID:     webAuthnRPID,
			RPOrigin: webAuthnOrigin,
		},
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		fmt.Printf("oar-control-plane listening on http://%s\n", addr)
		if workspaceGrantSigner != nil {
			fmt.Printf("  workspace grant signing enabled (issuer=%s audience=%s)\n", workspaceGrantIssuer, workspaceGrantAudience)
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			cancelBackupMaintenance()
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	case sig := <-shutdownSignals:
		cancelBackupMaintenance()
		fmt.Printf("\nreceived %s, shutting down gracefully...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "graceful shutdown failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("server stopped")
	}
}

func envString(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid integer value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid duration value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}
