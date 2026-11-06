package policy

import (
	"errors"
	"fmt"
	"strings"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"buf.build/go/protovalidate"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"

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
func failedPreconditionError(err error, help string, cause error) error {
	return newPolicyError(usefulerror.ErrConflict,
		"The server refused the request: "+describeStatus(err), help, cause)
}

// invalidArgumentError shows the message and each field violation of an
// INVALID_ARGUMENT. Without it, the generic conversion shows only "Invalid
// request" and the user cannot find the field to fix.
func invalidArgumentError(err error, help string, cause error) error {
	return newPolicyError(usefulerror.ErrBadRequest, describeStatus(err), help, cause)
}

// serviceFailedPrefix is the text that the server puts before the message of
// a service error. It names no field and no fix, so the CLI removes it.
const serviceFailedPrefix = "service execution failed: "

// describeStatus is the text of a gRPC status error for a person: the status
// message, then each field violation of a BadRequest detail as
// "field: description". The text of status.FromError on a wrapped error
// starts with "rpc error: code = ... desc =", so this reads the status of
// the innermost error. When the message already holds the description of
// each violation, the violations replace it, so no text shows two times.
func describeStatus(err error) string {
	st, ok := innerStatus(err)
	if !ok {
		return err.Error()
	}
	msg := strings.TrimSpace(st.Message())
	for strings.HasPrefix(msg, serviceFailedPrefix) {
		msg = strings.TrimSpace(strings.TrimPrefix(msg, serviceFailedPrefix))
	}
	var lines []string
	covered := true
	if bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err); ok {
		for _, v := range bad.GetFieldViolations() {
			line := v.GetDescription()
			if field := violationField(v); field != "" {
				line = field + ": " + line
			}
			lines = append(lines, tui.EscapeLine(line))
			covered = covered && v.GetDescription() != "" && strings.Contains(msg, v.GetDescription())
		}
	}
	if msg != "" && (len(lines) == 0 || !covered) {
		lines = append([]string{tui.EscapeText(msg)}, lines...)
	}
	if len(lines) == 0 {
		return st.Code().String()
	}
	return strings.Join(lines, "\n")
}

// innerStatus is the status of the innermost gRPC error in the chain of err,
// with its message as the server sent it.
func innerStatus(err error) (*status.Status, bool) {
	var gs interface{ GRPCStatus() *status.Status }
	if !errors.As(err, &gs) {
		return nil, false
	}
	st := gs.GRPCStatus()
	return st, st != nil
}

// violationField is the field of a violation as a path, as in
// "settings.overrides[0].name". The server sends the protovalidate field
// path of a request as protobuf text, which no person can read.
func violationField(v *errdetails.BadRequest_FieldViolation) string {
	field := v.GetField()
	path := &validate.FieldPath{}
	if err := prototext.Unmarshal([]byte(field), path); err != nil || len(path.GetElements()) == 0 {
		return field
	}
	return protovalidate.FieldPathString(path)
}

// errorText is the text of err for a person: the human error of a useful
// error in the chain, else the text of describeStatus.
func errorText(err error) string {
	var ue usefulerror.UsefulError
	if errors.As(err, &ue) {
		return ue.HumanError()
	}
	return describeStatus(err)
}

// violatesField reports whether a BadRequest detail of err names field.
func violatesField(err error, field string) bool {
	bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err)
	if !ok {
		return false
	}
	for _, v := range bad.GetFieldViolations() {
		if violationField(v) == field {
			return true
		}
	}
	return false
}

// alreadyExistsError is the error for a package rule that selects the same
// package as a rule at head. The server names that rule in a ResourceInfo
// detail. Without the detail, the status message is the fallback.
func alreadyExistsError(err error, cause error) error {
	if info, ok := usefulerror.DetailAs[*errdetails.ResourceInfo](err); ok && info.GetResourceName() != "" {
		id := tui.EscapeLine(info.GetResourceName())
		return newPolicyError(usefulerror.ErrConflict,
			fmt.Sprintf("Package rule %s already selects this package.", id),
			"See it with: safedep policy rule show "+id,
			cause)
	}
	return newPolicyError(usefulerror.ErrConflict, describeStatus(err),
		"List the rules: safedep policy rule list", cause)
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
			return failedPreconditionError(err, changePreconditionHelp(changeID), cause)
		case codes.InvalidArgument:
			return invalidArgumentError(err, "Correct the values and run the command again", cause)
		case codes.AlreadyExists:
			return alreadyExistsError(err, cause)
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
			return failedPreconditionError(err, "List the rules: safedep policy rule list", cause)
		case codes.InvalidArgument:
			return invalidArgumentError(err, fmt.Sprintf("See the flags: safedep policy rule %s --help", op), cause)
		case codes.AlreadyExists:
			return alreadyExistsError(err, cause)
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
			"The server refused the filters: "+describeStatus(err),
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
