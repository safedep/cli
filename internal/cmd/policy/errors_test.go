package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/usefulerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

// encodedFieldPath is a field path as the server sends it today: the
// protobuf text of a protovalidate FieldPath.
func encodedFieldPath(names ...string) string {
	path := &validate.FieldPath{}
	for _, name := range names {
		el := &validate.FieldPathElement{}
		n, index, indexed := strings.Cut(name, "[")
		el.SetFieldName(n)
		el.SetFieldType(descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
		if indexed {
			var i uint64
			_, err := fmt.Sscanf(strings.TrimSuffix(index, "]"), "%d", &i)
			if err != nil {
				panic(err)
			}
			el.SetIndex(i)
		}
		path.SetElements(append(path.GetElements(), el))
	}
	return prototext.Format(path)
}

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

func TestDescribeStatus(t *testing.T) {
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
			name: "a wrapped status",
			err: usefulerror.NewUsefulError().WithHumanError("x").
				Wrap(fmt.Errorf("policy change create: %w", status.Error(codes.InvalidArgument, "bad title"))),
			want: "bad title",
		},
		{
			name: "a violation with the message as its description",
			err: statusWithViolations(codes.InvalidArgument,
				"service execution failed: unknown label prefix \"team/\"",
				violation("settings.overrides[0].selector.match_labels", "unknown label prefix \"team/\"")),
			want: "settings.overrides[0].selector.match_labels: unknown label prefix \"team/\"",
		},
		{
			name: "a protovalidate violation with an encoded field path",
			err: statusWithViolations(codes.InvalidArgument,
				"service execution failed: API validation error: validation error:\n - settings.overrides[0].name: "+
					"value does not match regex pattern `^[a-z0-9]$` [string.pattern]",
				violation(encodedFieldPath("settings", "overrides[0]", "name"),
					"value does not match regex pattern `^[a-z0-9]$`")),
			want: "settings.overrides[0].name: value does not match regex pattern `^[a-z0-9]$`",
		},
		{
			name: "a message that the violations do not hold",
			err: statusWithViolations(codes.InvalidArgument, "the request is not valid",
				violation("title", "must be at most 200 characters"),
				violation("settings.base_description", "must be at most 1000 characters")),
			want: "the request is not valid\ntitle: must be at most 200 characters\n" +
				"settings.base_description: must be at most 1000 characters",
		},
		{
			name: "control characters from the server",
			err:  status.Error(codes.InvalidArgument, "bad\x1b[2J"),
			want: `bad\x1b[2J`,
		},
		{
			name: "an error that is not a status",
			err:  errors.New("line 1: bad"),
			want: "line 1: bad",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeStatus(tt.err)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "rpc error")
		})
	}
}

func alreadyExists(ruleID string) error {
	st := status.New(codes.AlreadyExists, "service execution failed: a package rule already selects pkg:npm/leftpad")
	if ruleID == "" {
		return st.Err()
	}
	st, err := st.WithDetails(&errdetails.ResourceInfo{ResourceType: "package_rule", ResourceName: ruleID})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func TestServerErrorMapping_InvalidArgumentAndAlreadyExists(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantHuman string
		wantHelp  string
	}{
		{
			name: "a change with a field out of range",
			err: mapChangeError("create", "", statusWithViolations(codes.InvalidArgument,
				"service execution failed: API validation error",
				violation(encodedFieldPath("settings", "base", "package_admission", "cooldown", "min_age"),
					"value must be greater than or equal to 0s and less than or equal to 8760h0m0s"))),
			wantCode: usefulerror.ErrBadRequest,
			wantHuman: "API validation error\nsettings.base.package_admission.cooldown.min_age: " +
				"value must be greater than or equal to 0s and less than or equal to 8760h0m0s",
			wantHelp: "Correct the values and run the command again",
		},
		{
			name:      "a rule with a bad flag",
			err:       mapRuleError("create", "", badRequest("rules[0].reason", "value is required")),
			wantCode:  usefulerror.ErrBadRequest,
			wantHuman: "rules[0].reason: value is required",
			wantHelp:  "See the flags: safedep policy rule create --help",
		},
		{
			name:      "a rule create that duplicates a rule",
			err:       mapRuleError("create", "", alreadyExists("r_7f2")),
			wantCode:  usefulerror.ErrConflict,
			wantHuman: "Package rule r_7f2 already selects this package.",
			wantHelp:  "See it with: safedep policy rule show r_7f2",
		},
		{
			name:      "a change that duplicates a rule",
			err:       mapChangeError("publish", "3", alreadyExists("r_7f2")),
			wantCode:  usefulerror.ErrConflict,
			wantHuman: "Package rule r_7f2 already selects this package.",
			wantHelp:  "See it with: safedep policy rule show r_7f2",
		},
		{
			name:      "a duplicate with no resource detail",
			err:       mapRuleError("create", "", alreadyExists("")),
			wantCode:  usefulerror.ErrConflict,
			wantHuman: "a package rule already selects pkg:npm/leftpad",
			wantHelp:  "List the rules: safedep policy rule list",
		},
		{
			name:      "a precondition with the service prefix",
			err:       mapRuleError("delete", "r_1", status.Error(codes.FailedPrecondition, "service execution failed: gone")),
			wantCode:  usefulerror.ErrConflict,
			wantHuman: "The server refused the request: gone",
			wantHelp:  "List the rules: safedep policy rule list",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ue, ok := usefulerror.AsUsefulError(tt.err)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, ue.Code())
			assert.Equal(t, tt.wantHuman, ue.HumanError())
			assert.Equal(t, tt.wantHelp, ue.Help())
		})
	}
}

