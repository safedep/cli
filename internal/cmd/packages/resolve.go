package packages

import (
	"context"
	"fmt"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
)

// latestScan returns the newest scan for a target. The service lists newest
// first, so the first record is the latest. Returns an actionable error when
// no scan exists yet for the package.
func latestScan(ctx context.Context, lister ScanLister, target *packagev1.PackageVersion) (*Scan, error) {
	res, err := lister.List(ctx, ListInput{Target: target, PageSize: 1})
	if err != nil {
		return nil, err
	}
	if len(res.Scans) == 0 {
		eco, name, version := targetTriple(target)
		return nil, fmt.Errorf("no scan found for %s / %s @ %s: submit one with `safedep package scan run`", eco, name, version)
	}
	return &res.Scans[0], nil
}
