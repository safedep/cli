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

// sourceConfig groups the SafeDep-side feed parameters.
type sourceConfig struct {
	// pollInterval is the sleep duration between feed drains.
	pollInterval time.Duration

	// backfillWindow seeds the first-run since filter: on a fresh install
	// with no cursor, since = now - backfillWindow. The default of 0 means
	// a fresh start from now (no history pulled).
	backfillWindow time.Duration
}

// jfrogConfig groups the XRay HTTP endpoint and bearer credential. The URL
// is always normalised to https by resolveConfig so the access token is
// never transmitted in the clear.
type jfrogConfig struct {
	url         string
	accessToken string
}
