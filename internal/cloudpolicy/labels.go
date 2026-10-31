package cloudpolicy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
)

const (
	// CustomPrefix is the one label prefix that an admin writes. Each other
	// prefix has its own write path, and LabelService rejects it.
	CustomPrefix = "custom/"

	maxCustomValueLength = 63
	maxLabelsPerRequest  = 32
	maxSelectorTerms     = 32
)

// labelKeyPattern is the label key rule of the contract.
var labelKeyPattern = regexp.MustCompile(`^[a-z0-9.-]+/[A-Za-z0-9_.$#-]{1,75}$`)

// ParseCustomLabels reads "custom/key=value" pairs. It rejects any other
// prefix before the RPC, with the reason.
func ParseCustomLabels(values []string) ([]*messagescontroltowerv1.Label, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("give at least one label, as in --label custom/team=release")
	}
	if len(values) > maxLabelsPerRequest {
		return nil, fmt.Errorf("give at most %d labels in one command", maxLabelsPerRequest)
	}
	seen := make(map[string]struct{}, len(values))
	labels := make([]*messagescontroltowerv1.Label, 0, len(values))
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("label %q has no value: write it as custom/key=value", raw)
		}
		key = strings.TrimSpace(key)
		if err := ValidateCustomKey(key); err != nil {
			return nil, err
		}
		if len(value) > maxCustomValueLength {
			return nil, fmt.Errorf("the value of label %s has %d characters: the limit is %d",
				key, len(value), maxCustomValueLength)
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("label %s is given twice", key)
		}
		seen[key] = struct{}{}
		label := &messagescontroltowerv1.Label{}
		label.SetKey(key)
		label.SetValue(value)
		labels = append(labels, label)
	}
	return labels, nil
}

// ValidateCustomKeys checks label keys for a delete.
func ValidateCustomKeys(keys []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("give at least one label key, as in --key custom/team")
	}
	if len(keys) > maxLabelsPerRequest {
		return fmt.Errorf("give at most %d label keys in one command", maxLabelsPerRequest)
	}
	for _, key := range keys {
		if err := ValidateCustomKey(key); err != nil {
			return err
		}
	}
	return nil
}

// ValidateCustomKey checks that an admin can write the label key.
func ValidateCustomKey(key string) error {
	if !strings.HasPrefix(key, CustomPrefix) {
		prefix, _, _ := strings.Cut(key, "/")
		return fmt.Errorf("label key %q: an admin writes only %q labels. The source of %q labels sets them",
			key, CustomPrefix, prefix+"/")
	}
	if !labelKeyPattern.MatchString(key) {
		return fmt.Errorf("label key %q is not valid: use custom/ and a name of 1 to 75 letters, digits or _.$#-", key)
	}
	return nil
}

// FormatLabels writes labels as sorted "key=value" items.
func FormatLabels(labels []*messagescontroltowerv1.Label) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.GetKey()+"="+l.GetValue())
	}
	sort.Strings(out)
	return out
}

// ParseSelector reads a label selector in the Kubernetes text form. The terms
// are separated by commas, and they AND together:
//
//	key=value, key==value   the key has the value
//	key!=value              the key does not have the value
//	key in (a,b)            the key has one of the values
//	key notin (a,b)         the key has none of the values
//	key                     the key exists
//	!key                    the key does not exist
func ParseSelector(text string) (*messagescontroltowerv1.LabelSelector, error) {
	terms, err := splitSelectorTerms(text)
	if err != nil {
		return nil, err
	}
	if len(terms) == 0 {
		return nil, fmt.Errorf("the selector is empty: write it as key=value, as in custom/team=release")
	}
	if len(terms) > maxSelectorTerms {
		return nil, fmt.Errorf("the selector has %d terms: the limit is %d", len(terms), maxSelectorTerms)
	}

	selector := &messagescontroltowerv1.LabelSelector{}
	matchLabels := map[string]string{}
	var exprs []*messagescontroltowerv1.LabelSelectorRequirement
	for _, term := range terms {
		expr, key, value, err := parseSelectorTerm(term)
		if err != nil {
			return nil, err
		}
		if expr != nil {
			exprs = append(exprs, expr)
			continue
		}
		if existing, dup := matchLabels[key]; dup && existing != value {
			return nil, fmt.Errorf("selector term %q conflicts with %s=%s", term, key, existing)
		}
		matchLabels[key] = value
	}
	if len(matchLabels) > 0 {
		selector.SetMatchLabels(matchLabels)
	}
	if len(exprs) > 0 {
		selector.SetMatchExpressions(exprs)
	}
	return selector, nil
}

