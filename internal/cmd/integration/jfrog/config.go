package jfrog

import "time"

// Config is the resolved runtime configuration for the JFrog integration
// feed. Constructed by resolveConfig from CLI flags + env vars; never
// loaded from disk.
type Config struct {
	Source SourceConfig
	JFrog  JFrogConfig
}

// SourceConfig controls how SafeDep malicious package records are polled.
type SourceConfig struct {
	// PollInterval is the sleep duration between successful poll cycles.
	PollInterval time.Duration

	// CursorFile is an absolute path to the JSON cursor file. It is created
	// on first run and rewritten atomically every cycle.
	CursorFile string
}

// JFrogConfig holds the XRay HTTP endpoint and bearer credential. The URL is
// always normalised to https in resolveConfig so the access token is never
// transmitted in the clear.
type JFrogConfig struct {
	URL         string
	AccessToken string
}
