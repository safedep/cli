package project

import (
	"errors"
	"fmt"
	"strings"

	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// syncStoppedError reports a sync that failed after earlier requests of the
// same run wrote projects, so the caller knows how far it got. The server
// message goes into the help, because the generic rendering of a gRPC error
// hides it behind --verbose. Rerun advice is given only for failures that a
// rerun can fix. A failure before the first write keeps its own rendering.
func syncStoppedError(err error, synced, total int) error {
	if synced == 0 {
		return err
	}
	help := fmt.Sprintf("%s. Projects exist for the first %d repositories.", rpcMessage(err), synced)
	if retryable(err) {
		help += " Rerun the same command to sync the rest. The sync is idempotent."
	}
	return newProjectError(
		rpcErrorCode(err),
		fmt.Sprintf("Sync stopped after %d of %d repositories", synced, total),
		help,
		err,
	)
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
	var rpc interface{ GRPCStatus() *status.Status }
	if errors.As(err, &rpc) && rpc.GRPCStatus() != nil {
		return rpc.GRPCStatus().Message()
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
