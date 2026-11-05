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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// documentCodec writes the policy settings document. It leaves out the
// package rules, because admins change them through rule operations. It
// writes the id of each policy override as "id", so a rename keeps the
// override: the server sets the id, keeps it, and refuses an id that is not
// at head. An override with no id is a new one.
var documentCodec = &yamlCodec{
	skip: map[protoreflect.FullName]string{
		"safedep.policy.spec.v1.PolicySpec.core": "package rules are not in this file. " +
			"Change them with: safedep policy rule",
	},
	names: map[protoreflect.FullName]string{
		"safedep.services.controltower.policy.v1.Override.override_id": "id",
	},
}

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

// ErrUnknownFields is the error for a policy with fields that the SDK of
// this CLI does not know.
var ErrUnknownFields = errors.New("the policy holds fields that this safedep CLI does not know")

// CheckKnownFields walks every level of a message and fails when one holds
// fields that the SDK of this CLI does not know. An edit cannot show such a
// field, and a save would drop it from the policy, so the edit must stop.
func CheckKnownFields(m proto.Message) error {
	if m == nil {
		return nil
	}
	var paths []string
	collectUnknown(m.ProtoReflect(), "", &paths)
	if len(paths) == 0 {
		return nil
	}
	return fmt.Errorf("%w: at %s", ErrUnknownFields, strings.Join(paths, ", "))
}

func collectUnknown(m protoreflect.Message, path string, out *[]string) {
	if len(m.GetUnknown()) > 0 {
		at := path
		if at == "" {
			at = "the document root"
		}
		*out = append(*out, at)
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		fieldPath := joinPath(path, string(fd.Name()))
		switch {
		case fd.IsList() && isMessageKind(fd):
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				collectUnknown(list.Get(i).Message(), fmt.Sprintf("%s[%d]", fieldPath, i), out)
			}
		case fd.IsMap() && isMessageKind(fd.MapValue()):
			v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
				collectUnknown(mv.Message(), joinPath(fieldPath, k.String()), out)
				return true
			})
		case !fd.IsList() && !fd.IsMap() && isMessageKind(fd):
			collectUnknown(v.Message(), fieldPath, out)
		}
		return true
	})
}

func isMessageKind(fd protoreflect.FieldDescriptor) bool {
	return fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind
}
