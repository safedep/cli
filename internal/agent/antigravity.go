package agent

// antigravity is a stub. Config paths for Antigravity are unverified.
type antigravity struct {
	homeDir string
}

func newAntigravity(homeDir string) *antigravity {
	return &antigravity{homeDir: homeDir}
}

func (a *antigravity) Name() string                                   { return "antigravity" }
func (a *antigravity) Detected() bool                                 { return false }
func (a *antigravity) AsGlobalInjector() (GlobalInjector, bool)       { return nil, false }
func (a *antigravity) AsWorkspaceInjector() (WorkspaceInjector, bool) { return nil, false }

var _ Agent = (*antigravity)(nil)
