package policy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	corev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/core/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This file checks and builds the input of `policy rule create`, before any
// RPC, so one bad line in a file stops the whole create.

const (
	// maxTrustExpiry is the longest life of a trust entry in the MVP. The
	// server refuses a later expiry. A tenant preference changes it later.
	maxTrustExpiry = 90 * 24 * time.Hour
)

// widePattern matches a prefix that selects every package of an
// ecosystem, as "pkg:npm/*".
var widePattern = regexp.MustCompile(`^pkg:[A-Za-z0-9.+-]+/[*]$`)

// checkTrustExpiry refuses a trust expiry that the server refuses too.
func checkTrustExpiry(expires, now time.Time) error {
	if expires.Sub(now) <= maxTrustExpiry {
		return nil
	}
	return invalidInputError(
		fmt.Errorf("a trust entry expires at most 90 days from now, not on %s", expires.Format(time.DateOnly)),
		"Use --expires 90d or less. Extend the entry later with: safedep policy rule update RULE --expires 90d")
}

// isWideSelector reports a block that stops every install of an ecosystem.
func isWideSelector(purl string) bool {
	return widePattern.MatchString(purl)
}

func validateRuleCreate(in ruleCreateInput) error {
	switch {
	case in.Block == in.Trust:
		return invalidInputError(errors.New("give one of --block or --trust"),
			"Block: safedep policy rule create --block pkg:npm/name --reason TEXT")
	case in.File != "" && in.Selector != "":
		return invalidInputError(errors.New("give a PURL or --file, not both"), "Use --file for a list of selectors")
	case in.File == "" && in.Selector == "":
		return invalidInputError(errors.New("give the PURL of the package"),
			"As in: safedep policy rule create --block pkg:npm/evil --reason TEXT")
	case in.File != "" && in.Trust:
		return invalidInputError(errors.New("only block entries can come from a file"),
			"Create each trust entry with its own command")
	case in.File != "" && in.Vers != "":
		return invalidInputError(errors.New("--vers applies to one PURL: in a file, write the range after the PURL"),
			"Write each line as: PURL [RANGE]")
	case strings.TrimSpace(in.Reason) == "":
		return invalidInputError(errors.New("--reason is required"), "Say why the rule exists, as in --reason \"IR-2291 credential stealer\"")
	case in.Trust && len(in.Scopes) == 0 && in.waives():
		return waivedNeedsScopeError()
	case in.Trust && len(in.Scopes) == 0:
		return invalidInputError(errors.New("a trust entry needs --scope"),
			"Name each verdict that it waives: --scope "+strings.Join(createScopeTokens(), ", --scope ")+", or --scope all")
	case in.Trust && in.Expires == "":
		return invalidInputError(errors.New("a trust entry needs --expires"), "As in --expires 30d")
	case in.Block && len(in.Scopes) > 0:
		return invalidInputError(errors.New("a block entry has no scope"), "Remove --scope")
	case in.Block && in.waives():
		return invalidInputError(errors.New("a block entry waives no vulnerability"), "Remove --vulnerability")
	}
	_, _, err := ruleScopes(in)
	return err
}

// ruleScopes gives the scopes of the rule and the vulnerabilities that it
// waives. --scope vulnerability with --vulnerability gives the one scope
// NAMED_VULNERABILITY. Without --vulnerability it waives every vulnerability.
func ruleScopes(in ruleCreateInput) ([]corev1.TrustScope, []*corev1.WaivedVulnerability, error) {
	scopes, err := expandScopes(in.Scopes)
	if err != nil || !in.waives() {
		return scopes, nil, err
	}
	if err := checkWaivedScope(scopes); err != nil {
		return nil, nil, err
	}
	waived, err := parseWaivedVulnerabilities(in.Vulnerabilities)
	if err != nil {
		return nil, nil, err
	}
	return []corev1.TrustScope{corev1.TrustScope_TRUST_SCOPE_NAMED_VULNERABILITY}, waived, nil
}

