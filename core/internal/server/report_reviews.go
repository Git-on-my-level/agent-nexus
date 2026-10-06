package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"fmt"
	"net/http"
	"time"

	reports "agent-nexus-visualreport"
)

type reportReviewStore interface {
	AddReportReviewReminder(context.Context, string, string, string, string, string, string) error
}

// The author must currently be able to read the dashboard. Evaluate target
// authorization before storing a reminder, independent of the reading principal.
// Legacy display-name authors are addressed to the revision's writer principal.
func checkReportReviews(r *http.Request, opts handlerOptions, doc, revision map[string]any, panels []reports.Panel) error {
	store, ok := opts.primitiveStore.(reportReviewStore)
	if !ok {
		return nil
	}
	if opts.actorRegistry == nil {
		return nil
	}
	writer := anyString(revision["created_by"])
	now := time.Now().UTC()
	for _, panel := range panels {
		if reports.IsLive(panel.Type) || panel.Source != nil {
			continue
		}
		due, err := time.Parse(time.RFC3339Nano, panel.ReviewBy)
		if err != nil || now.Before(due) {
			continue
		}
		recipient := panel.Author
		// This internal identity decision reads only existence, never profile
		// fields. Profile evidence visibility must not change the recipient.
		exists, err := opts.actorRegistry.Exists(resourceaccess.WithoutPolicy(r.Context()), recipient)
		if err != nil {
			return err
		}
		if !exists {
			recipient = writer
		}
		if recipient == "" {
			continue
		}
		// Evaluate the same current privacy policy for the target, without granting
		// them a token or allowing the reader to impersonate them on a write.
		targetRead := r.Clone(r.Context())
		cacheAuthenticatedPrincipal(targetRead, &auth.Principal{ActorID: recipient})
		attachResourceAccessScope(targetRead, opts)
		if !inboxItemAccessible(targetRead, opts, documentBackingThreadID(doc), nil) {
			continue
		}

		title := fmt.Sprintf("Panel %s on %s is due for review: refresh it or convert it to live.", panel.ID, anyString(doc["title"]))
		if err := store.AddReportReviewReminder(r.Context(), anyString(doc["id"]), anyString(revision["revision_id"]), panel.ID, title, recipient, panel.ReviewBy); err != nil {
			return err
		}
	}
	return nil
}
