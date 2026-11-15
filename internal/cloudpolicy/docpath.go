package cloudpolicy

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	policyv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/services/controltower/policy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// selectorKeys are the document keys of the label selector fields. The
// document writes a selector in the Kubernetes form.
var selectorKeys = map[protoreflect.Name]string{
	"match_labels":      "matchLabels",
	"match_expressions": "matchExpressions",
}

// DocumentPath maps a dotted field path of a Policy, with proto field names
// as a server error gives it, to the keys of the edit document, as
// "overrides[0].selector.matchLabels". Indexes and map keys stay as they
// are. It also reports whether the field is a duration. A path that does
// not parse, or a part below a name that the Policy does not have, stays as
// it is, so a person still sees what the server sent.
func DocumentPath(path string) (string, bool) {
	segments, ok := splitFieldPath(path)
	if !ok {
		return path, false
	}
	md := (&policyv1.Policy{}).ProtoReflect().Descriptor()
	var last protoreflect.FieldDescriptor
	for i, seg := range segments {
		if md == nil {
			last = nil
			continue
		}
		fd := md.Fields().ByName(protoreflect.Name(seg.name))
		if fd == nil {
			md, last = nil, nil
			continue
		}
		key, isSelector := selectorKeys[fd.Name()]
		if !isSelector || md.FullName() != labelSelectorFullName {
			key = documentCodec.key(fd)
		}
		segments[i].name = key
		last, md = fd, nextMessage(fd, seg.subscript != "")
	}
	return joinFieldPath(segments), last != nil && last.Message() != nil &&
		last.Message().FullName() == durationFullName
}

// nextMessage is the message that the next part of a path names.
func nextMessage(fd protoreflect.FieldDescriptor, subscripted bool) protoreflect.MessageDescriptor {
	switch {
	case fd.IsMap():
		if subscripted {
			return fd.MapValue().Message()
		}
		return nil
	case fd.Message() != nil && fd.Message().FullName() != durationFullName &&
		fd.Message().FullName() != timestampFullName:
		return fd.Message()
	}
	return nil
}

type pathSegment struct {
	name      string
	subscript string
}

// splitFieldPath splits "a.b[0].c[\"k.x\"]" into its names and subscripts.
// A quoted map key can hold a dot or a bracket.
func splitFieldPath(path string) ([]pathSegment, bool) {
	var segments []pathSegment
	for i := 0; i < len(path); {
		end := strings.IndexAny(path[i:], ".[")
		if end < 0 {
			end = len(path) - i
		}
		seg := pathSegment{name: path[i : i+end]}
		if seg.name == "" {
			return nil, false
		}
		i += end
		for i < len(path) && path[i] == '[' {
			n, ok := subscriptLen(path[i:])
			if !ok {
				return nil, false
			}
			seg.subscript += path[i : i+n]
			i += n
		}
		segments = append(segments, seg)
		if i < len(path) {
			if path[i] != '.' || i == len(path)-1 {
				return nil, false
			}
			i++
		}
	}
	return segments, len(segments) > 0
}

// subscriptLen is the length of the subscript at the start of s, brackets
// included.
func subscriptLen(s string) (int, bool) {
	if len(s) > 1 && s[1] == '"' {
		for j := 2; j < len(s); j++ {
			switch s[j] {
			case '\\':
				j++
			case '"':
				if j+1 < len(s) && s[j+1] == ']' {
					return j + 2, true
				}
				return 0, false
			}
		}
		return 0, false
	}
	end := strings.IndexByte(s, ']')
	return end + 1, end > 1
}

func joinFieldPath(segments []pathSegment) string {
	parts := make([]string, len(segments))
	for i, seg := range segments {
		parts[i] = seg.name + seg.subscript
	}
	return strings.Join(parts, ".")
}

// wholeSeconds matches a protovalidate duration in seconds, as "31536000s".
// RE2 has no lookbehind, so the first group captures the character before
// the number. It excludes a word character and a dot, so the fraction of
// "0.604800s" does not match.
var wholeSeconds = regexp.MustCompile(`(^|[^\w.])([0-9]+)s\b`)

// DocumentDurations writes each duration in text that is a whole number of
// days in the form of the document notes, as "31536000s" to "365d".
// protovalidate writes a duration limit in seconds, which a person cannot
// compare with the "from 0s to 365d" note of the file.
func DocumentDurations(text string) string {
	return wholeSeconds.ReplaceAllStringFunc(text, func(m string) string {
		parts := wholeSeconds.FindStringSubmatch(m)
		n, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || n == 0 || n%(24*60*60) != 0 || n > int64(time.Duration(1<<62)/time.Second) {
			return m
		}
		return parts[1] + humanDuration(time.Duration(n)*time.Second)
	})
}
