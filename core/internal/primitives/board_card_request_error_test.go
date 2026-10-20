package primitives

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

func TestClassifyBoardCardRequestErrorKeepsClientFailures(t *testing.T) {
	nul := mutationOutcomeAfterBegin(classifyBoardCardRequestError(fmt.Errorf("insert card: %w", resourceaccess.ErrNULText)))
	if !errors.Is(nul, ErrInvalidBoardRequest) || !errors.Is(nul, resourceaccess.ErrNULText) {
		t.Fatalf("nul classification: %v", nul)
	}
	ref := mutationOutcomeAfterBegin(classifyBoardCardRequestError(fmt.Errorf("edge: %w", ErrInvalidResourceRef)))
	if !errors.Is(ref, ErrInvalidBoardRequest) || !errors.Is(ref, ErrInvalidResourceRef) {
		t.Fatalf("ref classification: %v", ref)
	}
	if !errors.Is(classifyBoardCardRequestError(sql.ErrNoRows), ErrNotFound) {
		t.Fatal("sql.ErrNoRows was not classified as not found")
	}
	var unknown *MutationOutcomeUnknown
	if errors.As(nul, &unknown) || errors.As(ref, &unknown) {
		t.Fatal("client error became an unknown mutation outcome")
	}
	// A statement error that rolled back committed nothing. It must stay a
	// plain internal error so the handler logs the chain as a 500. Only a
	// failed commit is an unknown outcome; content is promoted before it.
	internal := classifyBoardCardRequestError(errors.New("disk i/o error"))
	if errors.As(internal, &unknown) || internal.Error() != "disk i/o error" {
		t.Fatalf("rolled-back statement error changed: %v", internal)
	}
	for _, err := range []error{
		&MutationOutcomeUnknown{Cause: fmt.Errorf("commit board card create transaction: %w", errors.New("disk i/o error"))},
	} {
		classified := classifyBoardCardRequestError(err)
		if !errors.As(classified, &unknown) || !errors.Is(classified, err) {
			t.Fatalf("commit failure lost outcome_unknown: %v", classified)
		}
	}
}
