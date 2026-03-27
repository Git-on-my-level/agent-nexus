package controlplane

import "testing"

func TestNormalizePublicBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizePublicBaseURL("https://example.com/oar/team-alpha/?q=ignored")
	if err != nil {
		t.Fatalf("NormalizePublicBaseURL returned error: %v", err)
	}
	if got != "https://example.com/oar/team-alpha" {
		t.Fatalf("expected normalized public base URL, got %q", got)
	}
}

func TestPublicBaseDerivedTemplates(t *testing.T) {
	t.Parallel()

	workspaceTemplate, err := WorkspaceURLTemplateFromPublicBase("https://example.com/oar")
	if err != nil {
		t.Fatalf("WorkspaceURLTemplateFromPublicBase returned error: %v", err)
	}
	if workspaceTemplate != "https://example.com/oar/%s" {
		t.Fatalf("unexpected workspace template %q", workspaceTemplate)
	}

	inviteTemplate, err := InviteURLTemplateFromPublicBase("https://example.com/oar")
	if err != nil {
		t.Fatalf("InviteURLTemplateFromPublicBase returned error: %v", err)
	}
	if inviteTemplate != "https://example.com/oar/invites/%s" {
		t.Fatalf("unexpected invite template %q", inviteTemplate)
	}

	origin, err := PublicBaseOrigin("https://example.com/oar/team-alpha")
	if err != nil {
		t.Fatalf("PublicBaseOrigin returned error: %v", err)
	}
	if origin != "https://example.com" {
		t.Fatalf("unexpected public origin %q", origin)
	}
}
