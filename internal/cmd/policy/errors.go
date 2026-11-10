package policy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	errorv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/error/v1"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/safedep/cli/internal/cloudpolicy"
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

// stripServicePrefix removes each leading serviceFailedPrefix of a status
// message. A server that wraps an error two times sends the prefix two
// times, and a field name after one prefix must still read as the field.
func stripServicePrefix(msg string) string {
	msg = strings.TrimSpace(msg)
	for strings.HasPrefix(msg, serviceFailedPrefix) {
		msg = strings.TrimSpace(strings.TrimPrefix(msg, serviceFailedPrefix))
	}
	return msg
}

// noReasonText is the text of a status that has no message and no field
// violation.
const noReasonText = "The server gave no reason"

// describeStatus is the text of a gRPC status error for a person, with each
// field path in the form of the edit document. See describeStatusAt.
func describeStatus(err error) string {
	return describeStatusAt(err, documentPath)
}

// describeStatusAt is the text of a gRPC status error for a person: the
// status message, then each field violation of a BadRequest detail as
// "field: description", with the field from path. The text of
// status.FromError on a wrapped error starts with "rpc error: code = ...
// desc =", so this reads the status of the innermost error. When the
// message already holds the description of each violation, the violations
// replace it, so no text shows two times.
func describeStatusAt(err error, path func(string) (string, bool)) string {
	st, ok := innerStatus(err)
	if !ok {
		return tui.EscapeText(err.Error())
	}
	msg := stripServicePrefix(st.Message())
	var lines []string
	covered := true
	if bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err); ok {
		for _, v := range bad.GetFieldViolations() {
			field, isDuration := path(violationField(v))
			description := v.GetDescription()
			if isDuration {
				description = cloudpolicy.DocumentDurations(description)
			}
			if line := violationLine(field, description); line != "" {
				lines = append(lines, tui.EscapeLine(line))
			}
			covered = covered && v.GetDescription() != "" && strings.Contains(msg, v.GetDescription())
		}
	}
	if msg != "" && (len(lines) == 0 || !covered) {
		lines = append([]string{tui.EscapeText(msg)}, lines...)
	}
	if len(lines) == 0 {
		return noReasonText
	}
	return strings.Join(lines, "\n")
}

func violationLine(field, description string) string {
	switch {
	case field == "":
		return description
	case description == "":
		return field
	}
	return field + ": " + description
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

// violationField is the field of a violation as a dotted path, as in
// "settings.overrides[0].name". The server sends the protovalidate field
// path of a request as the protobuf text of a buf.validate.FieldPath, which
// no person can read. A field that does not parse as that text stays as it
// is.
func violationField(v *errdetails.BadRequest_FieldViolation) string {
	field := v.GetField()
	path := &validate.FieldPath{}
	if err := prototext.Unmarshal([]byte(field), path); err != nil || len(path.GetElements()) == 0 {
		return field
	}
	return fieldPathString(path)
}

// fieldPathString writes a field path as protovalidate.FieldPathString
// does. The CLI does not import protovalidate, because its CEL runtime makes
// the binary 18% larger.
func fieldPathString(path *validate.FieldPath) string {
	var b strings.Builder
	for i, el := range path.GetElements() {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(el.GetFieldName())
		var key string
		switch el.WhichSubscript() {
		case validate.FieldPathElement_Index_case:
			key = strconv.FormatUint(el.GetIndex(), 10)
		case validate.FieldPathElement_BoolKey_case:
			key = strconv.FormatBool(el.GetBoolKey())
		case validate.FieldPathElement_IntKey_case:
			key = strconv.FormatInt(el.GetIntKey(), 10)
		case validate.FieldPathElement_UintKey_case:
			key = strconv.FormatUint(el.GetUintKey(), 10)
		case validate.FieldPathElement_StringKey_case:
			key = strconv.Quote(el.GetStringKey())
		default:
			continue
		}
		b.WriteString("[" + key + "]")
	}
	return b.String()
}

// documentFieldPrefix is the request field that holds the edit document.
// The CLI shows a document path without it, as the local checks do, so a
// path names a key of the file.
const documentFieldPrefix = "settings."

// documentPath is a field path of a request as the path of the key in the
// edit document, and whether the field is a duration. Only a path below
// documentFieldPrefix is in the document.
func documentPath(field string) (string, bool) {
	rest, ok := strings.CutPrefix(field, documentFieldPrefix)
	if !ok {
		return field, false
	}
	return cloudpolicy.DocumentPath(rest)
}

// overrideFieldError marks a save error of an edit of one policy override.
// The file of that edit holds the override only, so a field path of it
// shows without "overrides[i].".
type overrideFieldError struct {
	err   error
	index int
}

func (e *overrideFieldError) Error() string { return e.err.Error() }
func (e *overrideFieldError) Unwrap() error { return e.err }

func (e *overrideFieldError) path(field string) (string, bool) {
	field, isDuration := documentPath(field)
	prefix := fmt.Sprintf("overrides[%d]", e.index)
	if field == prefix {
		return "", isDuration
	}
	if rest, ok := strings.CutPrefix(field, prefix+"."); ok {
		return rest, isDuration
	}
	return field, isDuration
}

// errorText is the text of err for a person, for the error lines of the
// edit file. An INVALID_ARGUMENT of an edit of one policy override shows its
// field paths as paths of the file. Other errors show the human error of a
// useful error in the chain, else the text of describeStatus.
func errorText(err error) string {
	var oe *overrideFieldError
	if st, ok := innerStatus(err); ok && st.Code() == codes.InvalidArgument && errors.As(err, &oe) {
		return describeStatusAt(err, oe.path)
	}
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
			fmt.Sprintf("Package rule %s already selects this package", id),
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
			return failedPreconditionError(err, changePreconditionHelp(op, changeID), cause)
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
func changePreconditionHelp(op, changeID string) string {
	// A delete has one refusal: the change is not open.
	if op == "delete" {
		return "List the open changes: safedep policy change list --state open"
	}
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
		return revisionNotFoundError(fromRevision, fmt.Errorf("policy change create: %w", err))
	}
	return mapChangeError("create", "", err)
}

// mapRevisionError gives the recovery for a read of a policy revision that
// does not exist.
func mapRevisionError(op string, revision uint64, err error) error {
	cause := fmt.Errorf("%s: %w", op, err)
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return revisionNotFoundError(revision, cause)
	}
	return cause
}

func revisionNotFoundError(revision uint64, cause error) error {
	return newPolicyError(usefulerror.ErrNotFound,
		fmt.Sprintf("Policy revision %d does not exist", revision),
		"List the revisions: safedep policy revision list",
		cause)
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
		if violatesField(err, "pagination.page_token") {
			return newPolicyError(usefulerror.ErrBadRequest,
				"The page token is not valid",
				"Run the command again without --page-token",
				cause)
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