func TestKeptEditError_Help(t *testing.T) {
	tests := []struct {
		name  string
		cause error
		want  string
	}{
		{
			name:  "a converted gRPC error with a full stop",
			cause: status.Error(codes.Unavailable, "down"),
			want:  "The service is temporarily unavailable. Retry after a short delay. Your edit is in /tmp/x.yaml",
		},
		{
			name:  "a useful error with no help",
			cause: usefulerror.NewUsefulError().WithCode(usefulerror.ErrBadRequest).WithHumanError("bad"),
			want:  "Your edit is in /tmp/x.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ue, ok := usefulerror.AsUsefulError(keptEditError("/tmp/x.yaml", tt.cause))
			require.True(t, ok)
			assert.True(t, strings.HasPrefix(ue.Help(), tt.want), "help: %s", ue.Help())
			assert.NotContains(t, ue.Help(), "..")
		})
	}
}

func TestRunEdit_ServerFieldErrorReopensWithTheField(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	calls := 0
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, *policyv1.CreatePolicyChangeRequest, ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			calls++
			if calls == 1 {
				return nil, statusWithViolations(codes.InvalidArgument,
					`service execution failed: unknown label prefix "team/"`,
					violation(encodedFieldPath("settings", "overrides[0]", "selector"), `unknown label prefix "team/"`))
			}
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(openChange("18"))
			return res, nil
		})

	ed := &scriptedEditor{edits: []func(string) string{
		replace("custom/criticality", "team/criticality"),
		replace("team/criticality", "custom/criticality"),
	}}
	change, err := runEdit(context.Background(), client, ed.open, "", editInput{})
	require.NoError(t, err)
	assert.Equal(t, "18", change.GetChangeId())
	require.Len(t, ed.seen, 2)
	assert.Contains(t, ed.seen[1], "# !   settings.overrides[0].selector: unknown label prefix \"team/\"\n")
	assert.NotContains(t, ed.seen[1], "rpc error")
	assert.NotContains(t, ed.seen[1], "service execution failed")
}

func TestRunEdit_ValueOutOfRangeReopensBeforeTheServer(t *testing.T) {
	tests := []struct {
		name string
		bad  func(string) string
		fix  func(string) string
		want string
	}{
		{
			name: "a min_age above the limit",
			bad: func(s string) string {
				s = uncomment("  # package_admission:", "    # cooldown:", "      # min_age: 7d")(s)
				return strings.Replace(s, "min_age: 7d", "min_age: 400d", 1)
			},
			fix:  replace("min_age: 400d", "min_age: 30d"),
			want: "# !   base.package_admission.cooldown.min_age: must be greater than or equal to 0s",
		},
		{
			name: "an override name with an upper case letter",
			bad:  replace("name: payments", "name: Payments"),
			fix:  replace("name: Payments", "name: payments-team"),
			want: "# !   overrides[0].name: does not match regex pattern",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mocks.NewPolicyServiceClientMock(t)
			client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
			var got *policyv1.Policy
			createdSettings(client, &got)

			ed := &scriptedEditor{edits: []func(string) string{tt.bad, tt.fix}}
			_, err := runEdit(context.Background(), client, ed.open, "", editInput{})
			require.NoError(t, err)
			require.Len(t, ed.seen, 2)
			assert.Contains(t, ed.seen[1], tt.want)
			require.NotNil(t, got)
			client.AssertNumberOfCalls(t, "CreatePolicyChange", 1)
		})
	}
}
