package rpcerror

import (
	"errors"
	"fmt"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
)

func statusWithViolations(code codes.Code, message string, violations ...*errdetails.BadRequest_FieldViolation) error {
	st, err := status.New(code, message).WithDetails(&errdetails.BadRequest{FieldViolations: violations})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func violation(field, description string) *errdetails.BadRequest_FieldViolation {
	return &errdetails.BadRequest_FieldViolation{Field: field, Description: description}
}

// fieldPath is settings.overrides[2].selector.match_labels["team/x"] as
// the protobuf text of a protovalidate FieldPath, as the server sends it.
func fieldPath() string {
	el := func(name string) *validate.FieldPathElement {
		e := &validate.FieldPathElement{}
		e.SetFieldName(name)
		return e
	}
	overrides, labels := el("overrides"), el("match_labels")
	overrides.SetIndex(2)
	labels.SetStringKey("team/x")
	path := &validate.FieldPath{}
	path.SetElements([]*validate.FieldPathElement{el("settings"), overrides, el("selector"), labels})
	return prototext.Format(path)
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a status with no detail",
			err:  status.Error(codes.InvalidArgument, "override priorities must be unique"),
			want: "override priorities must be unique",
		},
		{
			name: "the service prefix of the server",
			err:  status.Error(codes.InvalidArgument, "service execution failed: override priorities must be unique"),
			want: "override priorities must be unique",
		},
		{
			name: "a doubled service prefix",
			err:  status.Error(codes.InvalidArgument, "service execution failed:  service execution failed: bad"),
			want: "bad",
		},
		{
			name: "a wrapped status",
			err: usefulerror.NewUsefulError().WithHumanError("x").
				Wrap(fmt.Errorf("policy change create: %w", status.Error(codes.InvalidArgument, "bad title"))),
			want: "bad title",
		},
		{
			name: "a violation with the message as its description",
			err: statusWithViolations(codes.InvalidArgument, "service execution failed: name: too long",
				violation("name", "too long")),
			want: "name: too long",
		},
		{
			name: "a message that the violations do not hold",
			err: statusWithViolations(codes.InvalidArgument, "the request is not valid",
				violation("title", "must be at most 200 characters"), violation("repositories[1]", "acme/x is not visible")),
			want: "the request is not valid\ntitle: must be at most 200 characters\nrepositories[1]: acme/x is not visible",
		},
		{
			name: "an encoded field path with subscripts",
			err:  statusWithViolations(codes.InvalidArgument, "", violation(fieldPath(), "unknown label prefix")),
			want: `settings.overrides[2].selector.match_labels["team/x"]: unknown label prefix`,
		},
		{
			name: "a status with no message",
			err:  status.Error(codes.InvalidArgument, ""),
			want: NoReasonText,
		},
		{
			name: "a violation with no description",
			err:  statusWithViolations(codes.InvalidArgument, "", violation("title", "")),
			want: "title",
		},
		{
			name: "control characters from the server",
			err:  status.Error(codes.InvalidArgument, "bad\x1b[2J"),
			want: `bad\x1b[2J`,
		},
		{
			name: "an error that is not a status",
			err:  errors.New("line 1: bad\x1b[2J\nline 2"),
			want: "line 1: bad\\x1b[2J\nline 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Describe(tt.err, AsIs)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "rpc error")
		})
	}
}

func TestDescribe_ViolationText(t *testing.T) {
	err := statusWithViolations(codes.NotFound, "the installation cannot see a repository",
		violation("repositories[0]", "acme/x is not visible"))
	got := Describe(err, func(_, description string) (string, string) { return "", description })
	assert.Equal(t, "the installation cannot see a repository\nacme/x is not visible", got)
}
