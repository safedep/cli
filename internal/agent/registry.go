package agent

import (
	"os"

	"github.com/safedep/dry/log"
)

// NewRegistry returns all known agent adapters initialised with the
// current user's home directory.
func NewRegistry() []Agent {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Warnf("agent: registry: could not resolve home directory: %v", err)
		homeDir = ""
	}

	return []Agent{
		newClaudeCode(homeDir),
		newCursor(homeDir),
		newVSCode(homeDir),
		newGeminiCLI(homeDir),
		newOpenCode(homeDir),
		newAntigravity(homeDir),
	}
}
