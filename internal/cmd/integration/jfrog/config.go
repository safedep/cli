// internal/cmd/integration/jfrog/config.go
package jfrog

import "time"

// Config holds all runtime configuration for the JFrog integration feed.
type Config struct {
	Source SourceConfig
	JFrog  JFrogConfig
}

// SourceConfig controls how SafeDep malicious package records are polled.
type SourceConfig struct {
	PollInterval time.Duration
	CursorFile   string
}

// JFrogConfig holds the JFrog XRay connection parameters.
type JFrogConfig struct {
	URL         string
	AccessToken string
}
