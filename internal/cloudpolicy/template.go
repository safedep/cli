package cloudpolicy

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	yaml "go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/tui"
)

// The edit template is the policy settings document with every settings
// field of the contract. A field that the document sets is a YAML line. A
// field that it does not set is a commented YAML line at its nesting, so a
// person removes the "# " to set it. The walk reads the descriptor, so a new
// contract field shows after an SDK bump with no change here. The decoder
// skips the comments, so a save of the template as it is changes nothing.

const (
	// templateWidth is the longest line that a field note shares. A longer
	// note goes on its own comment line above the field.
	templateWidth = 100

	// examplePlaceholderDuration is the value of a duration field that no
	// layer sets, as an example of the form.
	examplePlaceholderDuration = 7 * 24 * time.Hour
)

var (
	policySpecFullName = (&specv1.PolicySpec{}).ProtoReflect().Descriptor().FullName()
	policyFullName     = (&policyv1.Policy{}).ProtoReflect().Descriptor().FullName()
)

// DefaultLayer is the SafeDep default policy, the layer below the base.
type DefaultLayer struct {
	Revision uint64
	Spec     *specv1.PolicySpec
}

// belowLayer is a layer below the one that the template writes, with its
// name for the field notes.
type belowLayer struct {
	name string
	spec *specv1.PolicySpec
}

type templateWriter struct {
	def   DefaultLayer
	base  *specv1.PolicySpec
	lines []string
}

// MarshalPolicyTemplate writes the policy settings document for an edit:
// the header, the set fields, every other settings field as a commented line
// with a note, and an example policy override when the policy has none.
func MarshalPolicyTemplate(p *policyv1.Policy, header string, def DefaultLayer) ([]byte, error) {
	if p == nil {
		p = &policyv1.Policy{}
	}
	w := &templateWriter{def: def, base: p.GetBase()}
	if err := w.messageFields(p.ProtoReflect(), 0); err != nil {
		return nil, err
	}
	if len(p.GetOverrides()) == 0 {
		if err := w.overrideExample(); err != nil {
			return nil, err
		}
	}
	return w.bytes(header), nil
}

// MarshalOverrideTemplate writes one policy override for an edit, in the
// form of MarshalPolicyTemplate. base is the policy base of the document,
// for the notes of the fields that the override does not set.
func MarshalOverrideTemplate(o *policyv1.Override, header string, def DefaultLayer, base *specv1.PolicySpec) ([]byte, error) {
	w := &templateWriter{def: def, base: base}
	if err := w.messageFields(o.ProtoReflect(), 0); err != nil {
		return nil, err
	}
	return w.bytes(header), nil
}

func (w *templateWriter) bytes(header string) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	for _, line := range w.lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func (w *templateWriter) real(indent int, text string) {
	w.lines = append(w.lines, strings.Repeat(" ", indent)+text)
}

func (w *templateWriter) comment(indent int, text string) {
	w.lines = append(w.lines, strings.Repeat(" ", indent)+"# "+text)
}

// messageFields writes the fields of a document message: a policy settings
// spec as a template, a list of messages item by item, and each other set
// field in the form of the codec.
func (w *templateWriter) messageFields(m protoreflect.Message, indent int) error {
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if _, skipped := documentCodec.skip[fd.FullName()]; skipped {
			continue
		}
		key := documentCodec.key(fd)
		switch {
		case isSpecField(fd):
			below := w.belowFor(m.Descriptor().FullName())
			if m.Has(fd) {
				w.real(indent, key+":")
				if err := w.specFields(fd.Message(), m.Get(fd).Message(), "", indent+2, below); err != nil {
					return err
				}
				continue
			}
			w.comment(indent, key+":")
			if err := w.specFields(fd.Message(), nil, "", indent+2, below); err != nil {
				return err
			}
		case !m.Has(fd):
		case fd.IsList() && isMessageKind(fd) && !isFormattedMessage(fd.Message()):
			w.real(indent, key+":")
			list := m.Get(fd).List()
			for j := 0; j < list.Len(); j++ {
				if err := w.listItem(list.Get(j).Message(), indent+2); err != nil {
					return err
				}
			}
		default:
			lines, err := fieldYAML(fd, m.Get(fd))
			if err != nil {
				return err
			}
			for _, line := range lines {
				w.real(indent, line)
			}
		}
	}
	return nil
}

