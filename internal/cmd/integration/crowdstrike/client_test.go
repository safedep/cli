package crowdstrike

import (
	"testing"
	"time"

	ctmsgv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToJSONEvent_MaliciousBlock(t *testing.T) {
	base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	ev := newTestEvent(base, blocked("evt-1", "left-pad", "1.0.0", 0))

	rec, ok := toJSONEvent(ev)
	require.True(t, ok)
	assert.Equal(t, eventPackageBlocked, rec.Event)
	assert.Equal(t, actionBlocked, rec.Action)
	assert.Equal(t, "evt-1", rec.EventID)
	assert.Equal(t, "left-pad", rec.Package)
	assert.Equal(t, "npm", rec.Ecosystem)
	assert.Equal(t, "1.0.0", rec.Version)
	assert.True(t, rec.IsMalware)
	assert.Equal(t, "2026-09-15T10:00:00Z", rec.Timestamp)
	assert.Nil(t, rec.Cooldown, "a malicious block carries no cooldown detail")

	assert.Equal(t, "Malicious block: left-pad@1.0.0 (npm) malware on host-1 [evt-1]", humanLine(rec))
}

func TestToJSONEvent_CooldownBlock(t *testing.T) {
	base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	ev := newTestEvent(base, eventSpec{
		id:           "evt-2",
		name:         "shiny-pkg",
		version:      "0.0.1",
		action:       ctmsgv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED,
		cooldownDays: 3,
	})

	rec, ok := toJSONEvent(ev)
	require.True(t, ok)
	assert.Equal(t, actionCooldownBlocked, rec.Action)
	require.NotNil(t, rec.Cooldown)
	assert.Equal(t, uint32(3), rec.Cooldown.DaysRemaining)

	assert.Equal(t, "Cooldown block: shiny-pkg@0.0.1 (npm) cooldown, 3d remaining on host-1 [evt-2]", humanLine(rec))
}

func TestToJSONEvent_NoPackageDecision_Skipped(t *testing.T) {
	ev := &packageGuardEvent{}
	ev.SetEventId("evt-3")

	_, ok := toJSONEvent(ev)
	assert.False(t, ok, "an event with no package decision is skipped")
}
