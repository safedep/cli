package project

import (
	"fmt"
	"strings"

	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/rpcerror"
	"github.com/safedep/cli/internal/sourcelink"
)

func newProjectError(code, humanError, help string, cause error) error {
	return usefulerror.NewUsefulError().
		WithCode(code).
		WithHumanError(humanError).
		WithHelp(help).
		Wrap(cause)
}

func invalidProjectSelectionError(cause error, help string) error {
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Invalid project selection",
		help,
		cause,
	)
}

func invalidRepositorySelectionError(cause error, help string) error {
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Invalid repository selection",
		help,
		cause,
	)
}

func unknownFilterValueError(flag, value string, allowed []string) error {
	choices := strings.Join(allowed, ", ")
	cause := fmt.Errorf("unknown --%s value %q: allowed values are %s", flag, value, choices)
	return newProjectError(
		usefulerror.ErrBadRequest,
		"Invalid filter value",
		fmt.Sprintf("Retry --%s with one of: %s.", flag, choices),
		cause,
	)
}

// batchStoppedError reports an --all run that failed after earlier batches
// wrote projects. The failed batch may have been written too: the server can
// commit it and still time out, or return a response the CLI rejects. A
// failure before the first write keeps its own rendering.
func batchStoppedError(err error, synced, total int) error {
	if synced == 0 {
		return err
	}
	return partialSyncError(err,
		fmt.Sprintf("Sync stopped after %d of %d repositories", synced, total),
		fmt.Sprintf("%s. Projects exist for at least the first %d %s.",
			rpcMessage(err), synced, pluralRepositories(synced)))
}

// linkStoppedError reports a run over several links that failed after the
// requests of earlier links wrote projects. It names the accounts, because
// the request order follows the links, not the argument order.
func linkStoppedError(err error, done []sourcelink.Link, failed sourcelink.Link) error {
	if len(done) == 0 {
		return err
	}
	written := make([]string, 0, len(done))
	for _, link := range done {
		written = append(written, sourcelink.Describe(link.Account, link.ID))
	}
	failedName := sourcelink.Describe(failed.Account, failed.ID)
	return partialSyncError(err,
		fmt.Sprintf("Sync through %s failed", failedName),
		fmt.Sprintf("Projects exist for the repositories of %s. The request for %s failed: %s.",
			strings.Join(written, ", "), failedName, rpcMessage(err)))
}

// partialSyncError keeps the server message in the help, because the generic
// rendering of a gRPC error hides it behind --verbose. Rerun advice is given
// only for failures that a rerun can fix. An Unauthenticated failure stays
// as it is, so the entry point turns it into the login guidance.
func partialSyncError(err error, humanError, help string) error {
	if status.Code(err) == codes.Unauthenticated {
		return err
	}
	if retryable(err) {
		help += " Rerun the same command to sync the rest. The sync is idempotent."
	}
	return newProjectError(rpcErrorCode(err), humanError, help, err)
}

// rpcErrorCode reuses the gRPC code mapping of the converters that dry
// registers, so a wrapped error carries the code the generic rendering of the
// same failure shows. dry has no converter for FailedPrecondition, the code
// of a suspended or removed installation, so it maps to conflict as the
// policy commands map it.
func rpcErrorCode(err error) string {
	if status.Code(err) == codes.FailedPrecondition {
		return usefulerror.ErrConflict
	}
	if converted, ok := usefulerror.AsUsefulError(err); ok && converted.Code() != "" {
		return converted.Code()
	}
	return usefulerror.ErrInternalServerError
}

// rpcMessage returns the server's message without the labels the CLI wrapped
// around it. status.Convert on a wrapped error returns the whole chain.
func rpcMessage(err error) string {
	if st, ok := rpcerror.InnerStatus(err); ok {
		return st.Message()
	}
	return err.Error()
}

func retryable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}
