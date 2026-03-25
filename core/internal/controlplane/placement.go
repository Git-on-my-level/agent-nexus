package controlplane

import (
	"context"
	"path/filepath"
	"strings"
)

const (
	defaultPackedHostID          = "host_local"
	defaultPackedHostLabel       = "Local packed host"
	defaultPackedHostPortStart   = 8000
	defaultPackedHostPortEnd     = 8990
	defaultPackedHostPortStride  = 10
	defaultPackedHostWebUIOffset = -5000
)

type PackedHost struct {
	ID              string
	Label           string
	WorkspacesRoot  string
	ListenPortStart int
	ListenPortEnd   int
	PortStride      int
	WebUIPortOffset int
}

type WorkspacePlacement struct {
	HostID        string `json:"host_id"`
	HostLabel     string `json:"host_label"`
	WorkspaceRoot string `json:"workspace_root"`
	ListenPort    int    `json:"listen_port"`
}

func defaultPackedHost(controlPlaneRoot string) PackedHost {
	return normalizePackedHost(PackedHost{
		ID:              defaultPackedHostID,
		Label:           defaultPackedHostLabel,
		WorkspacesRoot:  filepath.Join(controlPlaneRoot, "deployments"),
		ListenPortStart: defaultPackedHostPortStart,
		ListenPortEnd:   defaultPackedHostPortEnd,
		PortStride:      defaultPackedHostPortStride,
		WebUIPortOffset: defaultPackedHostWebUIOffset,
	}, controlPlaneRoot)
}

func normalizePackedHost(host PackedHost, controlPlaneRoot string) PackedHost {
	normalized := host
	normalized.ID = strings.TrimSpace(normalized.ID)
	if normalized.ID == "" {
		normalized.ID = defaultPackedHostID
	}
	normalized.Label = strings.TrimSpace(normalized.Label)
	if normalized.Label == "" {
		normalized.Label = defaultPackedHostLabel
	}
	normalized.WorkspacesRoot = strings.TrimSpace(normalized.WorkspacesRoot)
	if normalized.WorkspacesRoot == "" {
		normalized.WorkspacesRoot = filepath.Join(controlPlaneRoot, "deployments")
	} else if !filepath.IsAbs(normalized.WorkspacesRoot) {
		normalized.WorkspacesRoot = filepath.Join(controlPlaneRoot, normalized.WorkspacesRoot)
	}
	if normalized.ListenPortStart <= 0 {
		normalized.ListenPortStart = defaultPackedHostPortStart
	}
	if normalized.ListenPortEnd < normalized.ListenPortStart {
		normalized.ListenPortEnd = defaultPackedHostPortEnd
	}
	if normalized.PortStride <= 0 {
		normalized.PortStride = defaultPackedHostPortStride
	}
	if normalized.WebUIPortOffset == 0 {
		normalized.WebUIPortOffset = defaultPackedHostWebUIOffset
	}
	return normalized
}

func (s *Service) primaryPackedHost() PackedHost {
	if len(s.packedHosts) == 0 {
		return defaultPackedHost(s.workspaceRoot)
	}
	return s.packedHosts[0]
}

func (s *Service) packedHostForID(hostID string) PackedHost {
	hostID = strings.TrimSpace(hostID)
	if hostID == "" {
		return s.primaryPackedHost()
	}
	for _, host := range s.packedHosts {
		if host.ID == hostID {
			return host
		}
	}
	host := s.primaryPackedHost()
	host.ID = hostID
	return host
}

func (s *Service) workspacePlacement(workspace Workspace) WorkspacePlacement {
	host := s.packedHostForID(workspace.HostID)
	placement := WorkspacePlacement{
		HostID:        strings.TrimSpace(workspace.HostID),
		HostLabel:     strings.TrimSpace(workspace.HostLabel),
		WorkspaceRoot: strings.TrimSpace(workspace.WorkspaceRoot),
		ListenPort:    workspace.ListenPort,
	}
	if placement.HostID == "" {
		placement.HostID = host.ID
	}
	if placement.HostLabel == "" {
		placement.HostLabel = host.Label
	}
	if placement.WorkspaceRoot == "" {
		if deploymentRoot := strings.TrimSpace(workspace.DeploymentRoot); deploymentRoot != "" {
			placement.WorkspaceRoot = filepath.Join(deploymentRoot, "workspace")
		} else {
			placement.WorkspaceRoot = filepath.Join(host.WorkspacesRoot, workspace.OrganizationID, workspace.ID, "workspace")
		}
	}
	if placement.ListenPort <= 0 {
		placement.ListenPort = host.ListenPortStart
	}
	return placement
}

func (s *Service) workspaceWebUIPort(workspace Workspace) int {
	host := s.packedHostForID(workspace.HostID)
	listenPort := s.workspacePlacement(workspace).ListenPort
	webUIPort := listenPort + host.WebUIPortOffset
	if webUIPort <= 0 {
		return 3000
	}
	return webUIPort
}

func (s *Service) allocateWorkspacePlacement(ctx context.Context, workspace Workspace) (WorkspacePlacement, error) {
	host := s.primaryPackedHost()
	rows, err := s.db.QueryContext(ctx, `SELECT listen_port FROM workspaces WHERE host_id = ? ORDER BY listen_port ASC`, host.ID)
	if err != nil {
		return WorkspacePlacement{}, internalError("failed to load packed-host placement allocations")
	}
	defer rows.Close()

	usedPorts := map[int]struct{}{}
	for rows.Next() {
		var listenPort int
		if err := rows.Scan(&listenPort); err != nil {
			return WorkspacePlacement{}, internalError("failed to scan packed-host placement allocation")
		}
		if listenPort > 0 {
			usedPorts[listenPort] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return WorkspacePlacement{}, internalError("failed to iterate packed-host placement allocations")
	}

	for listenPort := host.ListenPortStart; listenPort <= host.ListenPortEnd; listenPort += host.PortStride {
		if _, exists := usedPorts[listenPort]; exists {
			continue
		}
		return WorkspacePlacement{
			HostID:        host.ID,
			HostLabel:     host.Label,
			WorkspaceRoot: filepath.Join(host.WorkspacesRoot, workspace.OrganizationID, workspace.ID, "workspace"),
			ListenPort:    listenPort,
		}, nil
	}

	return WorkspacePlacement{}, &APIError{
		Status:  422,
		Code:    "placement_unavailable",
		Message: "no packed-host placement slots are available",
	}
}
