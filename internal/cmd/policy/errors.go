package policy

import (
	"fmt"

	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/tui"
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

// failedPreconditionError shows the detail of a FAILED_PRECONDITION. The
// server sends no reason with it, so the hint covers the cases of the RPC.
func failedPreconditionError(st *status.Status, help string, cause error) error {
	return newPolicyError(usefulerror.ErrConflict,
		"The server refused the request: "+tui.EscapeLine(st.Message()), help, cause)
}

// violatesField reports whether a BadRequest detail of err names field.
func violatesField(err error, field string) bool {
	bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err)
	if !ok {
		return false
	}
	for _, v := range bad.GetFieldViolations() {
		if v.GetField() == field {
			return true
		}
	}
	return false
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
		case codes.FailedPrecondition:
			return failedPreconditionError(st, changePreconditionHelp(changeID), cause)
		}
	}
	return cause
}

// changePreconditionHelp covers the refusals of a change: a change that is
// not open, and a change that removes a policy override that package rules
// still use.
func changePreconditionHelp(changeID string) string {
	review := "Review the change: safedep policy change show"
	if changeID != "" {
		review += " " + changeID
	}
	return review + ". If it removes a policy override, delete the package rules of that override first, " +
		"or attach them to another one: safedep policy rule list --override NAME"
}

// mapRestoreError gives the recovery for the errors of a create that
// restores an old revision. The server refuses a revision above head as an
// invalid restore.from_revision.
func mapRestoreError(fromRevision uint64, err error) error {
	st, ok := status.FromError(err)
	missing := ok && (st.Code() == codes.NotFound ||
		(st.Code() == codes.InvalidArgument && violatesField(err, "restore.from_revision")))
	if missing {
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
		case codes.FailedPrecondition:
			return failedPreconditionError(st, "List the rules: safedep policy rule list", cause)
		}
	}
	return cause
}

// mapDecisionListError gives the recovery for the errors of the decision
// log: a tenant without policy management, a role that cannot read the
// policy, a bad filter or page token, and a server that does not have the
// RPC yet.
func mapDecisionListError(err error) error {
	cause := fmt.Errorf("policy decision list: %w", err)
	if reason, _ := usefulerror.ReasonOf(err); reason == errorv1.ErrorReason_ERROR_REASON_ENTITLEMENT_NOT_AVAILABLE {
		return newPolicyError(usefulerror.ErrMissingEntitlements,
			"Policy management is not enabled for this tenant",
			"See the plans at https://safedep.io/pricing, or ask SafeDep support to enable it",
			cause)
	}
	st, ok := status.FromError(err)
	if !ok {
		return cause
	}
	switch st.Code() {
	case codes.PermissionDenied:
		return newPolicyError(usefulerror.ErrAuthorizationFailed,
			"Your role cannot read the policy decision log",
			"Ask an Admin of the tenant for a role that can read the policy",
			cause)
	case codes.InvalidArgument:
		if bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err); ok {
			for _, v := range bad.GetFieldViolations() {
				if v.GetField() == "pagination.page_token" {
					return newPolicyError(usefulerror.ErrBadRequest,
						"The page token is not valid",
						"Run the command again without --page-token",
						cause)
				}
			}
		}
		return newPolicyError(usefulerror.ErrBadRequest,
			"The server refused the filters: "+tui.EscapeLine(st.Message()),
			"See the filters: safedep policy decision list --help",
			cause)
	case codes.Unimplemented:
		return newPolicyError(usefulerror.ErrServiceUnavailable,
			"SafeDep Cloud does not have the policy decision log yet",
			"Try again after the next SafeDep Cloud release",
			cause)
	}
	return cause
}
