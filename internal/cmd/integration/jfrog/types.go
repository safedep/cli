package jfrog

import "time"

// cmdConfig is the resolved runtime state passed between the package's
// components. Constructed once by resolveConfig from CLI flags and
// environment variables. It has no schema and is never read from or
// written to disk.
type cmdConfig struct {
	source sourceConfig
	jfrog  jfrogConfig
}

// sourceConfig groups the SafeDep-side polling parameters.
type sourceConfig struct {
	// PollInterval is the sleep duration between successful poll cycles.
	pollInterval time.Duration
}

// jfrogConfig groups the XRay HTTP endpoint and bearer credential. The URL
// is always normalised to https by resolveConfig so the access token is
// never transmitted in the clear.
type jfrogConfig struct {
	url         string
	accessToken string
}
