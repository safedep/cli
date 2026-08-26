package jfrog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"github.com/safedep/cli/internal/tui"
	tuioutput "github.com/safedep/dry/tui/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStreams redirects the shared dry/tui stdout and stderr to buffers for
// the test, restoring the real writers on cleanup. It returns (stdout, stderr)
// so a test can assert that -o json writes results to stdout and nothing to
// stderr.
func captureStreams(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	tuioutput.SetWriters(&out, &errb)
	t.Cleanup(func() { tuioutput.SetWriters(os.Stdout, os.Stderr) })
	return &out, &errb
}

// jsonLines splits captured JSONL into non-empty trimmed lines.
func jsonLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "each stdout line must be valid JSON: %q", line)
		events = append(events, m)
	}
	return events
}

func jsonReporter() *reporter { return newReporter(tui.NewPrinter(tui.ModeJSON)) }

func TestJSONEvent_MarshalsFlatOmitEmpty(t *testing.T) {
	b, err := jsonEvent{Event: eventPushed, Package: "foo", IssueID: "SD-1", Status: 201}.RenderJSON()
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, eventPushed, m["event"])
	assert.Equal(t, "foo", m["package"])
	assert.Equal(t, "SD-1", m["issue_id"])
	assert.EqualValues(t, 201, m["status"])
	_, hasVersions := m["versions"]
	assert.False(t, hasVersions, "empty fields must be omitted")
}

func TestNewReporter_JSONByMode(t *testing.T) {
	assert.True(t, newReporter(tui.NewPrinter(tui.ModeJSON)).json)
	assert.False(t, newReporter(tui.NewPrinter(tui.ModeTable)).json)
	assert.False(t, newReporter(tui.NewPrinter(tui.ModePlain)).json)
	assert.False(t, newReporter(nil).json, "nil printer must not panic and defaults to human")
}

// TestReporter_JSON_ResultOnlyOnStdout is the core of abhisek's ask: under
// -o json, results go to stdout as JSONL and every log is suppressed, so
// nothing lands on stderr.
func TestReporter_JSON_ResultOnlyOnStdout(t *testing.T) {
	out, errb := captureStreams(t)
	r := jsonReporter()

	r.logInfo("connectivity ok")
	r.logSuccess("pushed something")
	r.logWarn("transient error")
	r.logDim("already pushed")
	assert.Empty(t, out.String(), "logs must not reach stdout")
	assert.Empty(t, errb.String(), "logs must be suppressed entirely under -o json")

	r.result(func() { t.Fatal("human closure must not run under -o json") },
		jsonEvent{Event: eventPushed, Package: "a"})

	events := jsonLines(t, out.String())
	require.Len(t, events, 1)
	assert.Equal(t, eventPushed, events[0]["event"])
	assert.Empty(t, errb.String(), "a result writes JSON to stdout, nothing to stderr")
}

// TestReporter_Human_LogsToStderrNotStdout confirms human mode is unchanged:
// logs go to stderr, stdout stays empty.
func TestReporter_Human_LogsToStderrNotStdout(t *testing.T) {
	out, errb := captureStreams(t)
	r := newReporter(tui.NewPrinter(tui.ModeTable))

	r.logInfo("hello world")
	assert.Empty(t, out.String(), "human logs never touch stdout")
	assert.Contains(t, errb.String(), "hello world")

	ran := false
	r.result(func() { ran = true }, jsonEvent{Event: eventPushed})
	assert.True(t, ran, "human mode runs the result closure")
	assert.Empty(t, out.String(), "human mode writes no JSON to stdout")
}

