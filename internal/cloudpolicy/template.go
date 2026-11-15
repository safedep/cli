package cloudpolicy

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	packageadmissionv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/packageadmission/v1"
	scangatev1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/scangate/v1"
	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	yaml "go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"

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

	// placeholderDuration is the value of a duration field that no layer
	// sets, as an example of the form.
	placeholderDuration = 7 * 24 * time.Hour
)

var (
	policySpecFullName = (&specv1.PolicySpec{}).ProtoReflect().Descriptor().FullName()
	policyFullName     = (&policyv1.Policy{}).ProtoReflect().Descriptor().FullName()

	// baseDescriptionFullName is the field that holds the reason for the
	// values of the base. The template shows it commented when it is unset,
	// because the header asks for the reason in a description field.
	baseDescriptionFullName = policyFullName.Append("base_description")

	licenseGateFullName = (&scangatev1.LicenseGate{}).ProtoReflect().Descriptor().FullName()

	// fieldNotes give the meaning of a field whose value form alone misleads
	// a person. An empty license list is a value, and the gate reads the
	// lists before unknown.
	fieldNotes = map[protoreflect.FullName]string{
		licenseGateFullName.Append("deny"): "The licenses that fail the gate. A list replaces the list below it. " +
			"deny: {licenses: []} denies nothing.",
		licenseGateFullName.Append("allow"): "With entries, a license outside the list fails the gate. " +
			"A list replaces the list below it. An empty list turns the allow list off.",
		licenseGateFullName.Append("unknown"): "What the gate does on a package with an unknown license " +
			"that the lists do not fail.",
	}
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

// notePrefix opens the part of a note that names the value below a field.
// A field of the base gets the value that its subjects get now. A field of
// an override gets only the value of the base or the default policy: an
// override of a lower priority can also set it, for some subjects.
type notePrefix struct {
	label  string
	notSet string
}

var (
	baseNote = notePrefix{
		label:  "Now",
		notSet: "not set, each tool uses its local value",
	}
	overrideNote = notePrefix{
		label:  "Below this override",
		notSet: "not set",
	}
)

type templateWriter struct {
	def   DefaultLayer
	base  *specv1.PolicySpec
	lines []string
	// example writes only the set fields, with durations in days, for the
	// commented example override.
	example bool
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
			if err := w.spec(key, fd, m, indent); err != nil {
				return err
			}
		case !m.Has(fd):
			if fd.FullName() == baseDescriptionFullName && !w.example {
				w.comment(indent, key+": Why the base has these values  # text")
			}
		case fd.IsList() && isMessageKind(fd) && !isFormattedMessage(fd.Message()):
			w.real(indent, key+":")
			list := m.Get(fd).List()
			for j := 0; j < list.Len(); j++ {
				if err := w.listItem(list.Get(j).Message(), indent+2); err != nil {
					return err
				}
			}
		default:
			if err := w.realField(fd, m.Get(fd), indent); err != nil {
				return err
			}
		}
	}
	return nil
}

// spec writes a policy settings spec field of m. An unset spec is a
// commented block.
func (w *templateWriter) spec(key string, fd protoreflect.FieldDescriptor, m protoreflect.Message, indent int) error {
	below, note := w.belowFor(m.Descriptor().FullName())
	var msg protoreflect.Message
	switch {
	case m.Has(fd):
		w.real(indent, key+":")
		msg = m.Get(fd).Message()
	case w.example:
		return nil
	default:
		w.comment(indent, key+":")
	}
	return w.specFields(fd.Message(), msg, "", indent+2, below, note)
}

