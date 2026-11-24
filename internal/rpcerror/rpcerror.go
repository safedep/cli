// Package rpcerror turns a gRPC status error from SafeDep Cloud into text
// for a person: the status message without the service prefix of the
// server, and each field violation of a BadRequest detail with a readable
// field path. It lives outside internal/cmd because sibling command
// packages must not import each other.
package rpcerror

import (
	"errors"
	"strconv"
	"strings"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/safedep/cli/internal/tui"
)

// serviceFailedPrefix is the text that the server puts before the message of
// a service error. It names no field and no fix, so the CLI removes it.
const serviceFailedPrefix = "service execution failed: "

// NoReasonText is the text of a status that has no message and no field
// violation.
const NoReasonText = "The server gave no reason"

// ViolationText gives the field and the description that a person sees for
// one field violation. The field is the dotted path of ViolationField.
type ViolationText func(field, description string) (string, string)

// AsIs shows each field violation as the server sent it.
func AsIs(field, description string) (string, string) { return field, description }

// Describe is the text of a gRPC status error for a person: the status
// message, then each field violation of a BadRequest detail as
// "field: description", with the text from text. The text of
// status.FromError on a wrapped error starts with "rpc error: code = ...
// desc =", so this reads the status of the innermost error. When the
// message already holds the description of each violation, the violations
// replace it, so no text shows two times.
func Describe(err error, text ViolationText) string {
	st, ok := InnerStatus(err)
	if !ok {
		return tui.EscapeText(err.Error())
	}
	msg := StripServicePrefix(st.Message())
	var lines []string
	covered := true
	if bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err); ok {
		for _, v := range bad.GetFieldViolations() {
			field, description := text(ViolationField(v), v.GetDescription())
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
		return NoReasonText
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

// StripServicePrefix removes each leading service prefix of a status
// message. A server that wraps an error two times sends the prefix two
// times, and a field name after one prefix must still read as the field.
func StripServicePrefix(msg string) string {
	msg = strings.TrimSpace(msg)
	for strings.HasPrefix(msg, serviceFailedPrefix) {
		msg = strings.TrimSpace(strings.TrimPrefix(msg, serviceFailedPrefix))
	}
	return msg
}

// InnerStatus is the status of the innermost gRPC error in the chain of err,
// with its message as the server sent it.
func InnerStatus(err error) (*status.Status, bool) {
	var gs interface{ GRPCStatus() *status.Status }
	if !errors.As(err, &gs) {
		return nil, false
	}
	st := gs.GRPCStatus()
	return st, st != nil
}

// ViolationField is the field of a violation as a dotted path, as in
// "settings.overrides[0].name". The server sends the protovalidate field
// path of a request as the protobuf text of a buf.validate.FieldPath, which
// no person can read. A field that does not parse as that text stays as it
// is.
func ViolationField(v *errdetails.BadRequest_FieldViolation) string {
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
