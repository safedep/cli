package cloudpolicy

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	yaml "go.yaml.in/yaml/v3"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/duration"
	"github.com/safedep/cli/internal/tui"
)

// The YAML form walks the proto descriptor, so a new contract field works
// after an SDK bump with no code change here. Three message types get a
// form for people: a duration ("168h"), a timestamp (RFC 3339) and a label
// selector (the Kubernetes form, with matchLabels and matchExpressions).

const (
	durationFullName      = "google.protobuf.Duration"
	timestampFullName     = "google.protobuf.Timestamp"
	labelSelectorFullName = "safedep.messages.controltower.v1.LabelSelector"
)

// selectorOperators maps the LabelSelectorOperator numbers to the Kubernetes
// operator names.
var selectorOperators = map[protoreflect.EnumNumber]string{
	1: "In",
	2: "NotIn",
	3: "Exists",
	4: "DoesNotExist",
}

// yamlCodec encodes and decodes proto messages as YAML nodes. skip names the
// fields that the document leaves out, by full name, with the reason that a
// decode gives when a document sets one. names gives a field another key in
// the document, by full name.
type yamlCodec struct {
	skip  map[protoreflect.FullName]string
	names map[protoreflect.FullName]string
}

// key is the document key of a field.
func (c *yamlCodec) key(fd protoreflect.FieldDescriptor) string {
	if name, ok := c.names[fd.FullName()]; ok {
		return name
	}
	return string(fd.Name())
}

// fieldByKey finds the field of a document key. It also accepts the proto
// name and the JSON name of a field.
func (c *yamlCodec) fieldByKey(md protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		if c.key(fields.Get(i)) == key {
			return fields.Get(i)
		}
	}
	if fd := fields.ByName(protoreflect.Name(key)); fd != nil {
		return fd
	}
	return fields.ByJSONName(key)
}

func (c *yamlCodec) encodeMessage(m protoreflect.Message) *yaml.Node {
	switch m.Descriptor().FullName() {
	case durationFullName, timestampFullName:
		return scalarNode(c.formatWellKnown(m))
	case labelSelectorFullName:
		return encodeSelector(m)
	}

	out := &yaml.Node{Kind: yaml.MappingNode}
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if _, skipped := c.skip[fd.FullName()]; skipped || !m.Has(fd) {
			continue
		}
		out.Content = append(out.Content, scalarNode(c.key(fd)), c.encodeField(fd, m.Get(fd)))
	}
	return out
}

func (c *yamlCodec) encodeField(fd protoreflect.FieldDescriptor, v protoreflect.Value) *yaml.Node {
	switch {
	case fd.IsList():
		list := v.List()
		out := &yaml.Node{Kind: yaml.SequenceNode}
		for i := 0; i < list.Len(); i++ {
			out.Content = append(out.Content, c.encodeSingular(fd, list.Get(i)))
		}
		return out
	case fd.IsMap():
		entries := v.Map()
		keys := make([]protoreflect.MapKey, 0, entries.Len())
		entries.Range(func(k protoreflect.MapKey, _ protoreflect.Value) bool {
			keys = append(keys, k)
			return true
		})
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		out := &yaml.Node{Kind: yaml.MappingNode}
		for _, k := range keys {
			out.Content = append(out.Content, scalarNode(k.String()), c.encodeSingular(fd.MapValue(), entries.Get(k)))
		}
		return out
	default:
		return c.encodeSingular(fd, v)
	}
}

func (c *yamlCodec) encodeSingular(fd protoreflect.FieldDescriptor, v protoreflect.Value) *yaml.Node {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return c.encodeMessage(v.Message())
	case protoreflect.EnumKind:
		return scalarNode(enumValueToken(fd.Enum(), v.Enum()))
	case protoreflect.BytesKind:
		return scalarNode(base64.StdEncoding.EncodeToString(v.Bytes()))
	case protoreflect.StringKind:
		return stringNode(v.String())
	default:
		return scalarNode(v.String())
	}
}

func (c *yamlCodec) formatWellKnown(m protoreflect.Message) string {
	switch msg := m.Interface().(type) {
	case *durationpb.Duration:
		return duration.Format(msg.AsDuration())
	case *timestamppb.Timestamp:
		return msg.AsTime().UTC().Format(time.RFC3339)
	default:
		return ""
	}
}

