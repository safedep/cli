package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAllowedVerb(t *testing.T) {
	for verb := range AllowedVerbs {
		assert.True(t, IsAllowedVerb(verb), "expected %q to be allowed", verb)
	}
	assert.False(t, IsAllowedVerb("frobnicate"))
	assert.False(t, IsAllowedVerb(""))
}

func TestAllowedVerbs_devguideAlignment(t *testing.T) {
	required := []string{
		"get", "list", "show", "run", "exec",
		"login", "logout", "status",
		"install", "uninstall",
		"enable", "disable",
		"create", "delete", "update",
		"set", "init", "sync", "edit",
	}
	for _, v := range required {
		assert.True(t, IsAllowedVerb(v), "DEVGUIDE-required verb missing: %q", v)
	}
}
