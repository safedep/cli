package jfrog

import (
	"context"
	"errors"

	threatintelv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/threatintel/v1"
)

// packageSource delivers malicious package reports to a callback, owning how it
// fetches and where it resumes. feedService consumes reports and nothing else.
type packageSource interface {
	// subscribe blocks until ctx is cancelled, invoking onRecord once per
	// report. Transient errors are handled internally; only a fatal startup
	// error or cancellation returns.
	subscribe(ctx context.Context, onRecord recordHandler) error
}

// recordHandler handles one report. A non-nil error stops delivery and surfaces
// from subscribe.
type recordHandler func(*threatintelv1.PackageReport) error

// callbackError marks a handler error so a source can tell it apart from a
// transient infra error: the former surfaces, the latter is retried. Wrap
// inside the source, unwrap at the subscribe boundary, never leak the wrapper.
type callbackError struct {
	err error
}

func (e *callbackError) Error() string { return e.err.Error() }
func (e *callbackError) Unwrap() error { return e.err }

// isCallbackError reports whether err came from a recordHandler.
func isCallbackError(err error) bool {
	var cb *callbackError
	return errors.As(err, &cb)
}
