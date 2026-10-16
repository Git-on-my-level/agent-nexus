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
	internal := mutationOutcomeAfterBegin(classifyBoardCardRequestError(errors.New("disk i/o error")))
	if !errors.As(internal, &unknown) {
		t.Fatalf("internal error stayed typed: %v", internal)
	}
}
