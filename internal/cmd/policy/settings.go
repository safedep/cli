package policy

import (
	"context"
	"errors"
	"fmt"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"github.com/safedep/dry/tui"
	"github.com/safedep/dry/usefulerror"
	"google.golang.org/protobuf/proto"

	"github.com/safedep/cli/internal/cloudpolicy"
	clitui "github.com/safedep/cli/internal/tui"
)

// editSettings opens the settings, or one policy override of them, in the
// editor and gives the whole edited document to commit.
func editSettings(
	ctx context.Context,
	open openEditorFunc,
	start *policyv1.Policy,
	header string,
	def cloudpolicy.DefaultLayer,
	overrideName string,
	commit func(*policyv1.Policy) error,
) error {
	if start == nil {
		start = &policyv1.Policy{}
	}
	if err := requireKnownFields(start); err != nil {
		return err
	}
	if overrideName == "" {
		return editWholeDocument(ctx, open, start, header, def, commit)
	}
	return editOneOverride(ctx, open, start, header, def, overrideName, commit)
}

// defaultLayer is the SafeDep default policy of a GetPolicy reply, for the
// notes of the edit template.
func defaultLayer(head *policyv1.GetPolicyResponse) cloudpolicy.DefaultLayer {
	return cloudpolicy.DefaultLayer{
		Revision: head.GetDefaultPolicy().GetRevision(),
		Spec:     head.GetDefaultPolicy().GetSpec(),
	}
}

// decodeDocument reads a saved document. The template of a policy with no
// settings has only comments, so a save of it as it is reads as that empty
// policy. A file with only comments for a policy with settings stays an
// error, so a person never clears the policy by accident.
func decodeDocument(saved []byte, start *policyv1.Policy) (*policyv1.Policy, error) {
	edited, err := cloudpolicy.UnmarshalPolicy(saved)
	if errors.Is(err, cloudpolicy.ErrEmptyDocument) && proto.Size(start) == 0 {
		return &policyv1.Policy{}, nil
	}
	return edited, err
}

func editWholeDocument(
	ctx context.Context,
	open openEditorFunc,
	start *policyv1.Policy,
	header string,
	def cloudpolicy.DefaultLayer,
	commit func(*policyv1.Policy) error,
) error {
	text, err := cloudpolicy.MarshalPolicyTemplate(start, header, def)
	if err != nil {
		return err
	}
	unchanged := func(saved []byte) bool {
		edited, err := decodeDocument(saved, start)
		return err == nil && proto.Equal(edited, start)
	}
	return editLoop(ctx, open, text, unchanged, func(saved []byte) error {
		edited, err := decodeDocument(saved, start)
		if err != nil {
			return retryable(err)
		}
		if err := checkEditedSettings(start, edited); err != nil {
			return retryable(err)
		}
		warnComments(saved, text)
		return commit(edited)
	})
}

func editOneOverride(
	ctx context.Context,
	open openEditorFunc,
	start *policyv1.Policy,
	header string,
	def cloudpolicy.DefaultLayer,
	overrideName string,
	commit func(*policyv1.Policy) error,
) error {
	current, err := findOverride(start, overrideName)
	if err != nil {
		return err
	}
	text, err := cloudpolicy.MarshalOverrideTemplate(current, header, def, start.GetBase())
	if err != nil {
		return err
	}
	unchanged := func(saved []byte) bool {
		edited, err := cloudpolicy.UnmarshalOverride(saved)
		return err == nil && proto.Equal(edited, current)
	}
	return editLoop(ctx, open, text, unchanged, func(saved []byte) error {
		edited, err := cloudpolicy.UnmarshalOverride(saved)
		if err != nil {
			return retryable(err)
		}
		full, ok := proto.Clone(start).(*policyv1.Policy)
		if !ok {
			return errors.New("policy edit: clone the policy")
		}
		overrides := full.GetOverrides()
		for i, o := range overrides {
			if o.GetOverrideId() == current.GetOverrideId() {
				overrides[i] = edited
			}
		}
		full.SetOverrides(overrides)
		if err := checkEditedSettings(start, full); err != nil {
			return retryable(err)
		}
		warnComments(saved, text)
		return commit(full)
	})
}

// requireKnownFields stops an edit of a policy with fields that this CLI
// does not know, because the save would drop them.
func requireKnownFields(p *policyv1.Policy) error {
	err := cloudpolicy.CheckKnownFields(p)
	if err == nil {
		return nil
	}
	return usefulerror.NewUsefulError().
		WithCode(usefulerror.ErrBadRequest).
		WithHumanError(err.Error() + ". An edit would drop them").
		WithHelp("Update the safedep CLI to the latest release, then run the command again").
		Wrap(err)
}

// checkEditedSettings checks the rules that a person breaks most often in an
// edit, so the editor shows them before the RPC. The server checks every
// rule. It also warns about each policy override that the edit removes.
func checkEditedSettings(start, edited *policyv1.Policy) error {
	if err := validateSettings(edited); err != nil {
		return err
	}
	known := make(map[string]*policyv1.Override, len(start.GetOverrides()))
	for _, o := range start.GetOverrides() {
		known[o.GetOverrideId()] = o
	}
	kept := make(map[string]struct{}, len(edited.GetOverrides()))
	for _, o := range edited.GetOverrides() {
		id := o.GetOverrideId()
		if id == "" {
			continue
		}
		if _, ok := known[id]; !ok {
			return fmt.Errorf("override %q: id %q is not in the policy. Remove the id line for a new override",
				o.GetName(), id)
		}
		if _, dup := kept[id]; dup {
			return fmt.Errorf("override %q: id %q is also the id of another override. Each id must be unique",
				o.GetName(), id)
		}
		kept[id] = struct{}{}
	}
	for _, o := range start.GetOverrides() {
		if _, ok := kept[o.GetOverrideId()]; !ok {
			tui.Warning("This removes policy override %q (id %s). The server refuses the removal while package rules attach to it",
				clitui.EscapeLine(o.GetName()), clitui.EscapeLine(o.GetOverrideId()))
		}
	}
	return nil
}

func validateSettings(p *policyv1.Policy) error {
	names := map[string]struct{}{}
	priorities := map[uint32]string{}
	for i, o := range p.GetOverrides() {
		if o.GetName() == "" {
			return fmt.Errorf("overrides[%d]: give the override a name", i)
		}
		if _, dup := names[o.GetName()]; dup {
			return fmt.Errorf("overrides[%d]: two overrides are named %q. Each name must be unique", i, o.GetName())
		}
		names[o.GetName()] = struct{}{}
		if o.GetPriority() == 0 {
			return fmt.Errorf("override %q: give it a priority from 1 to 1000000", o.GetName())
		}
		if other, dup := priorities[o.GetPriority()]; dup {
			return fmt.Errorf("override %q: priority %d is also the priority of %q. Each priority must be unique",
				o.GetName(), o.GetPriority(), other)
		}
		priorities[o.GetPriority()] = o.GetName()
		if !o.HasSelector() {
			return fmt.Errorf("override %q: give it a selector. Write selector: {} to select every subject", o.GetName())
		}
	}
	return nil
}

func warnComments(saved, written []byte) {
	if cloudpolicy.HasUserComments(saved, written) {
		tui.Warning("The file holds comments. The policy keeps no comments, so they are lost. " +
			"Write the reason in a description field")
	}
}
