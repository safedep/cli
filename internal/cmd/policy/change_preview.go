package policy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	recordv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/record/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/safedep/cli/internal/cloudpolicy"
	"github.com/safedep/cli/internal/tui"
)

// This file turns a PreviewPolicyChangeResponse into the view that
// `change show` and `change publish` print.

// changeItem is one row of "What changes": a field, a policy override or a
// package rule operation.
type changeItem struct {
	Where     string `json:"where"`
	What      string `json:"what"`
	Old       string `json:"old,omitempty"`
	New       string `json:"new,omitempty"`
	Direction string `json:"direction"`
	oldHuman  string
	newHuman  string
	// whatHuman and changeHuman replace What and "old -> new" in the table
	// when a plain value reads badly, as for an added override.
	whatHuman   string
	changeHuman string
	// sentence names the change for the publish warning. Empty means the
	// generic form.
	sentence string
}

// whatCell is the WHAT cell of the table.
func (it changeItem) whatCell() string {
	if it.whatHuman != "" {
		return it.whatHuman
	}
	return it.What
}

// changeCell is the OLD -> NEW cell of the table.
func (it changeItem) changeCell() string {
	if it.changeHuman != "" {
		return it.changeHuman
	}
	return it.oldHuman + " -> " + it.newHuman
}

type coverageView struct {
	Path        string `json:"path"`
	Value       string `json:"value,omitempty"`
	Enforce     uint32 `json:"enforce"`
	Partial     uint32 `json:"partial"`
	Detect      uint32 `json:"detect"`
	Unsupported uint32 `json:"unsupported"`
}

type warningView struct {
	Code    string `json:"code"`
	Subject string `json:"subject,omitempty"`
	Message string `json:"message"`
}

type previewView struct {
	HeadRevision     uint64         `json:"head_revision"`
	Stale            bool           `json:"stale"`
	Direction        string         `json:"direction"`
	Items            []changeItem   `json:"changes"`
	MatchedProjects  uint32         `json:"matched_project_count"`
	MatchedEndpoints uint32         `json:"matched_endpoint_count"`
	Coverage         []coverageView `json:"coverage"`
	Warnings         []warningView  `json:"warnings"`
}

func newPreviewView(res *policyv1.PreviewPolicyChangeResponse) *previewView {
	v := &previewView{
		HeadRevision:     res.GetHeadRevision(),
		Stale:            res.GetStale(),
		Direction:        directionToken(res.GetDirection()),
		MatchedProjects:  res.GetMatchedProjectCount(),
		MatchedEndpoints: res.GetMatchedEndpointCount(),
		Items:            []changeItem{},
		Coverage:         []coverageView{},
		Warnings:         []warningView{},
	}
	for _, c := range res.GetChanges() {
		where := "base"
		if c.GetLayer() == recordv1.Layer_LAYER_OVERRIDE {
			where = fmt.Sprintf("override %q", c.GetOverrideName())
		}
		oldValue, oldHuman := cloudpolicy.FormatJSONValue(c.GetPath(), c.GetOldValue())
		newValue, newHuman := cloudpolicy.FormatJSONValue(c.GetPath(), c.GetNewValue())
		v.Items = append(v.Items, changeItem{
			Where: where, What: c.GetPath(), Old: oldValue, New: newValue,
			Direction: directionToken(c.GetDirection()), oldHuman: oldHuman, newHuman: newHuman,
		})
	}
	for _, c := range res.GetOverrideChanges() {
		v.Items = append(v.Items, overrideChangeItem(c))
	}
	for _, op := range res.GetPackageRuleOperations() {
		item := ruleOperationItem(op.GetOperation())
		item.Direction = directionToken(op.GetDirection())
		if op.GetOperation().HasCreate() {
			item.New = fmt.Sprintf("matches %d packages, seen on %d endpoints in 30d",
				op.GetMatchedPackageCount(), op.GetMatchedEndpointCount())
			item.newHuman = item.New
		}
		v.Items = append(v.Items, item)
	}
	for _, c := range res.GetCoverage() {
		value, _ := cloudpolicy.FormatJSONValue(c.GetPath(), c.GetValue())
		if c.GetValue() == "" {
			value = ""
		}
		v.Coverage = append(v.Coverage, coverageView{
			Path: c.GetPath(), Value: value, Enforce: c.GetEnforceCount(), Partial: c.GetPartialCount(),
			Detect: c.GetDetectCount(), Unsupported: c.GetUnsupportedCount(),
		})
	}
	for _, w := range res.GetWarnings() {
		v.Warnings = append(v.Warnings, warningView{
			Code:    strings.TrimPrefix(w.GetCode().String(), warningCodePrefix),
			Subject: w.GetSubject(),
			Message: w.GetMessage(),
		})
	}
	return v
}