// listItem writes one item of a list of messages. The first YAML line of the
// item carries the "- " of the list.
func (w *templateWriter) listItem(m protoreflect.Message, indent int) error {
	start := len(w.lines)
	if err := w.messageFields(m, indent+2); err != nil {
		return err
	}
	itemIndent := strings.Repeat(" ", indent+2)
	for i := start; i < len(w.lines); i++ {
		line := w.lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		w.lines[i] = strings.Repeat(" ", indent) + "- " + strings.TrimPrefix(line, itemIndent)
		return nil
	}
	// An item with no set field is an empty message.
	w.lines = append(w.lines[:start], append([]string{strings.Repeat(" ", indent) + "- {}"}, w.lines[start:]...)...)
	return nil
}

// belowFor gives the layers below a policy settings spec of a message: the
// default policy for the base, and the base and the default policy for an
// override.
func (w *templateWriter) belowFor(owner protoreflect.FullName) []belowLayer {
	def := belowLayer{
		name: fmt.Sprintf("default policy, revision %d", w.def.Revision),
		spec: w.def.Spec,
	}
	if owner == policyFullName {
		return []belowLayer{def}
	}
	return []belowLayer{{name: "base", spec: w.base}, def}
}

// specFields writes the fields of a policy settings message. msg is nil when
// the document does not set the message, and then every line is a comment.
func (w *templateWriter) specFields(
	md protoreflect.MessageDescriptor,
	msg protoreflect.Message,
	path string,
	indent int,
	below []belowLayer,
) error {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if _, skipped := documentCodec.skip[fd.FullName()]; skipped {
			continue
		}
		fieldPath := joinPath(path, string(fd.Name()))
		set := msg != nil && msg.Has(fd)
		if isTemplateLeaf(fd) {
			if err := w.leaf(fd, msg, set, fieldPath, indent, below); err != nil {
				return err
			}
			continue
		}
		key := documentCodec.key(fd)
		var child protoreflect.Message
		if set {
			w.real(indent, key+":")
			child = msg.Get(fd).Message()
		} else {
			w.comment(indent, key+":")
		}
		if err := w.specFields(fd.Message(), child, fieldPath, indent+2, below); err != nil {
			return err
		}
	}
	return nil
}

// leaf writes one settings field. An unset field shows the value that the
// subjects get now, or an example of the form, with a note.
func (w *templateWriter) leaf(
	fd protoreflect.FieldDescriptor,
	msg protoreflect.Message,
	set bool,
	path string,
	indent int,
	below []belowLayer,
) error {
	if set {
		lines, err := fieldYAML(fd, msg.Get(fd))
		if err != nil {
			return err
		}
		for _, line := range lines {
			w.real(indent, line)
		}
		return nil
	}

	value := placeholderValue(fd)
	now := "Now: not set"
	for _, layer := range below {
		if v, ok := valueAtPath(layer.spec, path); ok {
			value = v
			_, human := formatFieldValue(fd, v)
			now = fmt.Sprintf("Now: %s (%s)", human, layer.name)
			break
		}
	}
	lines, err := commentedFieldYAML(fd, value)
	if err != nil {
		return err
	}
	note := tui.EscapeLine(valueForm(fd) + ". " + now)
	first := strings.Repeat(" ", indent) + "# " + lines[0]
	if len(first)+len("  # ")+len(note) <= templateWidth {
		w.lines = append(w.lines, first+"  # "+note)
	} else {
		w.comment(indent, note)
		w.lines = append(w.lines, first)
	}
	for _, line := range lines[1:] {
		w.comment(indent, line)
	}
	return nil
}

