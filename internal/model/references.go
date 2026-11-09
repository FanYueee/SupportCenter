package model

import (
	"regexp"
	"strings"
)

// ReferenceKind is a kind of item an outside source, such as the Portal, lets
// tickets tag: its key, its names and the character that starts a tag, e.g.
// invoices tagged as $ and the invoice's id.
type ReferenceKind struct {
	Key, Label, LabelEn, Trigger string
}

// Name is the kind's name in the viewer's language.
func (k ReferenceKind) Name(lang string) string {
	if lang == "en" && k.LabelEn != "" {
		return k.LabelEn
	}
	return k.Label
}

// Reference is a tagged item as the source described it when it was tagged.
// Customers open CustomerURL and staff StaffURL. Internal ones were tagged only
// in staff notes.
type Reference struct {
	ReferenceKind
	ID, Title, Detail, CustomerURL, StaffURL string
	Internal                                 bool
}

// ReferenceList is a customer's items of one kind, as the source ranks
// them; Failed when the source could not say.
type ReferenceList struct {
	Kind   ReferenceKind
	Items  []Reference
	Failed bool
}

// Token is how a message writes the tag, e.g. "$5895aa2a-a677-…".
func (r Reference) Token() string {
	return r.Trigger + r.ID
}

// ReferenceTriggers are the characters a kind may start its tags with; # is
// taken by tickets.
const ReferenceTriggers = "$!&%@^"

var (
	ReferenceKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)
	ReferenceID  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	// A tag is a trigger and an id, not inside a word, so "US$5" stays text.
	referenceTag = regexp.MustCompile(`[$!&%@^][A-Za-z0-9][A-Za-z0-9_-]{0,63}`)
)

// ReferenceSpan is one tag found in a message, at text[Start:End].
type ReferenceSpan struct {
	Start, End  int
	Trigger, ID string
}

func referenceWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// ReferenceSpans finds the tags in text. Whether one names a real item is up
// to the source; until then it is just text.
func ReferenceSpans(text string) []ReferenceSpan {
	var spans []ReferenceSpan
	for _, m := range referenceTag.FindAllStringIndex(text, -1) {
		if m[0] > 0 && (referenceWordByte(text[m[0]-1]) || strings.IndexByte("#&/"+ReferenceTriggers, text[m[0]-1]) >= 0) {
			continue
		}
		if m[1] < len(text) && referenceWordByte(text[m[1]]) {
			continue
		}
		spans = append(spans, ReferenceSpan{Start: m[0], End: m[1], Trigger: text[m[0] : m[0]+1], ID: text[m[0]+1 : m[1]]})
	}
	return spans
}

// ReferenceText writes the known tags in text as the kind and title, e.g.
// "[帳單：2024 年 10 月月費]", for places that show plain text: emails, chat
// notifications and the AI's view of the conversation.
func ReferenceText(text string, refs []Reference) string {
	if len(refs) == 0 {
		return text
	}
	known := make(map[string]Reference, len(refs))
	for _, ref := range refs {
		known[ref.Token()] = ref
	}
	var b strings.Builder
	last := 0
	for _, span := range ReferenceSpans(text) {
		ref, ok := known[text[span.Start:span.End]]
		if !ok {
			continue
		}
		b.WriteString(text[last:span.Start])
		b.WriteString("[" + ref.Label + "：" + ref.Title + "]")
		last = span.End
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}
