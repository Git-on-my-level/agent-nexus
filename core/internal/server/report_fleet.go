package server

import (
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/series"
)

const (
	maxReportFleetHosts       = 100
	maxReportFleetHostAgents  = 50
	maxReportFleetEnrollments = 100
	maxReportFleetSeries      = 100
)

func (reader *reportReader) fleetHealth() (map[string]any, bool, error) {
	if reader.opts.authStore == nil {
		return nil, false, fmt.Errorf("host inventory unavailable")
	}
	hosts, hostCount, activeHostCount, err := reader.opts.authStore.ListHostInventory(reader.r.Context(), maxReportFleetHosts, maxReportFleetHostAgents)
	if err != nil {
		return nil, false, err
	}
	data := map[string]any{
		"hosts":                []map[string]any{},
		"host_count":           hostCount,
		"active_host_count":    activeHostCount,
		"enrollments":          []map[string]any{},
		"enrollment_count":     0,
		"enrollment_available": false,
		"enrollment_message":   "Enrollment details are visible only to humans and auth admins.",
		"series":               []map[string]any{},
		"series_message":       "",
	}
	truncated := hostCount > len(hosts)
	hostRows := make([]map[string]any, 0, len(hosts))
	for _, entry := range hosts {
		host := entry.Host
		agents := make([]map[string]any, 0, len(host.Agents))
		for _, agent := range host.Agents {
			agents = append(agents, map[string]any{
				"name": agent.Name, "handle": agent.Handle,
				"bridge_online": agent.BridgeOnline, "revoked_at": agent.RevokedAt,
			})
		}
		if entry.AgentCount > len(agents) {
			truncated = true
		}
		hostRows = append(hostRows, map[string]any{
			"ref": host.Ref, "slug": host.Slug, "display_name": host.DisplayName,
			"hostname": host.Hostname, "discovered_adapters": host.DiscoveredAdapters,
			"agent_count": entry.AgentCount, "agents": agents,
			"created_at": host.CreatedAt, "revoked_at": host.RevokedAt,
		})
	}
	data["hosts"] = hostRows

	if principal, ok := cachedAuthenticatedPrincipal(reader.r); ok && isAuthAdminPrincipal(principal) {
		data["enrollment_available"] = true
		data["enrollment_message"] = ""
		enrollments, enrollmentCount, err := reader.opts.authStore.PendingHostEnrollmentsPage(reader.r.Context(), maxReportFleetEnrollments)
		if err != nil {
			data["enrollment_available"] = false
			data["enrollment_message"] = "Pending enrollment inventory is unavailable."
		} else {
			data["enrollment_count"] = enrollmentCount
			if enrollmentCount > len(enrollments) {
				truncated = true
			}
			rows := make([]map[string]any, 0, len(enrollments))
			for _, enrollment := range enrollments {
				rows = append(rows, map[string]any{
					"requested_slug": enrollment.RequestedSlug, "status": enrollment.Status,
					"discovered_adapters": enrollment.DiscoveredAdapters,
					"created_at":          enrollment.CreatedAt, "expires_at": enrollment.ExpiresAt,
				})
			}
			data["enrollments"] = rows
		}
	}

	if reader.opts.seriesStore == nil {
		data["series_message"] = "Fleet series source is unavailable. The host inventory below is live."
		return data, truncated, nil
	}
	definitions, err := reader.opts.seriesStore.List(reader.r.Context())
	if err != nil {
		data["series_message"] = "Could not read fleet.* series right now. The host inventory below is live."
		return data, truncated, nil
	}
	active := make([]series.Result, 0)
	for _, definition := range definitions {
		if strings.HasPrefix(definition.Name, "fleet.") && definition.RevokedAt == nil {
			active = append(active, definition)
		}
	}
	if len(active) > maxReportFleetSeries {
		active = active[:maxReportFleetSeries]
		truncated = true
	}
	seriesRows := make([]map[string]any, 0, len(active))
	for _, definition := range active {
		result, queryErr := reader.opts.seriesStore.Query(reader.r.Context(), definition.Name, nil, 24*time.Hour, time.Hour, "last", reader.now)
		row := map[string]any{
			"name": definition.Name, "kind": definition.Kind, "unit": definition.Unit,
			"adapter": definition.Adapter, "host": definition.Host,
			"last_push":                 definition.LastPush,
			"expected_interval_seconds": definition.ExpectedInterval,
			"status":                    "ok", "streams": []map[string]any{},
		}
		if queryErr != nil {
			row["status"] = "unavailable"
			row["message"] = "Series observations are temporarily unavailable."
			seriesRows = append(seriesRows, row)
			continue
		}
		streams := make([]map[string]any, 0, len(result.Streams))
		hasRecentPoint := false
		for _, stream := range result.Streams {
			streamRow := map[string]any{
				"labels": stream.Labels, "last_observed_at": stream.LastPoint,
				"stale": stream.Stale,
			}
			if stream.Stale {
				row["status"] = "stale"
			}
			if len(stream.Points) > 0 {
				hasRecentPoint = true
				point := stream.Points[len(stream.Points)-1]
				latest := map[string]any{"observed_at": point.TS}
				if point.Value != nil {
					latest["value"] = *point.Value
				}
				if point.State != nil {
					latest["state"] = *point.State
				}
				streamRow["latest"] = latest
			}
			streams = append(streams, streamRow)
		}
		row["streams"] = streams
		if !hasRecentPoint && row["status"] != "stale" {
			row["status"] = "unavailable"
			row["message"] = "No observations in the last 24 hours."
		} else if len(streams) == 0 {
			row["status"] = "unavailable"
			row["message"] = "No observations have been published for this series."
		}
		seriesRows = append(seriesRows, row)
	}
	data["series"] = seriesRows
	if len(active) == 0 {
		data["series_message"] = "No fleet.* series is declared yet. Declare a fleet.* adapter to publish live fleet health data; the host inventory below remains live."
	}
	return data, truncated, nil
}
