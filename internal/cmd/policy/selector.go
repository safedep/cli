package policy

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/duration"
)

// These patterns mirror the checks of PackageSelector in the contract. The
// server checks them too. The CLI checks them first, so a typo fails with a
// clear message and no publish.
var (
	purlTypePattern      = regexp.MustCompile(`^pkg:[A-Za-z0-9.+-]+/[^/].*$`)
	prefixPattern        = regexp.MustCompile(`^[^*?#]*(/|[^/*?#][.])[*]$`)
	prefixVersionPattern = regexp.MustCompile(`[^/]@`)
	encodedStarPattern   = regexp.MustCompile(`(?i)%2a`)
	purlVersionPattern   = regexp.MustCompile(`^[^?#]*@[^/?#]+([?#].*)?$`)
)

const (
	minSelectorLength = 6
	maxSelectorLength = 512
)

// buildSelector checks a PURL or a prefix and an optional vers range, and
// builds the package selector. A trust entry selects one package: only a
// block entry can use a prefix.
func buildSelector(purl, vers string, effect corev1.Effect) (*corev1.PackageSelector, error) {
	purl = strings.TrimSpace(purl)
	if err := validatePURL(purl, effect); err != nil {
		return nil, err
	}
	selector := &corev1.PackageSelector{}
	selector.SetPurl(purl)
	if vers = strings.TrimSpace(vers); vers != "" {
		if purlVersionPattern.MatchString(purl) {
			return nil, fmt.Errorf("selector %s: set a version in the PURL or a --vers range, not both", purl)
		}
		selector.SetVers(normalizeVers(purl, vers))
		if len(selector.GetVers()) > maxSelectorLength {
			return nil, fmt.Errorf("vers range of %s: the limit is %d characters", purl, maxSelectorLength)
		}
	}
	return selector, nil
}

func validatePURL(purl string, effect corev1.Effect) error {
	if !strings.HasPrefix(purl, "pkg:") || len(purl) < minSelectorLength || len(purl) > maxSelectorLength {
		return fmt.Errorf("selector %q: write a PURL of %d to %d characters, as in pkg:npm/name",
			purl, minSelectorLength, maxSelectorLength)
	}
	if !purlTypePattern.MatchString(purl) {
		return fmt.Errorf("selector %q: a PURL has a type and a name, as in pkg:npm/name", purl)
	}
	if encodedStarPattern.MatchString(purl) {
		return fmt.Errorf("selector %q: do not encode '*' as %%2A", purl)
	}
	if !strings.Contains(purl, "*") {
		return nil
	}
	if effect == corev1.Effect_EFFECT_TRUST {
		return fmt.Errorf("selector %q: a trust entry selects one package, so it cannot use a prefix. "+
			"Only a block entry can use a prefix that ends with /* or .*", purl)
	}
	if strings.Count(purl, "*") > 1 || !prefixPattern.MatchString(purl) {
		return fmt.Errorf("selector %q: a prefix has one '*', at the end, after '/' or '.', "+
			"as in pkg:npm/@acme/* or pkg:maven/org.apache.*. It has no qualifiers or subpath", purl)
	}
	if prefixVersionPattern.MatchString(purl) {
		return fmt.Errorf("selector %q: a prefix has no version. Use --vers for a range", purl)
	}
	return nil
}

// normalizeVers turns a bare range such as ">=1.2.0" into the vers form of
// the contract, "vers:npm/>=1.2.0", with the type of the PURL.
func normalizeVers(purl, vers string) string {
	if strings.HasPrefix(vers, "vers:") {
		return vers
	}
	purlType, _, _ := strings.Cut(strings.TrimPrefix(purl, "pkg:"), "/")
	return "vers:" + strings.ToLower(purlType) + "/" + vers
}

// selectorText writes a selector as one value, as in
// "pkg:npm/evil vers:npm/>=1.2.0".
func selectorText(s *corev1.PackageSelector) string {
	if s.GetVers() == "" {
		return s.GetPurl()
	}
	return s.GetPurl() + " " + s.GetVers()
}

// parseExpiry reads a duration from now, as "30d", or a time, as
// "2027-01-26". The expiry must be in the future.
func parseExpiry(text string, now time.Time) (time.Time, error) {
	if d, err := duration.Parse(text); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("--expires %q: give a duration after now, as in 30d", text)
		}
		return now.Add(d).UTC(), nil
	}
	t, err := cloudpolicy.ParseTime(text)
	if err != nil {
		return time.Time{}, fmt.Errorf("--expires %q: give a duration, as in 30d, or a date, as in 2027-01-26", text)
	}
	if !t.After(now) {
		return time.Time{}, fmt.Errorf("--expires %q: the time is in the past", text)
	}
	return t.UTC(), nil
}