// TestService_JSON_OnlyStateChanges asserts that under -o json only a real
// state change reaches stdout, and no-ops produce nothing on any stream.
func TestService_JSON_OnlyStateChanges(t *testing.T) {
	tests := []struct {
		name      string
		fake      *fakeXrayClient
		withdrawn bool
		wantEvent string // "" means no output at all (it is a suppressed log)
	}{
		{name: "pushed is output", fake: &fakeXrayClient{pushStat: http.StatusCreated}, wantEvent: eventPushed},
		{name: "deleted is output", fake: &fakeXrayClient{delStat: http.StatusOK}, withdrawn: true, wantEvent: eventDeleted},
		{name: "already pushed is suppressed", fake: &fakeXrayClient{pushStat: http.StatusBadRequest}, wantEvent: ""},
		{name: "does not exist is suppressed", fake: &fakeXrayClient{delStat: http.StatusNotFound}, withdrawn: true, wantEvent: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errb := captureStreams(t)
			svc := newFeedService(nil, tt.fake, jsonReporter())

			report := newTestReport("r-1", "pkg-a", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
			report.SetWithdrawn(tt.withdrawn)
			require.NoError(t, svc.handleRecord(context.Background(), report))

			assert.Empty(t, errb.String(), "under -o json nothing is written to stderr")
			events := jsonLines(t, out.String())
			if tt.wantEvent == "" {
				assert.Empty(t, events, "an operational no-op must not reach the json output")
				return
			}
			require.Len(t, events, 1)
			assert.Equal(t, tt.wantEvent, events[0]["event"])
			assert.Equal(t, "pkg-a", events[0]["package"])
			assert.Equal(t, "SD-r-1", events[0]["issue_id"])
		})
	}
}

func TestService_JSON_FailureIsSuppressed(t *testing.T) {
	out, errb := captureStreams(t)
	svc := newFeedService(nil, &fakeXrayClient{pushErr: errors.New("boom")}, jsonReporter())

	report := newTestReport("r-1", "pkg-a", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	require.NoError(t, svc.handleRecord(context.Background(), report))

	assert.Empty(t, out.String(), "a failure is a log, not output")
	assert.Empty(t, errb.String(), "under -o json a failure log is suppressed")
}

// TestService_Human_NoOpShownDim confirms the no-op now shows in human mode
// without --verbose (it moved from Faint to a dim line at normal verbosity).
func TestService_Human_NoOpShownDim(t *testing.T) {
	out, errb := captureStreams(t)
	svc := newFeedService(nil, &fakeXrayClient{pushStat: http.StatusBadRequest}, newReporter(tui.NewPrinter(tui.ModeTable)))

	report := newTestReport("r-1", "pkg-a", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	require.NoError(t, svc.handleRecord(context.Background(), report))

	assert.Empty(t, out.String(), "a no-op is a log, never stdout")
	assert.Contains(t, errb.String(), "Already pushed", "the no-op is shown (dimmed) at normal verbosity")
}

func TestPrintClient_JSON_EmitsDryRunPush(t *testing.T) {
	out, errb := captureStreams(t)
	c := newPrintClient(jsonReporter())

	report := newTestReport("r-1", "make-array", packagev1.Ecosystem_ECOSYSTEM_NPM, "0.1.2")
	id, status, err := c.pushMaliciousPackage(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, "SD-r-1", id)
	assert.Equal(t, 0, status)

	assert.Empty(t, errb.String())
	events := jsonLines(t, out.String())
	require.Len(t, events, 1)
	assert.Equal(t, eventDryRunPush, events[0]["event"])
	assert.Equal(t, "make-array", events[0]["package"])
	assert.Equal(t, "npm", events[0]["ecosystem"])
	assert.Equal(t, []any{"0.1.2"}, events[0]["versions"])
}

func TestClient_JSON_SkipSuppressed(t *testing.T) {
	out, errb := captureStreams(t)
	c := newJFrogClient(jfrogConfig{url: "https://unused.example", accessToken: "TOK"}, jsonReporter())

	// "SD-" + 30 chars is one over the JFrog id limit, so buildEvent skips it
	// before any HTTP call. The skip is a log, suppressed under -o json.
	report := newTestReport(strings.Repeat("A", 30), "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	_, status, err := c.pushMaliciousPackage(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, 0, status)

	assert.Empty(t, out.String(), "a skip is a log, not output")
	assert.Empty(t, errb.String(), "under -o json the skip log is suppressed")
}