// expandScopes reads the --scope values. "all" expands to every trust scope
// that this SDK knows, because the contract has no "all" value: a scope that
// a later release adds never widens a stored entry.
// Every value is checked before "all" expands, so a bad value after "all"
// still fails.
func expandScopes(values []string) ([]corev1.TrustScope, error) {
	var out []corev1.TrustScope
	all := false
	for _, v := range values {
		switch token := strings.TrimSpace(v); {
		case strings.EqualFold(token, scopeAll):
			all = true
		case strings.EqualFold(token, namedVulnerabilityToken()):
			return nil, invalidInputError(fmt.Errorf("--scope %s is not a value of create: it needs vulnerability ids", v),
				"Use --scope vulnerability with --vulnerability ID")
		default:
			scope, err := parseTrustScope(v, createScopeTokens())
			if err != nil {
				return nil, err
			}
			if !slices.Contains(out, scope) {
				out = append(out, scope)
			}
		}
	}
	if !all {
		return out, nil
	}
	out = out[:0]
	for _, token := range createScopeTokens() {
		scope, err := parseTrustScope(token, createScopeTokens())
		if err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, nil
}

// buildRuleCreates checks every selector and builds the rules, before any
// RPC, so one bad line in a file stops the whole create.
func buildRuleCreates(in ruleCreateInput, stdin io.Reader, now time.Time) ([]*policyv1.PackageRuleCreate, error) {
	if err := validateRuleCreate(in); err != nil {
		return nil, err
	}
	effect := corev1.Effect_EFFECT_BLOCK
	if in.Trust {
		effect = corev1.Effect_EFFECT_TRUST
	}
	scopes, waived, err := ruleScopes(in)
	if err != nil {
		return nil, err
	}
	var expires *timestamppb.Timestamp
	if in.Expires != "" {
		t, err := parseExpiry(in.Expires, now)
		if err != nil {
			return nil, invalidInputError(err, "Use a duration, as in 30d, or a date, as in 2027-01-26")
		}
		if in.Trust {
			if err := checkTrustExpiry(t, now); err != nil {
				return nil, err
			}
		}
		expires = timestamppb.New(t)
	}

	entries := []selectorEntry{{purl: in.Selector, vers: in.Vers}}
	if in.File != "" {
		entries, err = readSelectorFile(in.File, stdin)
		if err != nil {
			return nil, err
		}
	}

	creates := make([]*policyv1.PackageRuleCreate, 0, len(entries))
	for _, e := range entries {
		selector, err := buildSelector(e.purl, e.vers, effect)
		if err != nil {
			if e.line > 0 {
				err = fmt.Errorf("%s line %d: %w", in.File, e.line, err)
			}
			return nil, invalidInputError(err, "Fix the selector and run the command again")
		}
		rule := &corev1.PackageRule{}
		rule.SetSelector(selector)
		rule.SetEffect(effect)
		rule.SetScopes(scopes)
		rule.SetVulnerabilities(waived)
		rule.SetReason(in.Reason)
		if expires != nil {
			rule.SetExpiresAt(expires)
		}
		create := &policyv1.PackageRuleCreate{}
		create.SetRule(rule)
		create.SetTicket(in.Ticket)
		create.SetRequestedBy(in.RequestedBy)
		creates = append(creates, create)
	}
	return creates, nil
}

type selectorEntry struct {
	purl string
	vers string
	line int
}

// readSelectorFile reads one selector on each line: a PURL and an optional
// range after a space. A line that starts with # and an empty line are
// skipped.
func readSelectorFile(path string, stdin io.Reader) ([]selectorEntry, error) {
	r := stdin
	if path != "-" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, invalidInputError(fmt.Errorf("read %s: %w", path, err), "Check the path of --file")
		}
		r = bytes.NewReader(data)
	}
	var entries []selectorEntry
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 2 {
			return nil, invalidInputError(fmt.Errorf("%s line %d: write a PURL and an optional range, not %d values", path, n, len(fields)),
				"Write each line as: PURL [RANGE]")
		}
		entry := selectorEntry{purl: fields[0], line: n}
		if len(fields) == 2 {
			entry.vers = fields[1]
		}
		entries = append(entries, entry)
		if len(entries) > maxBulkRules {
			return nil, invalidInputError(fmt.Errorf("%s has more than %d selectors", path, maxBulkRules),
				fmt.Sprintf("Split the file into parts of %d lines or fewer", maxBulkRules))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(entries) == 0 {
		return nil, invalidInputError(fmt.Errorf("%s has no selectors", path), "Write one PURL on each line")
	}
	return entries, nil
}
