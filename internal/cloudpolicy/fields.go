package cloudpolicy

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	specv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/policy/spec/v1"
	"github.com/safedep/dry/tui/humanize"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/safedep/cli/internal/duration"
)

// coreCapability is the capability of the package rules. It is not a policy
// setting, so the field lists and the restore leave it out.
const coreCapability = "core"

// FieldValue is one set field of a PolicySpec.
type FieldValue struct {
	// Path is the contract path, as in "scan_gate.fail_on".
	Path string
	// Value is the exact value, in the form of the policy document, as
	// "168h" or "medium".
	Value string
	// Human is the value for a table, as "7d".
	Human string
}

// Fields lists the set fields of a PolicySpec, other than package rules, in
// descriptor order.
func Fields(spec *specv1.PolicySpec) []FieldValue {
	if spec == nil {
		return nil
	}
	var out []FieldValue
	collectFields(spec.ProtoReflect(), "", &out)
	return out
}

func collectFields(m protoreflect.Message, path string, out *[]FieldValue) {
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if path == "" && string(fd.Name()) == coreCapability {
			continue
		}
		if !m.Has(fd) {
			continue
		}
		fieldPath := joinPath(path, string(fd.Name()))
		if isLeaf(fd) {
			value, human := formatFieldValue(fd, m.Get(fd))
			*out = append(*out, FieldValue{Path: fieldPath, Value: value, Human: human})
			continue
		}
		collectFields(m.Get(fd).Message(), fieldPath, out)
	}
}

// LeafPaths lists every settings path of the contract that the SDK knows, in
// descriptor order, as in "scan_gate.fail_on".
func LeafPaths() []string {
	var out []string
	collectPaths((&specv1.PolicySpec{}).ProtoReflect().Descriptor(), "", &out)
	return out
}

func collectPaths(md protoreflect.MessageDescriptor, path string, out *[]string) {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if path == "" && string(fd.Name()) == coreCapability {
			continue
		}
		fieldPath := joinPath(path, string(fd.Name()))
		if isLeaf(fd) {
			*out = append(*out, fieldPath)
			continue
		}
		collectPaths(fd.Message(), fieldPath, out)
	}
}

// Capabilities lists the settings capabilities of the contract: the fields of
// PolicySpec other than the package rules, as in "scan_gate".
func Capabilities() []string {
	md := (&specv1.PolicySpec{}).ProtoReflect().Descriptor()
	out := make([]string, 0, md.Fields().Len())
	for i := 0; i < md.Fields().Len(); i++ {
		name := string(md.Fields().Get(i).Name())
		if name != coreCapability {
			out = append(out, name)
		}
	}
	return out
}

// ValidateFieldPaths checks that each path is a settings path or a
// capability of the contract, so a typo fails before the RPC.
func ValidateFieldPaths(paths []string) error {
	known := LeafPaths()
	for _, p := range paths {
		if slices.Contains(known, p) || MatchesFieldFilter(known, []string{p}) {
			continue
		}
		return fmt.Errorf("unknown policy field %q: known fields are %s", p, strings.Join(known, ", "))
	}
	return nil
}

// MatchesFieldFilter reports whether any path in paths is selected by the
// filter. A filter entry selects its own path and every path below it. An
// empty filter selects every path.
func MatchesFieldFilter(paths, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, p := range paths {
		for _, f := range filter {
			if p == f || strings.HasPrefix(p, f+".") {
				return true
			}
		}
	}
	return false
}

func isLeaf(fd protoreflect.FieldDescriptor) bool {
	if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
		return true
	}
	return isFormattedMessage(fd.Message())
}

func formatFieldValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, string) {
	switch {
	case fd.IsList():
		list := v.List()
		values := make([]string, 0, list.Len())
		humans := make([]string, 0, list.Len())
		for i := 0; i < list.Len(); i++ {
			value, human := formatSingular(fd, list.Get(i))
			values = append(values, value)
			humans = append(humans, human)
		}
		return strings.Join(values, ", "), strings.Join(humans, ", ")
	case fd.IsMap():
		var pairs []string
		v.Map().Range(func(k protoreflect.MapKey, mv protoreflect.Value) bool {
			value, _ := formatSingular(fd.MapValue(), mv)
			pairs = append(pairs, k.String()+"="+value)
			return true
		})
		slices.Sort(pairs)
		joined := strings.Join(pairs, ", ")
		return joined, joined
	default:
		return formatSingular(fd, v)
	}
}

func formatSingular(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, string) {
	switch fd.Kind() {
	case protoreflect.EnumKind:
		token := enumValueToken(fd.Enum(), v.Enum())
		return token, token
	case protoreflect.MessageKind, protoreflect.GroupKind:
		switch msg := v.Message().Interface().(type) {
		case *durationpb.Duration:
			d := msg.AsDuration()
			return duration.Format(d), humanDuration(d)
		case *timestamppb.Timestamp:
			t := msg.AsTime().UTC()
			return t.Format(time.RFC3339Nano), humanize.Time(t, time.Now())
		default:
			text := fmt.Sprint(v.Message().Interface())
			return text, text
		}
	default:
		text := v.String()
		return text, text
	}
}

// humanDuration keeps a zero duration readable as "0s". A zero cooldown
// turns the cooldown off.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return duration.Format(d)
	}
	return humanize.Duration(d)
}

// FormatJSONValue turns a value of a contract path in protojson form, as a
// preview returns it, into the document form and the table form. An empty
// raw value means that the layer did not set the field, and shows as "-".
func FormatJSONValue(path, raw string) (string, string) {
	if raw == "" {
		return "-", "-"
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw, raw
	}

	fd := fieldByPath(path)
	text, isString := value.(string)
	switch {
	case fd != nil && isString && fd.Kind() == protoreflect.EnumKind:
		if ev := fd.Enum().Values().ByName(protoreflect.Name(text)); ev != nil {
			token := enumValueToken(fd.Enum(), ev.Number())
			return token, token
		}
		return text, text
	case fd != nil && isString && fd.Kind() == protoreflect.MessageKind && fd.Message().FullName() == durationFullName:
		if d, err := time.ParseDuration(text); err == nil {
			return duration.Format(d), humanDuration(d)
		}
		return text, text
	case isString:
		return text, text
	default:
		compact, err := json.Marshal(value)
		if err != nil {
			return raw, raw
		}
		return string(compact), string(compact)
	}
}

func fieldByPath(path string) protoreflect.FieldDescriptor {
	md := (&specv1.PolicySpec{}).ProtoReflect().Descriptor()
	var fd protoreflect.FieldDescriptor
	for _, segment := range strings.Split(path, ".") {
		if md == nil {
			return nil
		}
		fd = md.Fields().ByName(protoreflect.Name(segment))
		if fd == nil {
			return nil
		}
		md = fd.Message()
	}
	return fd
}
