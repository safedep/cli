package agent

// geminiCLI is a stub. Config paths for Gemini CLI are unverified.
// Detected always returns false until the adapter is fully implemented.
type geminiCLI struct {
	homeDir string
}

func newGeminiCLI(homeDir string) *geminiCLI {
	return &geminiCLI{homeDir: homeDir}
}

func (g *geminiCLI) Name() string                                   { return "gemini-cli" }
func (g *geminiCLI) Detected() bool                                 { return false }
func (g *geminiCLI) AsGlobalInjector() (GlobalInjector, bool)       { return nil, false }
func (g *geminiCLI) AsWorkspaceInjector() (WorkspaceInjector, bool) { return nil, false }

var _ Agent = (*geminiCLI)(nil)
