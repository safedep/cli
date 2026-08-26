package jfrog

import (
	"context"
	"strings"
	"testing"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintClient_NeverSendsAndNeedsNoCreds(t *testing.T) {
	// Empty config, no server: if the print client reached JFrog it would
	// error. It must not, because it only builds the event and prints.
	c := newPrintClient(newReporter(nil))

	require.NoError(t, c.validate(context.Background()))

	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "make-array", packagev1.Ecosystem_ECOSYSTEM_NPM, "0.1.2")
	id, status, err := c.pushMaliciousPackage(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, "SD-01KR0EKN6PMW0ZRFRN992H1PKX", id, "preview reports the id it would push")
	assert.Equal(t, 0, status, "status 0 keeps the service quiet; the print client already logged the preview")
}

func TestPrintClient_SkippedReportReturnsZeroNoError(t *testing.T) {
	c := newPrintClient(newReporter(nil))

	// An id of "SD-" + a 30-char report id is one over the JFrog limit, so
	// buildEvent skips it. The print client mirrors the real client's skip.
	overLength := newTestReport(strings.Repeat("A", 30), "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	id, status, err := c.pushMaliciousPackage(context.Background(), overLength)
	require.NoError(t, err)
	assert.Empty(t, id)
	assert.Equal(t, 0, status)
}

func TestPrintClient_DeletePreviewsAndNeverSends(t *testing.T) {
	c := newPrintClient(newReporter(nil))

	report := newTestReport("01KR0EKN6PMW0ZRFRN992H1PKX", "make-array", packagev1.Ecosystem_ECOSYSTEM_NPM, "0.1.2")
	id, status, err := c.deleteMaliciousPackage(context.Background(), report)
	require.NoError(t, err)
	assert.Equal(t, "SD-01KR0EKN6PMW0ZRFRN992H1PKX", id, "preview reports the id it would delete")
	assert.Equal(t, 0, status, "status 0 keeps the service quiet; the print client already logged the preview")

	// Over-length id was never pushed, so the dry-run delete stays quiet too.
	overLength := newTestReport(strings.Repeat("A", 30), "foo", packagev1.Ecosystem_ECOSYSTEM_NPM, "1.0.0")
	id, status, err = c.deleteMaliciousPackage(context.Background(), overLength)
	require.NoError(t, err)
	assert.Empty(t, id)
	assert.Equal(t, 0, status)
}