// overrideExample writes a commented policy override after the base, for a
// policy that has none. The codec writes it, so the keys are the ones that
// it reads.
func (w *templateWriter) overrideExample() error {
	selector := &messagescontroltowerv1.LabelSelector{}
	selector.SetMatchLabels(map[string]string{"custom/team": "payments"})
	spec := &specv1.PolicySpec{}
	if path, ok := firstLeafPath(spec.ProtoReflect().Descriptor(), ""); ok {
		setPlaceholder(spec.ProtoReflect(), path)
	}
	o := &policyv1.Override{}
	o.SetName("payments")
	o.SetPriority(100)
	o.SetSelector(selector)
	o.SetSpec(spec)
	example := &policyv1.Policy{}
	example.SetOverrides([]*policyv1.Override{o})

	text, err := MarshalPolicy(example, "")
	if err != nil {
		return err
	}
	w.lines = append(w.lines,
		"",
		"# An example policy override. It applies to the subjects that its selector matches.",
		"# Remove the \"# \" at the start of each line to use it, then change the values.",
	)
	for _, line := range strings.Split(strings.TrimRight(string(text), "\n"), "\n") {
		w.lines = append(w.lines, "# "+line)
	}
	return nil
}

// HasUserComments reports whether the saved file holds a comment that the
// CLI did not write. The policy keeps no comments, so a person loses them on
// save. written is the text that the CLI wrote: its header, the commented
// fields and their notes. A field line that a person uncomments keeps its
// note, so each part of a written comment line from a "#" on counts too.
func HasUserComments(data, written []byte) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false
	}
	known := writtenComments(written)
	var comments []string
	collectComments(&root, &comments)
	for _, c := range comments {
		for _, line := range strings.Split(c, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := known[line]; !ok {
				return true
			}
		}
	}
	return false
}

func writtenComments(written []byte) map[string]struct{} {
	known := map[string]struct{}{}
	for _, line := range strings.Split(string(written), "\n") {
		for i := 0; i < len(line); i++ {
			if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				known[strings.TrimSpace(line[i:])] = struct{}{}
			}
		}
	}
	return known
}

func collectComments(n *yaml.Node, out *[]string) {
	for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
		if c != "" {
			*out = append(*out, c)
		}
	}
	for _, child := range n.Content {
		collectComments(child, out)
	}
}

func isSpecField(fd protoreflect.FieldDescriptor) bool {
	return !fd.IsList() && !fd.IsMap() && isMessageKind(fd) && fd.Message().FullName() == policySpecFullName
}

// isFormattedMessage reports a message type that the codec writes in a form
// for people instead of field by field.
func isFormattedMessage(md protoreflect.MessageDescriptor) bool {
	switch md.FullName() {
	case durationFullName, timestampFullName, labelSelectorFullName:
		return true
	}
	return false
}

func isTemplateLeaf(fd protoreflect.FieldDescriptor) bool {
	return isLeaf(fd) || isFormattedMessage(fd.Message())
}

// fieldYAML writes one field as the codec does, as "key: value" lines.
func fieldYAML(fd protoreflect.FieldDescriptor, v protoreflect.Value) ([]string, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	node.Content = append(node.Content, scalarNode(documentCodec.key(fd)), documentCodec.encodeField(fd, v))
	text, err := marshalNode(node, "")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(text), "\n"), "\n"), nil
}

// commentedFieldYAML writes a commented field as fieldYAML does, but a
// duration in days when that keeps the value, as 7d for 168h. The codec reads
// both forms.
func commentedFieldYAML(fd protoreflect.FieldDescriptor, v protoreflect.Value) ([]string, error) {
	if fd.IsList() || fd.IsMap() || !isMessageKind(fd) || fd.Message().FullName() != durationFullName {
		return fieldYAML(fd, v)
	}
	if d, ok := v.Message().Interface().(*durationpb.Duration); ok {
		text := humanDuration(d.AsDuration())
		if parsed, err := duration.Parse(text); err != nil || parsed != d.AsDuration() {
			text = duration.Format(d.AsDuration())
		}
		return []string{documentCodec.key(fd) + ": " + text}, nil
	}
	return fieldYAML(fd, v)
}

// placeholderValue is an example value of a field that no layer sets: the
// first value of an enum, 7d for a duration, or the zero value.
func placeholderValue(fd protoreflect.FieldDescriptor) protoreflect.Value {
	if fd.IsList() || fd.IsMap() {
		return newField(fd)
	}
	switch fd.Kind() {
	case protoreflect.EnumKind:
		for i := 0; i < fd.Enum().Values().Len(); i++ {
			if v := fd.Enum().Values().Get(i); v.Number() != 0 {
				return protoreflect.ValueOfEnum(v.Number())
			}
		}
		return fd.Default()
	case protoreflect.MessageKind, protoreflect.GroupKind:
		switch fd.Message().FullName() {
		case durationFullName:
			return protoreflect.ValueOfMessage(durationpb.New(examplePlaceholderDuration).ProtoReflect())
		case timestampFullName:
			return protoreflect.ValueOfMessage(timestamppb.New(time.Unix(0, 0)).ProtoReflect())
		}
		return newField(fd)
	default:
		return fd.Default()
	}
}

