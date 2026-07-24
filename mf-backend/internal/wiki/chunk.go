// Package wiki is DeepKwiki: an ingested knowledge base, lexical retrieval over
// it, and grounded answers that cite the passages they came from.
//
// The design constraint that shapes everything here is the same one behind the
// analysis engine: a claim the reader cannot check against a source is worse
// than no claim. So retrieval returns verbatim passages, answers carry the
// passages they used, and a question with no supporting passage is answered
// with "not in the knowledge base" rather than with the model's own recall.
package wiki

import (
	"strings"
	"unicode"
)

// Chunk sizing.
//
// These are character counts, not token counts, because the splitter runs
// before anything model-specific and Turkish tokenises poorly enough (~2.5
// characters per token here) that a token budget computed at this layer would
// be wrong for any other language.
//
// maxChunkChars is set so that the retrieved passages for one question — five
// of them by default — fit comfortably inside a 2B model's context alongside
// the question and the instructions, with room for the answer.
const (
	maxChunkChars = 1200
	// A chunk below this is folded into its neighbour. Isolated fragments
	// ("Sonuç.", a stray table row) rank badly and read worse: retrieved on
	// their own they carry no information a reader can act on.
	minChunkChars = 200
)

// Chunk is one retrievable passage.
type Chunk struct {
	Ordinal int    `json:"ordinal"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// Split turns a document into retrievable passages.
//
// Structure first, size second. The split follows Markdown headings where the
// document has them, because a heading is the author's own statement about
// where one idea ends and the next begins — better evidence than any character
// count. Size is only used to break up sections that are too long to retrieve
// usefully, and there the break goes on a paragraph boundary.
//
// The nearest heading is carried onto every chunk beneath it, so a passage
// retrieved in isolation still says what it is about.
func Split(body string) []Chunk {
	lines := strings.Split(normaliseNewlines(body), "\n")

	var (
		out     []Chunk
		heading string
		buf     []string
	)

	flush := func() {
		text := strings.TrimSpace(strings.Join(buf, "\n"))
		buf = buf[:0]
		if text == "" {
			return
		}
		for _, part := range splitBySize(text) {
			out = append(out, Chunk{Heading: heading, Body: part})
		}
	}

	for _, line := range lines {
		if h, ok := markdownHeading(line); ok {
			// The heading ends the previous section, and is not carried into
			// the body of the next one — it already lives in the Heading field,
			// and repeating it would double its weight in the index.
			flush()
			heading = h
			continue
		}
		buf = append(buf, line)
	}
	flush()

	out = mergeRunts(out)
	for i := range out {
		out[i].Ordinal = i + 1
	}
	return out
}

// markdownHeading recognises both ATX headings (`## Title`) and the all-caps
// short lines that pasted documents use instead. The second form is why this is
// not a regexp over `#`: ingested material is mostly pasted from decks and PDFs,
// where headings survive as bare capitalised lines and nothing else.
func markdownHeading(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" {
		return "", false
	}

	if strings.HasPrefix(t, "#") {
		h := strings.TrimSpace(strings.TrimLeft(t, "#"))
		if h != "" {
			return h, true
		}
		return "", false
	}

	// A short line, no terminal punctuation, and no lower-case letters. The
	// last condition is what keeps ordinary short sentences out; requiring the
	// absence of a full stop alone would promote every list item.
	if len(t) > 80 || strings.HasSuffix(t, ".") || strings.HasSuffix(t, ":") {
		return "", false
	}
	hasUpper, hasLower := false, false
	for _, r := range t {
		if unicode.IsLower(r) {
			hasLower = true
			break
		}
		if unicode.IsUpper(r) {
			hasUpper = true
		}
	}
	if hasUpper && !hasLower {
		return t, true
	}
	return "", false
}

// splitBySize breaks an over-long section on paragraph boundaries.
//
// Paragraphs are never split mid-sentence unless one paragraph is on its own
// longer than the limit, which in practice means a wall-of-text paste. Cutting
// a sentence in half produces a passage that cannot be quoted, and quoting is
// the point.
func splitBySize(text string) []string {
	if len(text) <= maxChunkChars {
		return []string{text}
	}

	paras := strings.Split(text, "\n\n")
	var out []string
	var cur strings.Builder

	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if cur.Len() > 0 && cur.Len()+len(p)+2 > maxChunkChars {
			out = append(out, cur.String())
			cur.Reset()
		}
		if len(p) > maxChunkChars {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			out = append(out, splitLongParagraph(p)...)
			continue
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// splitLongParagraph is the last resort: a single paragraph over the limit.
// Breaks on sentence ends where it can, and only mid-sentence when a single
// sentence exceeds the limit on its own.
func splitLongParagraph(p string) []string {
	var out []string
	var cur strings.Builder

	for _, s := range splitSentences(p) {
		if cur.Len() > 0 && cur.Len()+len(s)+1 > maxChunkChars {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		for len(s) > maxChunkChars {
			// Cut on a rune boundary, not a byte one: Turkish text is full of
			// multi-byte characters and slicing a byte index through one
			// produces invalid UTF-8 that Postgres rejects on insert.
			cut := safeCut(s, maxChunkChars)
			out = append(out, s[:cut])
			s = s[cut:]
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(s)
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

func splitSentences(p string) []string {
	var out []string
	start := 0
	runes := []rune(p)
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
			continue // a decimal point or an abbreviation, not a sentence end
		}
		s := strings.TrimSpace(string(runes[start : i+1]))
		if s != "" {
			out = append(out, s)
		}
		start = i + 1
	}
	if s := strings.TrimSpace(string(runes[start:])); s != "" {
		out = append(out, s)
	}
	return out
}

// safeCut returns the largest byte offset <= limit that lands on a rune
// boundary.
func safeCut(s string, limit int) int {
	if limit >= len(s) {
		return len(s)
	}
	for limit > 0 && !utf8Start(s[limit]) {
		limit--
	}
	return limit
}

// utf8Start reports whether b begins a UTF-8 sequence: ASCII, or a leading byte
// (continuation bytes are 10xxxxxx).
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// mergeRunts folds undersized chunks into their neighbour.
//
// Merges forward into the *previous* chunk when they share a heading, because
// a fragment is almost always the tail of the thought above it rather than the
// head of the one below.
func mergeRunts(in []Chunk) []Chunk {
	if len(in) < 2 {
		return in
	}
	out := make([]Chunk, 0, len(in))
	for _, c := range in {
		n := len(out)
		if n > 0 &&
			len(c.Body) < minChunkChars &&
			out[n-1].Heading == c.Heading &&
			len(out[n-1].Body)+len(c.Body)+2 <= maxChunkChars {
			out[n-1].Body += "\n\n" + c.Body
			continue
		}
		out = append(out, c)
	}
	return out
}

func normaliseNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
