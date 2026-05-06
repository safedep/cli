package jfrog

import "time"

// Config is the resolved runtime state passed between the package's
// components. Constructed once by resolveConfig from CLI flags and
// environment variables. It has no schema and is never read from or
// written to disk.
type Config struct {
	Source SourceConfig
	JFrog  JFrogConfig
}

// SourceConfig groups the SafeDep-side polling parameters.
type SourceConfig struct {
	// PollInterval is the sleep duration between successful poll cycles.
	PollInterval time.Duration

	// CursorFile is the absolute path to the JSON cursor file. Created on
	// first run and rewritten atomically after each page of results.
	CursorFile string
}

// JFrogConfig groups the XRay HTTP endpoint and bearer credential. The URL
// is always normalised to https by resolveConfig so the access token is
// never transmitted in the clear.
type JFrogConfig struct {
	URL         string
	AccessToken string
}