// newField makes an empty value of a list, map or message field, through
// the type of the message that holds the field.
func newField(fd protoreflect.FieldDescriptor) protoreflect.Value {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(fd.ContainingMessage().FullName())
	if err != nil {
		return fd.Default()
	}
	return mt.New().NewField(fd)
}

// valueForm describes the values that a field accepts, in the words of the
// codec.
func valueForm(fd protoreflect.FieldDescriptor) string {
	form := singularForm(fd)
	switch {
	case fd.IsList():
		return "a list of: " + form
	case fd.IsMap():
		return "a mapping of: " + form
	}
	return form
}

func singularForm(fd protoreflect.FieldDescriptor) string {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		return strings.Join(enumTokens(fd.Enum()), ", ")
	case protoreflect.BoolKind:
		return "true or false"
	case protoreflect.StringKind:
		return "text"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "a number"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		switch fd.Message().FullName() {
		case durationFullName:
			return "a duration, as 7d or 12h" + durationRange(fd)
		case timestampFullName:
			return "a time, as 2027-01-26 or 2027-01-26T00:00:00Z"
		case labelSelectorFullName:
			return "a label selector, with matchLabels or matchExpressions"
		}
		return "a mapping"
	default:
		return "a whole number"
	}
}

// durationRange reads the bounds of a duration field from its protovalidate
// rules, as ", from 0s to 365d".
func durationRange(fd protoreflect.FieldDescriptor) string {
	rules, ok := proto.GetExtension(fd.Options(), validate.E_Field).(*validate.FieldRules)
	if !ok || !rules.HasDuration() {
		return ""
	}
	d := rules.GetDuration()
	var low, high string
	switch {
	case d.HasGte():
		low = humanDuration(d.GetGte().AsDuration())
	case d.HasGt():
		low = "more than " + humanDuration(d.GetGt().AsDuration())
	}
	switch {
	case d.HasLte():
		high = humanDuration(d.GetLte().AsDuration())
	case d.HasLt():
		high = "less than " + humanDuration(d.GetLt().AsDuration())
	}
	switch {
	case low != "" && high != "":
		return ", from " + low + " to " + high
	case low != "":
		return ", at least " + low
	case high != "":
		return ", at most " + high
	}
	return ""
}

// valueAtPath reads a set settings field of spec by its contract path.
func valueAtPath(spec *specv1.PolicySpec, path string) (protoreflect.Value, bool) {
	if spec == nil {
		return protoreflect.Value{}, false
	}
	m := spec.ProtoReflect()
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(segment))
		if fd == nil || !m.Has(fd) {
			return protoreflect.Value{}, false
		}
		if i == len(segments)-1 {
			return m.Get(fd), true
		}
		m = m.Get(fd).Message()
	}
	return protoreflect.Value{}, false
}

// firstLeafPath finds the first settings field in descriptor order.
func firstLeafPath(md protoreflect.MessageDescriptor, path string) (string, bool) {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if _, skipped := documentCodec.skip[fd.FullName()]; skipped {
			continue
		}
		fieldPath := joinPath(path, string(fd.Name()))
		if isTemplateLeaf(fd) {
			return fieldPath, true
		}
		if p, ok := firstLeafPath(fd.Message(), fieldPath); ok {
			return p, true
		}
	}
	return "", false
}

// setPlaceholder sets the field at path to its placeholder value.
func setPlaceholder(m protoreflect.Message, path string) {
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		fd := m.Descriptor().Fields().ByName(protoreflect.Name(segment))
		if fd == nil {
			return
		}
		if i == len(segments)-1 {
			if fd.IsList() || fd.IsMap() {
				return
			}
			m.Set(fd, placeholderValue(fd))
			return
		}
		m = m.Mutable(fd).Message()
	}
}
