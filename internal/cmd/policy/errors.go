package policy

import (
	"fmt"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newPolicyError(code, humanError, help string, cause error) error {
	return usefulerror.NewUsefulError().
		WithCode(code).
		WithHumanError(humanError).
		WithHelp(help).
		Wrap(cause)
}

func invalidInputError(cause error, help string) error {
	return newPolicyError(usefulerror.ErrBadRequest, cause.Error(), help, cause)
}

// mapChangeError gives the recovery for the errors of a policy change RPC:
// a stale base, an old etag, or a change that does not exist.
func mapChangeError(op, changeID string, err error) error {
	cause := fmt.Errorf("policy change %s: %w", op, err)
	switch reason, _ := usefulerror.ReasonOf(err); reason {
	case errorv1.ErrorReason_ERROR_REASON_POLICY_CHANGE_STALE:
		return newPolicyError(usefulerror.ErrConflict,
			fmt.Sprintf("The policy moved: a part that change %s edits changed after its base revision", changeID),
			fmt.Sprintf("Sync it and review it again: safedep policy change sync %s", changeID),
			cause)
	case errorv1.ErrorReason_ERROR_REASON_POLICY_CHANGE_ETAG_MISMATCH:
		return newPolicyError(usefulerror.ErrConflict,
			fmt.Sprintf("Another request updated change %s after this command read it", changeID),
			fmt.Sprintf("Review it again: safedep policy change show %s", changeID),
			cause)
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.NotFound:
			if changeID == "" {
				return cause
			}
			return newPolicyError(usefulerror.ErrNotFound,
				fmt.Sprintf("Policy change %s does not exist", changeID),
				"List the changes: safedep policy change list",
				cause)
		case codes.PermissionDenied:
			return newPolicyError(usefulerror.ErrAuthorizationFailed,
				"Your role cannot change the policy",
				"Ask an Admin of the tenant to do it",
				cause)
		}
	}
	return cause
}

// mapRestoreError gives the recovery for the errors of a create that
// restores an old revision. A missing revision is the usual cause of
// NotFound.
func mapRestoreError(fromRevision uint64, err error) error {
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return newPolicyError(usefulerror.ErrNotFound,
			fmt.Sprintf("Policy revision %d does not exist", fromRevision),
			"List the revisions: safedep policy revision list",
			fmt.Errorf("policy change create: %w", err))
	}
	return mapChangeError("create", "", err)
}

// mapRuleError gives the recovery for the errors of a package rule RPC.
func mapRuleError(op, ruleID string, err error) error {
	cause := fmt.Errorf("package rule %s: %w", op, err)
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.NotFound:
			return newPolicyError(usefulerror.ErrNotFound,
				fmt.Sprintf("Package rule %s does not exist", ruleID),
				"List the rules: safedep policy rule list",
				cause)
		case codes.PermissionDenied:
			return newPolicyError(usefulerror.ErrAuthorizationFailed,
				"Your role cannot change package rules",
				"Ask an Admin of the tenant to do it",
				cause)
		}
	}
	return cause
}