func splitSelectorTerms(text string) ([]string, error) {
	var terms []string
	depth := 0
	start := 0
	for i, r := range text {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("selector %q has a ')' with no '('", text)
			}
		case ',':
			if depth == 0 {
				terms = appendTerm(terms, text[start:i])
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("selector %q has a '(' with no ')'", text)
	}
	return appendTerm(terms, text[start:]), nil
}

func appendTerm(terms []string, term string) []string {
	if t := strings.TrimSpace(term); t != "" {
		return append(terms, t)
	}
	return terms
}

var setTermPattern = regexp.MustCompile(`^(\S+)\s+(in|notin)\s*\((.*)\)$`)

func parseSelectorTerm(term string) (*messagescontroltowerv1.LabelSelectorRequirement, string, string, error) {
	if m := setTermPattern.FindStringSubmatch(term); m != nil {
		op := messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_IN
		if m[2] == "notin" {
			op = messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_NOT_IN
		}
		var values []string
		for _, v := range strings.Split(m[3], ",") {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			return nil, "", "", fmt.Errorf("selector term %q needs at least one value", term)
		}
		expr, err := newRequirement(m[1], op, values)
		return expr, "", "", err
	}

	if key, value, ok := strings.Cut(term, "!="); ok {
		expr, err := newRequirement(strings.TrimSpace(key),
			messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_NOT_IN,
			[]string{strings.TrimSpace(value)})
		return expr, "", "", err
	}
	if key, value, ok := strings.Cut(term, "="); ok {
		key = strings.TrimSpace(key)
		value = strings.TrimPrefix(value, "=")
		if err := validateSelectorKey(key); err != nil {
			return nil, "", "", err
		}
		return nil, key, strings.TrimSpace(value), nil
	}
	if key, ok := strings.CutPrefix(term, "!"); ok {
		expr, err := newRequirement(strings.TrimSpace(key),
			messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_DOES_NOT_EXIST, nil)
		return expr, "", "", err
	}
	expr, err := newRequirement(term, messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_EXISTS, nil)
	return expr, "", "", err
}

func newRequirement(
	key string,
	op messagescontroltowerv1.LabelSelectorOperator,
	values []string,
) (*messagescontroltowerv1.LabelSelectorRequirement, error) {
	if err := validateSelectorKey(key); err != nil {
		return nil, err
	}
	expr := &messagescontroltowerv1.LabelSelectorRequirement{}
	expr.SetKey(key)
	expr.SetOperator(op)
	if len(values) > 0 {
		expr.SetValues(values)
	}
	return expr, nil
}

func validateSelectorKey(key string) error {
	if !labelKeyPattern.MatchString(key) {
		return fmt.Errorf("selector label key %q is not valid: write it as prefix/name, as in custom/team or scm/forge", key)
	}
	return nil
}

// FormatSelector writes a label selector in the text form that ParseSelector
// reads. An empty selector matches every subject.
func FormatSelector(selector *messagescontroltowerv1.LabelSelector) string {
	if selector == nil {
		return "(every subject)"
	}
	var terms []string
	for key, value := range selector.GetMatchLabels() {
		terms = append(terms, key+"="+value)
	}
	sort.Strings(terms)
	for _, expr := range selector.GetMatchExpressions() {
		values := strings.Join(expr.GetValues(), ",")
		switch expr.GetOperator() {
		case messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_IN:
			terms = append(terms, fmt.Sprintf("%s in (%s)", expr.GetKey(), values))
		case messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_NOT_IN:
			terms = append(terms, fmt.Sprintf("%s notin (%s)", expr.GetKey(), values))
		case messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_EXISTS:
			terms = append(terms, expr.GetKey())
		case messagescontroltowerv1.LabelSelectorOperator_LABEL_SELECTOR_OPERATOR_DOES_NOT_EXIST:
			terms = append(terms, "!"+expr.GetKey())
		default:
			terms = append(terms, fmt.Sprintf("%s %s (%s)", expr.GetKey(), expr.GetOperator(), values))
		}
	}
	if len(terms) == 0 {
		return "(every subject)"
	}
	return strings.Join(terms, ", ")
}