// overrideChangeValue gives the human form of a value of an override change.
// The server writes a selector as protojson. The table and the publish
// warning show it in the selector syntax of the CLI, as in
// custom/criticality=high. A value that does not parse stays as the server
// wrote it.
func overrideChangeValue(kind policyv1.OverrideChangeKind, value string) string {
	switch {
	case kind == policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_EXPIRY:
		return expiryValue(value)
	case kind != policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_SELECTOR || value == "":
		return cloudpolicy.EmptyDash(value)
	}
	selector := &messagescontroltowerv1.LabelSelector{}
	if err := protojson.Unmarshal([]byte(value), selector); err != nil {
		return value
	}
	return cloudpolicy.FormatSelector(selector)
}

// expiryValue reads an expiry that the server writes as a protojson time,
// as "2027-01-26T00:00:00Z" with its quotes. No expiry is "none".
func expiryValue(value string) string {
	if value == "" {
		return "none"
	}
	var text string
	if err := json.Unmarshal([]byte(value), &text); err != nil {
		return value
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return text
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// overrideChangeItem turns one change to a policy override into a row. Each
// kind gets its own sentence for the publish warning. An added or removed
// override has no old and new values to show, so its row says what happens.
func overrideChangeItem(c *policyv1.OverrideChange) changeItem {
	kind := c.GetKind()
	item := changeItem{
		Where:     fmt.Sprintf("override %q", c.GetOverrideName()),
		What:      tui.EnumToken(kind.String(), overrideKindPrefix),
		Old:       c.GetOldValue(),
		New:       c.GetNewValue(),
		Direction: directionToken(c.GetDirection()),
		oldHuman:  overrideChangeValue(kind, c.GetOldValue()),
		newHuman:  overrideChangeValue(kind, c.GetNewValue()),
	}
	name := c.GetOverrideName()
	changes := func(what string) string {
		return fmt.Sprintf("It changes the %s of policy override %q from %s to %s.", what, name, item.oldHuman, item.newHuman)
	}
	switch kind {
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_ADDED:
		item.whatHuman, item.changeHuman = "new policy override", "-"
		item.sentence = fmt.Sprintf("It adds policy override %q.", name)
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_REMOVED:
		item.whatHuman, item.changeHuman = "removed policy override", "-"
		item.sentence = fmt.Sprintf("It removes policy override %q.", name)
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_SELECTOR:
		item.sentence = changes("selector")
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_PRIORITY:
		item.sentence = changes("priority")
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_EXPIRY:
		item.sentence = changes("expiry")
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_NAME:
		item.sentence = fmt.Sprintf("It renames policy override %q to %q.", item.oldHuman, item.newHuman)
	case policyv1.OverrideChangeKind_OVERRIDE_CHANGE_KIND_DESCRIPTION:
		item.sentence = fmt.Sprintf("It changes the description of policy override %q.", name)
	}
	return item
}

func ruleOperationItems(ops []*policyv1.PackageRuleOperation) []changeItem {
	out := make([]changeItem, 0, len(ops))
	for _, op := range ops {
		out = append(out, ruleOperationItem(op))
	}
	return out
}

func ruleOperationItem(op *policyv1.PackageRuleOperation) changeItem {
	item := changeItem{Where: "package rules", oldHuman: "-", newHuman: "-"}
	switch {
	case op.HasCreate():
		rule := op.GetCreate().GetRule()
		item.What = fmt.Sprintf("create %s %s",
			tui.EnumToken(rule.GetEffect().String(), effectPrefix), selectorText(rule.GetSelector()))
		if id := op.GetCreate().GetOverrideId(); id != "" {
			item.Where = "package rules of override " + id
		}
	case op.HasUpdate():
		item.What = "update " + op.GetUpdate().GetRuleId()
	case op.HasDelete():
		item.What = "delete " + op.GetDelete().GetRuleId()
	}
	return item
}
