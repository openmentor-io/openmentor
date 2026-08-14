package middleware

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/price"
)

// astralRune is ONE rune written the most expensive way JSON allows: an
// astral-plane character as a `\uXXXX\uXXXX` surrogate pair, 12 wire bytes for
// a single rune.
//
// Twelve is the number that makes body caps hard to size by eye.
// go-playground/validator counts RUNES for `max=` on a string
// (utf8.RuneCountInString), not bytes, so `max=5000` bounds a field at 60,000
// wire bytes. A browser sends raw UTF-8 and never gets near that; a hand-written
// client does it for free, and the cap has to hold either way.
const astralRune = `\ud83d\ude00`

// worstCaseJSON renders the largest JSON object that can still bind into proto:
// every string field filled to its `max=` rune budget in the most expensive
// encoding its tags allow.
//
// overrides supplies the (pre-escaped, unquoted) value for a json key whose
// worst case is not derivable from tags — today only the base64 image, which is
// deliberately capped by the body limit rather than by a tag. Any other
// unbounded string fails the test rather than quietly dropping out of the
// arithmetic: a field with no size cannot be budgeted for.
func worstCaseJSON(t *testing.T, proto any, overrides map[string]string) string {
	t.Helper()
	return worstCaseObject(t, reflect.TypeOf(proto), overrides)
}

func worstCaseObject(t *testing.T, typ reflect.Type, overrides map[string]string) string {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("worstCaseJSON: %s is not a struct", typ)
	}

	fields := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		key := jsonKey(field)
		if key == "" || key == "-" {
			continue
		}
		fields = append(fields, `"`+key+`":`+worstCaseValue(t, field.Type, field.Tag.Get("binding"), key, overrides))
	}
	return "{" + strings.Join(fields, ",") + "}"
}

func worstCaseValue(t *testing.T, typ reflect.Type, binding, key string, overrides map[string]string) string {
	t.Helper()

	if value, ok := overrides[key]; ok {
		return `"` + value + `"`
	}

	switch typ.Kind() {
	case reflect.Struct:
		return worstCaseObject(t, typ, overrides)
	case reflect.Slice:
		sliceTags, elemTags := splitDive(binding)
		count, ok := tagInt(sliceTags, "max")
		if !ok {
			t.Fatalf("field %q: the slice has no max=, so the body it can carry is unbounded", key)
		}
		elems := make([]string, count)
		for i := range elems {
			elems[i] = worstCaseValue(t, typ.Elem(), strings.Join(elemTags, ","), key, nil)
		}
		return "[" + strings.Join(elems, ",") + "]"
	case reflect.String:
		return `"` + worstCaseString(t, strings.Split(binding, ","), key) + `"`
	default:
		t.Fatalf("field %q: worstCaseJSON does not model %s values", key, typ.Kind())
		return ""
	}
}

func worstCaseString(t *testing.T, tags []string, key string) string {
	t.Helper()

	// oneof bounds the field by enumeration, so the longest option IS the worst
	// case however big any max= next to it says.
	if longest, ok := longestOneOf(tags); ok {
		return longest
	}

	// The price tag bounds the field by grammar for the same reason: the
	// longest value it accepts is "Negotiable", however big the max= next to it
	// says. Derived rather than spelled out so a future spelling that is longer
	// moves this worst case with it.
	if hasTag(tags, "price") {
		return price.Value{Kind: price.Negotiable}.String()
	}

	maxRunes, ok := tagInt(tags, "max")
	if !ok {
		t.Fatalf("field %q has no max= and no override: its worst case is unbounded, so no body cap can be sized against it", key)
	}

	switch {
	case hasTag(tags, "email"):
		// The email tag rejects astral characters, so this field's worst case is
		// ASCII: one byte per rune, not twelve.
		return asciiEmail(maxRunes)
	case hasTag(tags, "https_url"):
		// safeurl.IsHTTPS only bans quotes, angle brackets and whitespace — the
		// path may be astral, so all but the origin costs 12 bytes a rune.
		const origin = "https://e.io/"
		return origin + strings.Repeat(astralRune, maxRunes-len(origin))
	default:
		return strings.Repeat(astralRune, maxRunes)
	}
}

// asciiEmail returns a syntactically valid address of exactly n characters.
func asciiEmail(n int) string {
	const suffix = ".com"
	local := 64 // the longest local part a mail server has to accept
	if limit := n - len(suffix) - 2; local > limit {
		local = limit
	}
	domain := n - local - len("@") - len(suffix)
	return strings.Repeat("a", local) + "@" + strings.Repeat("b", domain) + suffix
}

// splitDive separates the tags that bound a slice from the tags that bound its
// elements: `required,min=1,max=5,dive,max=50` is 5 elements of 50 runes.
func splitDive(binding string) (sliceTags, elemTags []string) {
	parts := strings.Split(binding, ",")
	for i, part := range parts {
		if part == "dive" {
			return parts[:i], parts[i+1:]
		}
	}
	return parts, nil
}

func tagInt(tags []string, name string) (int, bool) {
	for _, tag := range tags {
		raw, ok := strings.CutPrefix(tag, name+"=")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(raw); err == nil {
			return n, true
		}
	}
	return 0, false
}

func longestOneOf(tags []string) (string, bool) {
	for _, tag := range tags {
		raw, ok := strings.CutPrefix(tag, "oneof=")
		if !ok {
			continue
		}
		longest := ""
		for _, option := range strings.Fields(raw) {
			if len(option) > len(longest) {
				longest = option
			}
		}
		return longest, true
	}
	return "", false
}

func hasTag(tags []string, name string) bool {
	for _, tag := range tags {
		if tag == name {
			return true
		}
	}
	return false
}

func jsonKey(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return field.Name
	}
	return name
}
