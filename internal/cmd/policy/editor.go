package policy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	maxEditAttempts = 5
	errorLinePrefix = "# ! "
)

// openEditorFunc opens a file in the editor of the user and returns when
// the user closes it.
type openEditorFunc func(ctx context.Context, path string) error

// errNoChange stops an edit that the user saved with no change.
var errNoChange = errors.New("no change")

// retryableError marks a save error that the user can fix in the editor: a
// document that does not parse, or a change that the server rejects as
// invalid.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func retryable(err error) error { return &retryableError{err: err} }

// documentFields are the request fields that the edit document holds. An
// INVALID_ARGUMENT about one of them is an error that an edit can fix.
var documentFields = []string{"settings", "base", "base_description", "overrides"}

func isRetryable(err error) bool {
	var r *retryableError
	if errors.As(err, &r) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		return false
	}
	if bad, ok := usefulerror.DetailAs[*errdetails.BadRequest](err); ok && len(bad.GetFieldViolations()) > 0 {
		for _, v := range bad.GetFieldViolations() {
			if isDocumentField(v.GetField()) {
				return true
			}
		}
		return false
	}
	// With no field detail, the message starts with the field, as in
	// "title: must be at most 200 characters".
	field, _, found := strings.Cut(st.Message(), ":")
	return !found || strings.ContainsAny(field, " ") || isDocumentField(field)
}

func isDocumentField(field string) bool {
	for _, f := range documentFields {
		if field == f || strings.HasPrefix(field, f+".") || strings.HasPrefix(field, f+"[") {
			return true
		}
	}
	return false
}

// checkMaxLength refuses a flag value that the server refuses for its
// length, from the protovalidate rule of the request field, before the RPC.
// An edit cannot fix such a value, so the editor must not open again for it.
func checkMaxLength(flag, value string, request proto.Message, field protoreflect.Name) error {
	fd := request.ProtoReflect().Descriptor().Fields().ByName(field)
	if fd == nil {
		return nil
	}
	rules, ok := proto.GetExtension(fd.Options(), validate.E_Field).(*validate.FieldRules)
	if !ok || !rules.GetString().HasMaxLen() {
		return nil
	}
	limit := rules.GetString().GetMaxLen()
	var n uint64
	for range value {
		n++
	}
	if n > limit {
		return invalidInputError(fmt.Errorf("%s is %d characters long: the limit is %d", flag, n, limit),
			fmt.Sprintf("Give a %s of at most %d characters", flag, limit))
	}
	return nil
}

// editLoop writes text to a temporary file, opens the editor on it, and
// gives the saved text to save. unchanged reports whether the saved text
// means the same as text. It compares decoded messages, so an editor that
// only rewrites line endings or spacing makes no change. When save fails with an error that the user
// can fix, the loop opens the file again with the error at the top. The file
// stays on disk when the edit fails for another reason, so no work is lost.
func editLoop(
	ctx context.Context,
	open openEditorFunc,
	text []byte,
	unchanged func(saved []byte) bool,
	save func([]byte) error,
) error {
	f, err := os.CreateTemp("", "safedep-policy-*.yaml")
	if err != nil {
		return fmt.Errorf("policy edit: temporary file: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(text); err != nil {
		return errors.Join(fmt.Errorf("policy edit: write %s: %w", path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("policy edit: write %s: %w", path, err)
	}

	last := text
	for attempt := 0; attempt < maxEditAttempts; attempt++ {
		if err := open(ctx, path); err != nil {
			return keptEditError(path, err)
		}
		saved, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("policy edit: read %s: %w", path, err)
		}
		saved = stripErrorLines(saved)
		if attempt == 0 && unchanged(saved) {
			return errors.Join(errNoChange, os.Remove(path))
		}
		if attempt > 0 && bytes.Equal(saved, stripErrorLines(last)) {
			return keptEditError(path, errors.New("the file still has errors"))
		}

		err = save(saved)
		if err == nil {
			if rmErr := os.Remove(path); rmErr != nil {
				tui.Warning("cannot remove the temporary file %s: %v", path, rmErr)
			}
			return nil
		}
		if !isRetryable(err) {
			return keptEditError(path, err)
		}
		last = append(errorLines(err), saved...)
		if err := os.WriteFile(path, last, 0o600); err != nil { //nolint:gosec // G703: the CLI made the path with os.CreateTemp.
			return fmt.Errorf("policy edit: write %s: %w", path, err)
		}
	}
	return keptEditError(path, fmt.Errorf("the file still has errors after %d saves", maxEditAttempts))
}

func errorLines(err error) []byte {
	var b strings.Builder
	b.WriteString(errorLinePrefix + "The last save failed:\n")
	for _, line := range strings.Split(err.Error(), "\n") {
		b.WriteString(errorLinePrefix + "  " + line + "\n")
	}
	b.WriteString(errorLinePrefix + "Fix the file and save it again. Close it with no change to stop.\n")
	return []byte(b.String())
}

func stripErrorLines(text []byte) []byte {
	for bytes.HasPrefix(text, []byte(errorLinePrefix)) {
		i := bytes.IndexByte(text, '\n')
		if i < 0 {
			return nil
		}
		text = text[i+1:]
	}
	return text
}

// keptEditError keeps the code, the message and the help of a useful
// error, such as the one for a role that cannot change the policy, and adds
// the path of the kept file.
func keptEditError(path string, cause error) error {
	code, human, help := usefulerror.ErrBadRequest, "The command did not save the policy edit: "+cause.Error(), ""
	if ue, ok := usefulerror.AsUsefulError(cause); ok {
		code, human = ue.Code(), "The command did not save the policy edit: "+ue.HumanError()
		if ue.Help() != "" {
			help = ue.Help() + ". "
		}
	}
	return usefulerror.NewUsefulError().
		WithCode(code).
		WithHumanError(human).
		WithHelp(help + "Your edit is in " + path + ". Copy what you need from it, then run the command again").
		Wrap(cause)
}
