package primitives

import (
	"context"

	"agent-nexus-core/internal/resourceaccess"
)

// ScopeInboxCapture is supplied by A's trusted canonical adapter. It must
// validate the source/RID tuple, advance its canonical version and apply the
// detached B feed/payload/counter changes in the SAME transaction. It must not
// start another transaction, certify parity, or commit. This is not a callback
// accepted from HTTP; constructors/dispatch remain unwired.
type ScopeInboxCapture func(context.Context, *resourceaccess.Tx, *DerivedInboxItem, DerivedInboxItem) error
