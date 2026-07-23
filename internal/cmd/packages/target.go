package packages

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	drypb "github.com/safedep/dry/api/pb"
)

// targetFlags holds the explicit identity form, shared by run/get/show/list.
type targetFlags struct {
	Ecosystem string
	Name      string
	Version   string
}

func (t targetFlags) any() bool {
	return t.Ecosystem != "" || t.Name != "" || t.Version != ""
}

// resolveTarget turns a positional reference and/or explicit flags into a
// PackageVersion. The explicit ecosystem/name/version triple is canonical;
// a PURL or GitHub URL positional is convenience sugar that resolves into
// the same value. Ecosystem is always required by the backend to pick a
// scan workflow, so a resolved UNSPECIFIED is rejected here rather than
// forwarded.
func resolveTarget(ref string, flags targetFlags) (*packagev1.PackageVersion, error) {
	switch {
	case flags.any():
		return resolveExplicit(flags)
	case ref != "":
		return resolveRef(ref)
	default:
		return nil, fmt.Errorf("no package specified: pass a purl (e.g. pkg:npm/lodash@4.17.21) or --ecosystem/--name/--version")
	}
}

func resolveExplicit(flags targetFlags) (*packagev1.PackageVersion, error) {
	if flags.Name == "" || flags.Ecosystem == "" || flags.Version == "" {
		return nil, fmt.Errorf("explicit target needs --ecosystem, --name and --version together")
	}
	eco, err := parseEcosystem(flags.Ecosystem)
	if err != nil {
		return nil, err
	}
	pkg := &packagev1.Package{}
	pkg.SetEcosystem(eco)
	pkg.SetName(flags.Name)
	pv := &packagev1.PackageVersion{}
	pv.SetPackage(pkg)
	pv.SetVersion(flags.Version)
	return pv, nil
}

func resolveRef(ref string) (*packagev1.PackageVersion, error) {
	var (
		helper interface {
			PackageVersion() *packagev1.PackageVersion
		}
		err error
	)
	switch {
	case strings.HasPrefix(ref, "pkg:"):
		helper, err = drypb.NewPurlPackageVersion(ref)
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		helper, err = drypb.NewPurlPackageVersionFromGithubUrl(ref)
	default:
		return nil, fmt.Errorf("unrecognised package reference %q: use a purl (pkg:...), a GitHub URL, or --ecosystem/--name/--version", ref)
	}
	if err != nil {
		return nil, err
	}

	pv := helper.PackageVersion()
	// dry maps unknown purl types to UNSPECIFIED silently. The backend needs
	// a concrete ecosystem to route the scan, so fail here with guidance.
	if pv.GetPackage().GetEcosystem() == packagev1.Ecosystem_ECOSYSTEM_UNSPECIFIED {
		return nil, fmt.Errorf("unsupported package reference %q: unknown ecosystem: pass --ecosystem explicitly, or upgrade the CLI", ref)
	}
	if pv.GetVersion() == "" {
		return nil, fmt.Errorf("missing version in %q: specify a version (e.g. @1.2.3 or a GitHub ref)", ref)
	}
	return pv, nil
}

func parseEcosystem(token string) (packagev1.Ecosystem, error) {
	key := "ECOSYSTEM_" + strings.ToUpper(strings.TrimSpace(token))
	v, ok := packagev1.Ecosystem_value[key]
	if !ok || packagev1.Ecosystem(v) == packagev1.Ecosystem_ECOSYSTEM_UNSPECIFIED {
		return 0, fmt.Errorf("unknown ecosystem %q", token)
	}
	return packagev1.Ecosystem(v), nil
}

// targetTriple extracts display tokens from a resolved PackageVersion.
func targetTriple(pv *packagev1.PackageVersion) (ecosystem, name, version string) {
	pkg := pv.GetPackage()
	return ecosystemToken(pkg.GetEcosystem()), pkg.GetName(), pv.GetVersion()
}

// idempotencyKey derives a stable key from the target so repeat runs dedupe
// server-side. An empty key (used by --rescan) tells the server to skip
// dedup and create a fresh scan.
func idempotencyKey(pv *packagev1.PackageVersion) string {
	eco, name, version := targetTriple(pv)
	sum := sha256.Sum256([]byte(eco + "\x00" + name + "\x00" + version))
	return hex.EncodeToString(sum[:])
}
