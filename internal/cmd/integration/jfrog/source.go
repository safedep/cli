package jfrog

import (
	"context"
	"errors"

	malysisv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/malysis/v1"
)

// packageSource delivers verified malicious package records to the
// supplied callback. Implementations may pull (poll the gRPC API) or
// push (subscribe to a stream); feedService is agnostic to the delivery
// mechanism.
//
// Each source owns its own cadence and resume state:
//   - pollSource owns a KV cursor and the poll-interval sleep loop.
//
// feedService never sees these details — it only consumes the records.
type packageSource interface {
	// Subscribe blocks until ctx is cancelled. For each verified
	// malicious package the source invokes onRecord exactly once.
	//
	// Transient errors (gRPC failures, network blips) are logged
	// internally and the source continues. Only fatal startup errors
	// or context cancellation are returned.
	Subscribe(ctx context.Context, onRecord recordHandler) error
}

// recordHandler is the per-record callback invoked by a packageSource.
// Returning a non-nil error stops further delivery for the current
// session; the source surfaces the error from Subscribe.
type recordHandler func(*malysisv1.ListPackageAnalysisRecordsResponse_AnalysisRecord) error

// callbackError wraps an error returned by a recordHandler. Sources use
// this wrapper to distinguish a callback error (which must surface from
// Subscribe per the recordHandler contract) from transient infrastructure
// errors (gRPC blip, network reset) which should be logged and retried.
//
// Wrap at the source-internal boundary; unwrap via errors.As at the
// Subscribe boundary; never expose the wrapper to the caller (always
// surface the underlying error).
type callbackError struct {
	err error
}

func (e *callbackError) Error() string { return e.err.Error() }
func (e *callbackError) Unwrap() error { return e.err }

// isCallbackError reports whether err originated from a recordHandler
// (and was wrapped at the source-internal boundary). Used by Subscribe
// implementations to choose between surfacing and retrying.
func isCallbackError(err error) bool {
	var cb *callbackError
	return errors.As(err, &cb)
}
