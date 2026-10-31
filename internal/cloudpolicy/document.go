// Package cloudpolicy holds the client side of SafeDep Cloud policy
// management that more than one command tree uses: the YAML form of the
// policy settings document, the display of contract values, labels and label
// selectors, and the policy of one subject. The policy, project and endpoint
// command packages read it, because sibling command packages must not import
// each other.
package cloudpolicy

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	yaml "go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// documentCodec writes the policy settings document. It leaves out the
// package rules, because admins change them through rule operations, and the
// override ids, which the server owns. RestoreOverrideIDs puts the ids back
// by name before a save.
var documentCodec = &yamlCodec{skip: map[protoreflect.FullName]string{
	"safedep.policy.spec.v1.PolicySpec.core": "package rules are not in this file. " +
		"Change them with: safedep policy rule",
	"safedep.services.controltower.policy.v1.Override.override_id": "",
}}

// ErrEmptyDocument is the error for a saved file with no content.
var ErrEmptyDocument = errors.New("the file is empty")

// CommentBlock turns lines into YAML comment lines and ends the block with a
// blank line. An empty line becomes a bare "#".
func CommentBlock(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		if line == "" {
			b.WriteString("#\n")
			continue
		}
		b.WriteString("# ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

// MarshalPolicy writes the policy settings document as YAML, after the
// header.
func MarshalPolicy(p *policyv1.Policy, header string) ([]byte, error) {
	if p == nil {
		p = &policyv1.Policy{}
	}
	return marshalNode(documentCodec.encodeMessage(p.ProtoReflect()), header)
}

// UnmarshalPolicy reads a policy settings document.
func UnmarshalPolicy(data []byte) (*policyv1.Policy, error) {
	node, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	p := &policyv1.Policy{}
	if err := documentCodec.decodeMessage(node, p.ProtoReflect(), ""); err != nil {
		return nil, err
	}
	return p, nil
}

// MarshalOverride writes one policy override as YAML, after the header.
func MarshalOverride(o *policyv1.Override, header string) ([]byte, error) {
	return marshalNode(documentCodec.encodeMessage(o.ProtoReflect()), header)
}

// UnmarshalOverride reads one policy override.
func UnmarshalOverride(data []byte) (*policyv1.Override, error) {
	node, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	o := &policyv1.Override{}
	if err := documentCodec.decodeMessage(node, o.ProtoReflect(), "override"); err != nil {
		return nil, err
	}
	return o, nil
}

// PolicyMap returns the policy settings document as plain maps and lists, in
// the same form as the YAML, for JSON output.
func PolicyMap(p *policyv1.Policy) (any, error) {
	if p == nil {
		p = &policyv1.Policy{}
	}
	var out any
	if err := documentCodec.encodeMessage(p.ProtoReflect()).Decode(&out); err != nil {
		return nil, fmt.Errorf("policy document: %w", err)
	}
	return out, nil
}

// RestoreOverrideIDs gives each edited policy override with no id the id of
// the head policy override with the same name. The document leaves the ids
// out, so the server can tell an edit of a policy override from a new one.
func RestoreOverrideIDs(edited, head *policyv1.Policy) {
	ids := make(map[string]string, len(head.GetOverrides()))
	for _, o := range head.GetOverrides() {
		ids[o.GetName()] = o.GetOverrideId()
	}
	for _, o := range edited.GetOverrides() {
		if o.GetOverrideId() != "" {
			continue
		}
		if id, ok := ids[o.GetName()]; ok {
			o.SetOverrideId(id)
		}
	}
}

// HasUserComments reports whether the saved file holds a comment other than
// the header that the CLI wrote. The policy keeps no comments, so a person
// loses them on save.
func HasUserComments(data []byte, header string) bool {
	body := bytes.TrimPrefix(data, []byte(header))
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return false
	}
	return hasComment(&root)
}

func hasComment(n *yaml.Node) bool {
	if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
		return true
	}
	for _, child := range n.Content {
		if hasComment(child) {
			return true
		}
	}
	return false
}

func marshalNode(node *yaml.Node, header string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, fmt.Errorf("policy document: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("policy document: %w", err)
	}
	return buf.Bytes(), nil
}

func parseDocument(data []byte) (*yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("the file is not valid YAML: %w", err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, ErrEmptyDocument
	}
	node := root.Content[0]
	if isNull(node) {
		return nil, ErrEmptyDocument
	}
	return node, nil
}
