package policy

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/safedep/cli/internal/cloudpolicy/mocks"
)

// scriptedEditor plays one edit for each open. An edit gets the file text
// and returns the text to save.
type scriptedEditor struct {
	edits []func(string) string
	seen  []string
}

func (e *scriptedEditor) open(_ context.Context, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	e.seen = append(e.seen, string(b))
	if len(e.edits) == 0 {
		return errors.New("unexpected editor open")
	}
	edit := e.edits[0]
	e.edits = e.edits[1:]
	return os.WriteFile(path, []byte(edit(string(b))), 0o600)
}

func replace(old, new string) func(string) string {
	return func(s string) string { return strings.Replace(s, old, new, 1) }
}

func keep(s string) string { return s }

func TestEditLoop_NoChange(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{keep}}
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), func([]byte) error {
		t.Fatal("save must not run")
		return nil
	})
	assert.ErrorIs(t, err, errNoChange)
}

func TestEditLoop_ReopensWithTheErrorAndSaves(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{
		replace("a: 1", "a: bad"),
		replace("a: bad", "a: 2"),
	}}
	var saved []string
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), func(text []byte) error {
		saved = append(saved, string(text))
		if strings.Contains(string(text), "bad") {
			return retryable(errors.New("line 1: a: expected a number"))
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a: bad\n", "a: 2\n"}, saved, "the error lines never reach save")
	require.Len(t, ed.seen, 2)
	assert.Contains(t, ed.seen[1], "# ! The last save failed:")
	assert.Contains(t, ed.seen[1], "line 1: a: expected a number")
}

func TestEditLoop_ServerRejectionIsRetryable(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{replace("1", "2"), replace("2", "3")}}
	calls := 0
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), func([]byte) error {
		calls++
		if calls == 1 {
			return status.Error(codes.InvalidArgument, "override priorities must be unique")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestEditLoop_StopsWhenTheUserGivesUp(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{replace("1", "x"), keep}}
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), func([]byte) error {
		return retryable(errors.New("bad"))
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the file still has errors")
}

func TestEditLoop_KeepsTheFileOnAnRPCFailure(t *testing.T) {
	ed := &scriptedEditor{edits: []func(string) string{replace("1", "2")}}
	err := editLoop(context.Background(), ed.open, []byte("a: 1\n"), func([]byte) error {
		return status.Error(codes.Unavailable, "down")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "down")
}

func TestRunEdit_CreatesAChangeFromTheHead(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)

	var req *policyv1.CreatePolicyChangeRequest
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			req = r
			c := openChange("18")
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(c)
			return res, nil
		})

	ed := &scriptedEditor{edits: []func(string) string{
		replace("custom/criticality: high}\n    spec:\n      scan_gate:\n        fail_on: high",
			"custom/criticality: high}\n    spec:\n      scan_gate:\n        fail_on: medium"),
	}}
	change, err := runEdit(context.Background(), client, ed.open, "platform-eng.acme.safedep.io",
		editInput{Title: "payments: medium"})
	require.NoError(t, err)
	assert.Equal(t, "18", change.GetChangeId())

	header := ed.seen[0]
	assert.Contains(t, header, "# Policy of platform-eng.acme.safedep.io · head revision 46")
	assert.Contains(t, header, "#   1. SafeDep default policy (read-only, revision 12):")
	assert.Contains(t, header, "scan_gate.fail_on: high")
	assert.Contains(t, header, "# Package rules are not in this file: 211 rules")

	require.NotNil(t, req)
	assert.Equal(t, "payments: medium", req.GetTitle())
	assert.Equal(t, uint64(46), req.GetBaseRevision())
	overrides := req.GetSettings().GetOverrides()
	require.Len(t, overrides, 2)
	assert.Equal(t, "ov_pay", overrides[0].GetOverrideId(), "the edit keeps the ids of the overrides")
	assert.Equal(t, scangatev1.Severity_SEVERITY_MEDIUM, overrides[0].GetSpec().GetScanGate().GetFailOn())
	assert.Equal(t, "ov_ci", overrides[1].GetOverrideId())
}

func TestRunEdit_OneOverride(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	var req *policyv1.CreatePolicyChangeRequest
	client.EXPECT().CreatePolicyChange(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, r *policyv1.CreatePolicyChangeRequest, _ ...grpc.CallOption) (*policyv1.CreatePolicyChangeResponse, error) {
			req = r
			res := &policyv1.CreatePolicyChangeResponse{}
			res.SetChange(openChange("19"))
			return res, nil
		})

	ed := &scriptedEditor{edits: []func(string) string{replace("fail_on: critical", "fail_on: high")}}
	_, err := runEdit(context.Background(), client, ed.open, "", editInput{Override: "ci"})
	require.NoError(t, err)
	assert.NotContains(t, ed.seen[0], "payments", "only the named override opens")
	assert.Contains(t, ed.seen[0], "# Policy of this tenant")

	overrides := req.GetSettings().GetOverrides()
	require.Len(t, overrides, 2)
	assert.Equal(t, scangatev1.Severity_SEVERITY_HIGH, overrides[0].GetSpec().GetScanGate().GetFailOn(), "payments stays")
	assert.Equal(t, "ov_ci", overrides[1].GetOverrideId())
	assert.Equal(t, scangatev1.Severity_SEVERITY_HIGH, overrides[1].GetSpec().GetScanGate().GetFailOn())
}

func TestRunEdit_UnknownOverride(t *testing.T) {
	client := mocks.NewPolicyServiceClientMock(t)
	client.EXPECT().GetPolicy(mock.Anything, mock.Anything).Return(headResponse(), nil)
	_, err := runEdit(context.Background(), client, (&scriptedEditor{}).open, "", editInput{Override: "laptops"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no policy override is named "laptops": known overrides: ci, payments`)
}

func TestValidateSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*policyv1.Policy)
		want   string
	}{
		{"valid", func(*policyv1.Policy) {}, ""},
		{"duplicate name", func(p *policyv1.Policy) { p.GetOverrides()[1].SetName("payments") }, `two overrides are named "payments"`},
		{"duplicate priority", func(p *policyv1.Policy) { p.GetOverrides()[1].SetPriority(200) }, "priority 200 is also the priority"},
		{"no priority", func(p *policyv1.Policy) { p.GetOverrides()[0].SetPriority(0) }, "give it a priority"},
		{"no selector", func(p *policyv1.Policy) { p.GetOverrides()[0].ClearSelector() }, "give it a selector"},
		{"no name", func(p *policyv1.Policy) { p.GetOverrides()[0].SetName("") }, "give the override a name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := headPolicy()
			tt.mutate(p)
			err := validateSettings(p)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.want)
		})
	}
}