func encodeSelector(m protoreflect.Message) *yaml.Node {
	fields := m.Descriptor().Fields()
	out := &yaml.Node{Kind: yaml.MappingNode}

	if labels := m.Get(fields.ByName("match_labels")).Map(); labels.Len() > 0 {
		keys := make([]string, 0, labels.Len())
		labels.Range(func(k protoreflect.MapKey, _ protoreflect.Value) bool {
			keys = append(keys, k.String())
			return true
		})
		sort.Strings(keys)
		node := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		for _, k := range keys {
			v := labels.Get(protoreflect.ValueOfString(k).MapKey())
			node.Content = append(node.Content, scalarNode(k), stringNode(v.String()))
		}
		out.Content = append(out.Content, scalarNode("matchLabels"), node)
	}

	if exprs := m.Get(fields.ByName("match_expressions")).List(); exprs.Len() > 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for i := 0; i < exprs.Len(); i++ {
			expr := exprs.Get(i).Message()
			efs := expr.Descriptor().Fields()
			item := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
			item.Content = append(item.Content,
				scalarNode("key"), stringNode(expr.Get(efs.ByName("key")).String()),
				scalarNode("operator"), scalarNode(selectorOperators[expr.Get(efs.ByName("operator")).Enum()]),
			)
			if values := expr.Get(efs.ByName("values")).List(); values.Len() > 0 {
				valueSeq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
				for j := 0; j < values.Len(); j++ {
					valueSeq.Content = append(valueSeq.Content, stringNode(values.Get(j).String()))
				}
				item.Content = append(item.Content, scalarNode("values"), valueSeq)
			}
			seq.Content = append(seq.Content, item)
		}
		out.Content = append(out.Content, scalarNode("matchExpressions"), seq)
	}
	if len(out.Content) == 0 {
		out.Style = yaml.FlowStyle
	}
	return out
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

// stringNode tags the value as a string, so a value such as "true" or "300"
// keeps its type through a round trip.
func stringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// enumPrefix is the prefix that every value of a SafeDep enum shares, taken
// from its zero value, as "SEVERITY_" from "SEVERITY_UNSPECIFIED".
func enumPrefix(ed protoreflect.EnumDescriptor) string {
	zero := ed.Values().ByNumber(0)
	if zero == nil {
		return ""
	}
	return strings.TrimSuffix(string(zero.Name()), "UNSPECIFIED")
}

// enumValueToken writes an enum value as a short token, as "medium" for
// SEVERITY_MEDIUM. A number that the SDK does not know stays a number.
func enumValueToken(ed protoreflect.EnumDescriptor, n protoreflect.EnumNumber) string {
	value := ed.Values().ByNumber(n)
	if value == nil {
		return strconv.Itoa(int(n))
	}
	return tui.EnumToken(string(value.Name()), enumPrefix(ed))
}

// enumTokens lists the tokens of an enum, without the zero value.
func enumTokens(ed protoreflect.EnumDescriptor) []string {
	names := make(map[int32]string, ed.Values().Len())
	for i := 0; i < ed.Values().Len(); i++ {
		v := ed.Values().Get(i)
		names[int32(v.Number())] = string(v.Name())
	}
	return tui.EnumTokens(names, enumPrefix(ed))
}

// parseEnumToken reads a token, a full value name or a number.
func parseEnumToken(ed protoreflect.EnumDescriptor, token string) (protoreflect.EnumNumber, bool) {
	text := strings.TrimSpace(token)
	prefix := enumPrefix(ed)
	for i := 0; i < ed.Values().Len(); i++ {
		v := ed.Values().Get(i)
		if v.Number() == 0 {
			continue
		}
		name := string(v.Name())
		if text == name || strings.EqualFold(text, tui.EnumToken(name, prefix)) ||
			strings.EqualFold(strings.ReplaceAll(text, "_", "-"), tui.EnumToken(name, prefix)) {
			return v.Number(), true
		}
	}
	return 0, false
}

// decodeError carries the line and the document path of a decode failure.
type decodeError struct {
	line int
	path string
	msg  string
}

func (e *decodeError) Error() string {
	if e.line > 0 {
		return fmt.Sprintf("line %d: %s: %s", e.line, e.path, e.msg)
	}
	return fmt.Sprintf("%s: %s", e.path, e.msg)
}

