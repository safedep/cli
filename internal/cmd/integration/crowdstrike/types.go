package crowdstrike

import "time"

// cmdConfig is the resolved runtime state passed between the package's
// components. Constructed once by resolveConfig from CLI flags. It has no
// schema and is never read from or written to disk.
type cmdConfig struct {
	source sourceConfig
}

// sourceConfig groups the SafeDep-side sync parameters.
type sourceConfig struct {
	// pollInterval is the sleep duration between sync cycles.
	pollInterval time.Duration

	// backfillWindow seeds the first-run cursor: since = now - backfillWindow.
	// 0 (default) starts fresh from now.
	backfillWindow time.Duration
}
