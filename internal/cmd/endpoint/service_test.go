package endpoint

import (
	"testing"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrettyEnum(t *testing.T) {
	cases := []struct {
		name, in, prefix, want string
	}{
		{"strips prefix and lowercases", "ENDPOINT_OS_LINUX", "ENDPOINT_OS_", "linux"},
		{"unspecified maps to unknown", "ENDPOINT_OS_UNSPECIFIED", "ENDPOINT_OS_", "unknown"},
		{"empty value maps to unknown", "", "ENDPOINT_OS_", "unknown"},
		{"different prefix family", "ECOSYSTEM_NPM", "ECOSYSTEM_", "npm"},
		{"non-matching prefix returned lowercased", "WEIRD_VALUE", "ENDPOINT_OS_", "weird_value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, prettyEnum(tc.in, tc.prefix))
		})
	}
}

func TestMapPmgActions(t *testing.T) {
	t.Run("all known actions translate", func(t *testing.T) {
		got, err := mapPmgActions([]GuardAction{"blocked", "confirmed", "trusted", "cooldown-blocked"})
		require.NoError(t, err)
		assert.Equal(t, []messagescontroltowerv1.PmgPackageAction{
			messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED,
			messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_CONFIRMED,
			messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_TRUSTED,
			messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED,
		}, got)
	})

	t.Run("empty input is empty output", func(t *testing.T) {
		got, err := mapPmgActions(nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("unknown action returns error", func(t *testing.T) {
		_, err := mapPmgActions([]GuardAction{"allowed"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown action")
	})
}

func TestPmgActionToCLI(t *testing.T) {
	cases := []struct {
		in   messagescontroltowerv1.PmgPackageAction
		want GuardAction
	}{
		{messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_BLOCKED, "blocked"},
		{messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_CONFIRMED, "confirmed"},
		{messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_TRUSTED, "trusted"},
		{messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED, "cooldown-blocked"},
	}
	for _, tc := range cases {
		t.Run(string(tc.want)+"-roundtrips", func(t *testing.T) {
			assert.Equal(t, tc.want, pmgActionToCLI(tc.in))
		})
	}

	t.Run("unspecified falls through to enum String", func(t *testing.T) {
		got := pmgActionToCLI(messagescontroltowerv1.PmgPackageAction_PMG_PACKAGE_ACTION_UNSPECIFIED)
		assert.NotEmpty(t, string(got))
	})
}

func TestWindowFromDuration(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	t.Run("positive duration produces trailing window", func(t *testing.T) {
		w := WindowFromDuration(now, 24*time.Hour)
		assert.Equal(t, now.Add(-24*time.Hour), w.Start)
		assert.Equal(t, now, w.End)
	})
	t.Run("zero duration produces zero window", func(t *testing.T) {
		assert.Equal(t, TimeWindow{}, WindowFromDuration(now, 0))
	})
	t.Run("negative duration produces zero window", func(t *testing.T) {
		assert.Equal(t, TimeWindow{}, WindowFromDuration(now, -1*time.Hour))
	})
}