func errAt(n *yaml.Node, path, format string, args ...any) error {
	return &decodeError{line: n.Line, path: path, msg: fmt.Sprintf(format, args...)}
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func (c *yamlCodec) decodeMessage(n *yaml.Node, m protoreflect.Message, path string) error {
	if isNull(n) {
		return nil
	}
	switch m.Descriptor().FullName() {
	case durationFullName:
		return decodeDuration(n, m, path)
	case timestampFullName:
		return decodeTimestamp(n, m, path)
	case labelSelectorFullName:
		return decodeSelector(n, m, path)
	}
	if n.Kind != yaml.MappingNode {
		return errAt(n, path, "expected a mapping of fields")
	}

	seen := make(map[protoreflect.Name]struct{}, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		fieldPath := joinPath(path, key.Value)
		fd := c.fieldByKey(m.Descriptor(), key.Value)
		if fd == nil {
			return errAt(key, fieldPath, "unknown field. Known fields: %s", strings.Join(c.knownFields(m.Descriptor()), ", "))
		}
		if reason, skipped := c.skip[fd.FullName()]; skipped {
			return errAt(key, fieldPath, "%s", reason)
		}
		if _, dup := seen[fd.Name()]; dup {
			return errAt(key, fieldPath, "the field is set twice")
		}
		seen[fd.Name()] = struct{}{}
		if isNull(value) {
			continue
		}
		if err := c.decodeField(value, m, fd, fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func (c *yamlCodec) knownFields(md protoreflect.MessageDescriptor) []string {
	names := make([]string, 0, md.Fields().Len())
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		if _, skipped := c.skip[fd.FullName()]; skipped {
			continue
		}
		names = append(names, c.key(fd))
	}
	return names
}

func (c *yamlCodec) decodeField(n *yaml.Node, m protoreflect.Message, fd protoreflect.FieldDescriptor, path string) error {
	switch {
	case fd.IsList():
		if n.Kind != yaml.SequenceNode {
			return errAt(n, path, "expected a list")
		}
		list := m.Mutable(fd).List()
		for i, item := range n.Content {
			v, err := c.decodeSingular(item, fd, list.NewElement, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return err
			}
			list.Append(v)
		}
		return nil
	case fd.IsMap():
		if n.Kind != yaml.MappingNode {
			return errAt(n, path, "expected a mapping")
		}
		if fd.MapKey().Kind() != protoreflect.StringKind {
			return errAt(n, path, "this map field is not supported in the YAML form")
		}
		entries := m.Mutable(fd).Map()
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			v, err := c.decodeSingular(value, fd.MapValue(), entries.NewValue, joinPath(path, key.Value))
			if err != nil {
				return err
			}
			entries.Set(protoreflect.ValueOfString(key.Value).MapKey(), v)
		}
		return nil
	default:
		v, err := c.decodeSingular(n, fd, func() protoreflect.Value { return m.NewField(fd) }, path)
		if err != nil {
			return err
		}
		m.Set(fd, v)
		return nil
	}
}

func (c *yamlCodec) decodeSingular(
	n *yaml.Node,
	fd protoreflect.FieldDescriptor,
	newMessage func() protoreflect.Value,
	path string,
) (protoreflect.Value, error) {
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		v := newMessage()
		if err := c.decodeMessage(n, v.Message(), path); err != nil {
			return protoreflect.Value{}, err
		}
		return v, nil
	}
	if n.Kind != yaml.ScalarNode {
		return protoreflect.Value{}, errAt(n, path, "expected a single value")
	}
	return decodeScalar(n, fd, path)
}

func decodeScalar(n *yaml.Node, fd protoreflect.FieldDescriptor, path string) (protoreflect.Value, error) {
	text := n.Value
	switch fd.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(text), nil
	case protoreflect.BoolKind:
		b, err := strconv.ParseBool(text)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected true or false, not %q", text)
		}
		return protoreflect.ValueOfBool(b), nil
	case protoreflect.EnumKind:
		number, ok := parseEnumToken(fd.Enum(), text)
		if !ok {
			return protoreflect.Value{}, errAt(n, path, "unknown value %q. Use one of: %s",
				text, strings.Join(enumTokens(fd.Enum()), ", "))
		}
		return protoreflect.ValueOfEnum(number), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		i, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected a whole number, not %q", text)
		}
		return protoreflect.ValueOfInt32(int32(i)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected a whole number, not %q", text)
		}
		return protoreflect.ValueOfInt64(i), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		u, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected a positive whole number, not %q", text)
		}
		return protoreflect.ValueOfUint32(uint32(u)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		u, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected a positive whole number, not %q", text)
		}
		return protoreflect.ValueOfUint64(u), nil
	case protoreflect.FloatKind:
		f, err := strconv.ParseFloat(text, 32)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected a number, not %q", text)
		}
		return protoreflect.ValueOfFloat32(float32(f)), nil
	case protoreflect.DoubleKind:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected a number, not %q", text)
		}
		return protoreflect.ValueOfFloat64(f), nil
	case protoreflect.BytesKind:
		b, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return protoreflect.Value{}, errAt(n, path, "expected base64 data")
		}
		return protoreflect.ValueOfBytes(b), nil
	default:
		return protoreflect.Value{}, errAt(n, path, "this field kind is not supported in the YAML form")
	}
}

