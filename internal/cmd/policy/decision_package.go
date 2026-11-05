package policy

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	packagev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/package/v1"
	drypb "github.com/safedep/dry/api/pb"
)

// maxPackageNameLen is the longest package name that the server accepts.
const maxPackageNameLen = 255

const packageHelp = "Give a PURL with no version, as in --package pkg:npm/lodash or pkg:maven/org.example/lib"

// parseDecisionPackage reads the --package PURL. The server compares the
// stored package name exactly, so the command keeps the name as the user
// wrote it. A PURL parser changes some names: it lowercases a PyPI name and
// drops the vendor of a Composer package. Only the ecosystem comes from the
// parser. The name has the form that pmg and vet store:
//
//	npm       @scope/name
//	Maven     group:artifact
//	Go        the module path
//	Composer  vendor/name
//	others    the namespace and the name, joined with "/"
//
// The filter matches every version, so the command refuses a version. It
// also refuses qualifiers and a subpath, because the filter has no field for
// them.
func parseDecisionPackage(purl string) (*packagev1.Package, error) {
	fail := func(cause error) (*packagev1.Package, error) {
		return nil, invalidInputError(fmt.Errorf("--package %q: %w", purl, cause), packageHelp)
	}
	if !strings.HasPrefix(strings.ToLower(purl), "pkg:") {
		return fail(errors.New("not a PURL"))
	}
	rest := strings.TrimLeft(purl[len("pkg:"):], "/")
	switch {
	case strings.Contains(rest, "?"):
		return fail(errors.New("has qualifiers: the filter does not use them"))
	case strings.Contains(rest, "#"):
		return fail(errors.New("has a subpath: the filter does not use it"))
	}
	purlType, path, ok := strings.Cut(rest, "/")
	if !ok || path == "" {
		return fail(errors.New("has no name"))
	}
	// An "@" at the start of a segment begins an npm scope. Any other "@"
	// begins a version.
	if at := strings.LastIndexByte(path, '@'); at > 0 && path[at-1] != '/' {
		return fail(errors.New("has a version: the filter matches every version of the package"))
	}

	parsed, err := drypb.NewPurlPackageVersion(purl)
	if err != nil {
		return fail(err)
	}
	ecosystem := parsed.Ecosystem()
	if ecosystem == packagev1.Ecosystem_ECOSYSTEM_UNSPECIFIED {
		return fail(fmt.Errorf("unknown ecosystem %q", purlType))
	}

	var segments []string
	for _, raw := range strings.Split(path, "/") {
		if raw == "" {
			continue
		}
		segment, err := url.PathUnescape(raw)
		if err != nil {
			return fail(err)
		}
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return fail(errors.New("has no name"))
	}

	name := strings.Join(segments, "/")
	if ecosystem == packagev1.Ecosystem_ECOSYSTEM_MAVEN {
		if len(segments) != 2 {
			return fail(errors.New("a Maven PURL needs the group and the artifact, as in pkg:maven/org.example/lib"))
		}
		name = segments[0] + ":" + segments[1]
	}
	if err := checkLength("package", name, maxPackageNameLen); err != nil {
		return nil, err
	}

	pkg := &packagev1.Package{}
	pkg.SetEcosystem(ecosystem)
	pkg.SetName(name)
	return pkg, nil
}