func (w *templateWriter) realField(fd protoreflect.FieldDescriptor, v protoreflect.Value, indent int) error {
	lines, err := fieldYAML(fd, v, w.example)
	if err != nil {
		return err
	}
	for _, line := range lines {
		w.real(indent, line)
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
func (w *templateWriter) belowFor(owner protoreflect.FullName) ([]belowLayer, notePrefix) {
	def := belowLayer{
		name: fmt.Sprintf("default policy, revision %d", w.def.Revision),
		spec: w.def.Spec,
	}
	if owner == policyFullName {
		return []belowLayer{def}, baseNote
	}
	return []belowLayer{{name: "base", spec: w.base}, def}, overrideNote
}

// specFields writes the fields of a policy settings message. msg is nil when
// the document does not set the message, and then every line is a comment.
func (w *templateWriter) specFields(
	md protoreflect.MessageDescriptor,
	msg protoreflect.Message,
	path string,
	indent int,
	below []belowLayer,
	note notePrefix,
) error {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if _, skipped := documentCodec.skip[fd.FullName()]; skipped {
			continue
		}
		fieldPath := joinPath(path, string(fd.Name()))
		set := msg != nil && msg.Has(fd)
		if w.example && !set {
			continue
		}
		if isLeaf(fd) {
			if set {
				if err := w.realField(fd, msg.Get(fd), indent); err != nil {
					return err
				}
				continue
			}
			if err := w.commentedLeaf(fd, fieldPath, indent, below, note); err != nil {
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
		if err := w.specFields(fd.Message(), child, fieldPath, indent+2, below, note); err != nil {
			return err
		}
	}
	return nil
}

// commentedLeaf writes one unset settings field as a comment. It shows the
// value of the nearest layer below that sets it, or an example of the form,
// with a note.
func (w *templateWriter) commentedLeaf(
	fd protoreflect.FieldDescriptor,
	path string,
	indent int,
	below []belowLayer,
	note notePrefix,
) error {
	value, hasValue := placeholderValue(fd)
	now := note.label + ": " + note.notSet
	for _, layer := range below {
		if v, ok := valueAtPath(layer.spec, path); ok {
			value, hasValue = v, true
			_, human := formatFieldValue(fd, v)
			now = fmt.Sprintf("%s: %s (%s)", note.label, human, layer.name)
			break
		}
	}
	lines := []string{documentCodec.key(fd) + ":"}
	if hasValue {
		var err error
		if lines, err = fieldYAML(fd, value, true); err != nil {
			return err
		}
	}
	text := valueForm(fd) + ". " + now
	if note, ok := fieldNotes[fd.FullName()]; ok {
		text = note + " Values: " + text
	}
	text = tui.EscapeLine(text)
	first := strings.Repeat(" ", indent) + "# " + lines[0]
	if len(first)+len("  # ")+len(text) <= templateWidth {
		w.lines = append(w.lines, first+"  # "+text)
	} else {
		for _, line := range wrapWords(text, templateWidth-indent-len("# ")) {
			w.comment(indent, line)
		}
		w.lines = append(w.lines, first)
	}
	for _, line := range lines[1:] {
		w.comment(indent, line)
	}
	return nil
}

// wrapWords splits text into lines of at most width bytes, at spaces. A word
// longer than width gets a line of its own.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// overrideExample writes a commented policy override after the base, for a
// policy that has none. The generated setters build it, so a field that the
// contract removes fails the build here.
func (w *templateWriter) overrideExample() error {
	selector := &messagescontroltowerv1.LabelSelector{}
	selector.SetMatchLabels(map[string]string{"custom/team": "payments"})
	cooldown := &packageadmissionv1.Cooldown{}
	cooldown.SetMinAge(durationpb.New(placeholderDuration))
	admission := &packageadmissionv1.PackageAdmission{}
	admission.SetCooldown(cooldown)
	spec := &specv1.PolicySpec{}
	spec.SetPackageAdmission(admission)
	o := &policyv1.Override{}
	o.SetName("payments")
	o.SetDescription("A longer cooldown for the payments team")
	o.SetPriority(100)
	o.SetSelector(selector)
	o.SetSpec(spec)
	example := &policyv1.Policy{}
	example.SetOverrides([]*policyv1.Override{o})

	ex := &templateWriter{example: true}
	if err := ex.messageFields(example.ProtoReflect(), 0); err != nil {
		return err
	}
	w.lines = append(w.lines,
		"",
		"# An example policy override. It applies to the subjects that its selector matches.",
		"# Remove the \"# \" at the start of each line to use it, then change the values.",
	)
	for _, line := range ex.lines {
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

// writtenComments reads only the lines of written that start with "#", so a
// "#" in a value, as in a description, never counts. A line that starts with
// "#" in a block scalar can count as a comment: then the check misses only a
// user comment with the same text, and the save stays correct.
func writtenComments(written []byte) map[string]struct{} {
	known := map[string]struct{}{}
	for _, line := range strings.Split(string(written), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
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

// fieldYAML writes one field as the codec does, as "key: value" lines. With
// days, a duration is in days when that keeps the value, as 7d for 168h. The
// codec reads both forms.
func fieldYAML(fd protoreflect.FieldDescriptor, v protoreflect.Value, days bool) ([]string, error) {
	key := documentCodec.key(fd)
	if days && !fd.IsList() && !fd.IsMap() && isMessageKind(fd) && fd.Message().FullName() == durationFullName {
		if d, ok := v.Message().Interface().(*durationpb.Duration); ok {
			text := humanDuration(d.AsDuration())
			if parsed, err := duration.Parse(text); err != nil || parsed != d.AsDuration() {
				text = duration.Format(d.AsDuration())
			}
			return []string{key + ": " + text}, nil
		}
	}
	node := &yaml.Node{Kind: yaml.MappingNode}
	node.Content = append(node.Content, scalarNode(key), documentCodec.encodeField(fd, v))
	text, err := marshalNode(node, "")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(text), "\n"), "\n"), nil
}

// placeholderValue is an example value of a field that no layer sets: the
// first value of an enum, 7d for a duration, or the zero value of a scalar.
// Other fields get no example value.
func placeholderValue(fd protoreflect.FieldDescriptor) (protoreflect.Value, bool) {
	if fd.IsList() || fd.IsMap() {
		return protoreflect.Value{}, false
	}
	switch fd.Kind() {
	case protoreflect.EnumKind:
		for i := 0; i < fd.Enum().Values().Len(); i++ {
			if v := fd.Enum().Values().Get(i); v.Number() != 0 {
				return protoreflect.ValueOfEnum(v.Number()), true
			}
		}
		return protoreflect.Value{}, false
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if fd.Message().FullName() == durationFullName {
			return protoreflect.ValueOfMessage(durationpb.New(placeholderDuration).ProtoReflect()), true
		}
		return protoreflect.Value{}, false
	default:
		return fd.Default(), true
	}
}

// valueForm describes the values that a field accepts, in the words of the
// codec.
func valueForm(fd protoreflect.FieldDescriptor) string {
	if isListSetting(fd) {
		return "{" + documentCodec.key(wrappedList(fd.Message())) + ": [text, ...]}"
	}
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

// PruneEmpty returns a copy of m with every message field that holds no
// field cleared, at every level. A label selector, a duration, a time and a
// list setting stay, because their empty value means something: every
// subject, zero, or a list with no items.
// An edit compares pruned messages, so a parent key that a person uncomments
// with no field below it is no change.
func PruneEmpty(m proto.Message) proto.Message {
	out := proto.Clone(m)
	pruneMessage(out.ProtoReflect())
	return out
}

func pruneMessage(m protoreflect.Message) {
	var empty []protoreflect.FieldDescriptor
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case !isMessageKind(fd) || fd.IsMap() || isListSetting(fd):
		case fd.IsList():
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				pruneMessage(list.Get(i).Message())
			}
		case !isFormattedMessage(fd.Message()):
			child := v.Message()
			pruneMessage(child)
			if proto.Size(child.Interface()) == 0 {
				empty = append(empty, fd)
			}
		}
		return true
	})
	for _, fd := range empty {
		m.Clear(fd)
	}
}
