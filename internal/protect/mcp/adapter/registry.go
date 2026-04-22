package adapter

// All returns all registered MCP adapters. Add new IDE adapters here.
func All() []MCPAdapter {
	return []MCPAdapter{
		&claudeCodeAdapter{},
		&cursorAdapter{},
		&windsurfAdapter{},
	}
}
