package agent

// openCode is a stub. Config paths for OpenCode are unverified.
type openCode struct {
	homeDir string
}

func newOpenCode(homeDir string) *openCode {
	return &openCode{homeDir: homeDir}
}

func (o *openCode) Name() string                                   { return "opencode" }
func (o *openCode) Detected() bool                                 { return false }
func (o *openCode) AsGlobalInjector() (GlobalInjector, bool)       { return nil, false }
func (o *openCode) AsWorkspaceInjector() (WorkspaceInjector, bool) { return nil, false }

var _ Agent = (*openCode)(nil)