func decodeDuration(n *yaml.Node, m protoreflect.Message, path string) error {
	if n.Kind != yaml.ScalarNode {
		return errAt(n, path, "expected a duration, as in 168h or 7d")
	}
	d, err := duration.Parse(n.Value)
	if err != nil {
		return errAt(n, path, "%v", err)
	}
	pb := durationpb.New(d)
	fields := m.Descriptor().Fields()
	m.Set(fields.ByName("seconds"), protoreflect.ValueOfInt64(pb.GetSeconds()))
	m.Set(fields.ByName("nanos"), protoreflect.ValueOfInt32(pb.GetNanos()))
	return nil
}

func decodeTimestamp(n *yaml.Node, m protoreflect.Message, path string) error {
	if n.Kind != yaml.ScalarNode {
		return errAt(n, path, "expected a time, as in 2026-12-31T00:00:00Z")
	}
	t, err := ParseTime(n.Value)
	if err != nil {
		return errAt(n, path, "%v", err)
	}
	pb := timestamppb.New(t)
	fields := m.Descriptor().Fields()
	m.Set(fields.ByName("seconds"), protoreflect.ValueOfInt64(pb.GetSeconds()))
	m.Set(fields.ByName("nanos"), protoreflect.ValueOfInt32(pb.GetNanos()))
	return nil
}

// ParseTime reads an RFC 3339 time or a date, as in "2027-01-26". A date is
// midnight UTC.
func ParseTime(s string) (time.Time, error) {
	text := strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.DateOnly, text); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: use RFC 3339, as in 2027-01-26T00:00:00Z, or a date, as in 2027-01-26", s)
}

func decodeSelector(n *yaml.Node, m protoreflect.Message, path string) error {
	if n.Kind != yaml.MappingNode {
		return errAt(n, path, "expected a mapping with matchLabels or matchExpressions")
	}
	fields := m.Descriptor().Fields()
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]
		fieldPath := joinPath(path, key.Value)
		switch key.Value {
		case "matchLabels", "match_labels":
			if isNull(value) {
				continue
			}
			if value.Kind != yaml.MappingNode {
				return errAt(value, fieldPath, "expected a mapping of label keys and values")
			}
			labels := m.Mutable(fields.ByName("match_labels")).Map()
			for j := 0; j+1 < len(value.Content); j += 2 {
				k, v := value.Content[j], value.Content[j+1]
				if v.Kind != yaml.ScalarNode {
					return errAt(v, joinPath(fieldPath, k.Value), "expected a label value")
				}
				labels.Set(protoreflect.ValueOfString(k.Value).MapKey(), protoreflect.ValueOfString(v.Value))
			}
		case "matchExpressions", "match_expressions":
			if isNull(value) {
				continue
			}
			if err := decodeSelectorExpressions(value, m, fieldPath); err != nil {
				return err
			}
		default:
			return errAt(key, fieldPath, "unknown field. Known fields: matchLabels, matchExpressions")
		}
	}
	return nil
}

func decodeSelectorExpressions(n *yaml.Node, m protoreflect.Message, path string) error {
	if n.Kind != yaml.SequenceNode {
		return errAt(n, path, "expected a list of expressions")
	}
	list := m.Mutable(m.Descriptor().Fields().ByName("match_expressions")).List()
	for i, item := range n.Content {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if item.Kind != yaml.MappingNode {
			return errAt(item, itemPath, "expected a mapping with key, operator and values")
		}
		expr := list.NewElement()
		efs := expr.Message().Descriptor().Fields()
		for j := 0; j+1 < len(item.Content); j += 2 {
			k, v := item.Content[j], item.Content[j+1]
			fieldPath := joinPath(itemPath, k.Value)
			switch k.Value {
			case "key":
				expr.Message().Set(efs.ByName("key"), protoreflect.ValueOfString(v.Value))
			case "operator":
				op, ok := parseSelectorOperator(v.Value)
				if !ok {
					return errAt(v, fieldPath, "unknown operator %q. Use one of: In, NotIn, Exists, DoesNotExist", v.Value)
				}
				expr.Message().Set(efs.ByName("operator"), protoreflect.ValueOfEnum(op))
			case "values":
				if v.Kind != yaml.SequenceNode {
					return errAt(v, fieldPath, "expected a list of values")
				}
				values := expr.Message().Mutable(efs.ByName("values")).List()
				for _, value := range v.Content {
					values.Append(protoreflect.ValueOfString(value.Value))
				}
			default:
				return errAt(k, fieldPath, "unknown field. Known fields: key, operator, values")
			}
		}
		list.Append(expr)
	}
	return nil
}

func parseSelectorOperator(text string) (protoreflect.EnumNumber, bool) {
	for number, name := range selectorOperators {
		if strings.EqualFold(text, name) {
			return number, true
		}
	}
	return 0, false
}
