package wiki

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Grounded answering: the prompt that constrains the model to the retrieved
// passages, and the parser that checks it obeyed.
//
// The parser matters as much as the prompt. Instructing a model to cite its
// sources produces citations most of the time, and "most of the time" is not a
// property anyone can rely on — so what comes back is verified against the
// passages that were actually supplied, and an answer citing a source that was
// never given is reported as ungrounded rather than shown as if it were.

// maxSourcesInPrompt bounds how many passages are put in front of the model.
//
// Five, not more, and the reason is measured rather than aesthetic: passages
// are up to 1200 characters, Turkish runs about 2.5 characters per token, so
// five fills roughly 2400 tokens. Against this deployment's 4096-token budget
// that leaves room for the question, the instructions and the answer. A sixth
// passage would start pushing the earliest one out of the model's attention
// while still costing us the tokens.
const maxSourcesInPrompt = 5

// Source is a passage offered to the model, numbered as the answer must cite it.
type Source struct {
	N            int    `json:"n"`
	DocumentSlug string `json:"document_slug"`
	Title        string `json:"title"`
	SourceURL    string `json:"source_url"`
	Heading      string `json:"heading"`
	Body         string `json:"body"`
	// Cited records whether the answer actually referred to this passage. Shown
	// in the UI so a reader can see which of the retrieved material was used and
	// which was merely nearby.
	Cited bool `json:"cited"`
}

// Answer is a grounded reply.
type Answer struct {
	Query   string   `json:"query"`
	Text    string   `json:"text"`
	Sources []Source `json:"sources"`
	// Grounded is false when the answer cited nothing that was supplied. The UI
	// must not present an ungrounded answer as a knowledge-base result — it is
	// the model talking about the world, which is precisely what this feature
	// exists to avoid.
	Grounded bool `json:"grounded"`
	// NoResults distinguishes "the knowledge base does not cover this" from
	// "the model failed". The first is a legitimate, useful answer; the second
	// is a fault. Collapsing them teaches users to distrust both.
	NoResults bool   `json:"no_results"`
	Model     string `json:"model"`
	LatencyMs int    `json:"latency_ms"`
}

// SystemPrompt instructs the model to answer only from what it was given.
//
// Written in Turkish because the corpus and the users are Turkish, and a model
// this small answers in the language it was addressed in far more reliably than
// it follows an instruction to switch.
const SystemPrompt = `Sen bir bilgi tabanı asistanısın. Görevin, SADECE sana verilen kaynak
parçalarına dayanarak soruyu yanıtlamaktır.

Kurallar:
1. Yalnızca aşağıdaki kaynaklarda yazanları kullan. Kendi genel bilgini KULLANMA.
2. Her iddianın sonuna kullandığın kaynağın numarasını köşeli parantezle ekle: [1], [2].
   Birden fazla kaynak kullandıysan hepsini yaz: [1][3]
3. Cevap kaynaklarda yoksa, uydurma. Tam olarak şunu yaz:
   "Bu soru bilgi tabanındaki belgelerde geçmiyor."
4. Kaynakları özetlerken kendi yorumunu ekleme.
5. Türkçe yanıt ver ve kısa tut: en fazla altı cümle.`

// BuildPrompt renders the question and the retrieved passages.
//
// The passages are fenced with markers rather than quotes for the same reason
// the analysis prompt is: ingested text is full of quotation marks, and a model
// that copies one out of a quoted block terminates the block early and takes
// the rest of the document with it.
func BuildPrompt(query string, sources []Source) string {
	var b strings.Builder
	b.WriteString("SORU: ")
	b.WriteString(query)
	b.WriteString("\n\nKAYNAKLAR:\n")

	for _, s := range sources {
		fmt.Fprintf(&b, "\n[%d] %s", s.N, s.Title)
		if s.Heading != "" {
			fmt.Fprintf(&b, " — %s", s.Heading)
		}
		b.WriteString("\n<<<\n")
		b.WriteString(s.Body)
		b.WriteString("\n>>>\n")
	}

	b.WriteString("\nYanıtın (sadece yukarıdaki kaynaklara dayanarak, her iddiada [n] ile):\n")
	return b.String()
}

// SourcesFrom numbers the retrieved hits for the prompt.
func SourcesFrom(hits []Hit) []Source {
	if len(hits) > maxSourcesInPrompt {
		hits = hits[:maxSourcesInPrompt]
	}
	out := make([]Source, 0, len(hits))
	for i, h := range hits {
		out = append(out, Source{
			N:            i + 1,
			DocumentSlug: h.DocumentSlug,
			Title:        h.Title,
			SourceURL:    h.SourceURL,
			Heading:      h.Heading,
			Body:         h.Body,
		})
	}
	return out
}

// citationRE matches [1] and the [1][2] runs models tend to produce.
var citationRE = regexp.MustCompile(`\[(\d{1,2})\]`)

// notFoundPhrases are the ways the model says the answer is not in the corpus.
// Matched loosely because the exact sentence in the prompt is a target, not a
// guarantee — the point is to recognise a refusal, not to grade its wording.
var notFoundPhrases = []string{
	"bilgi tabanındaki belgelerde geçmiyor",
	"belgelerde geçmiyor",
	"kaynaklarda bulunmuyor",
	"kaynaklarda yer almıyor",
}

// Ground checks an answer against the passages it was given.
//
// Returns the marked-up sources and whether the answer is grounded. Citations
// pointing at numbers that were never supplied are stripped from the text: a
// dangling [7] beside a sentence reads as evidence to anyone skimming, and it
// is the single most misleading thing a broken answer can contain.
func Ground(text string, sources []Source) (string, []Source, bool) {
	cited := map[int]bool{}
	for _, m := range citationRE.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil {
			cited[n] = true
		}
	}

	valid := map[int]bool{}
	for i := range sources {
		if cited[sources[i].N] {
			sources[i].Cited = true
			valid[sources[i].N] = true
		}
	}

	// Drop citations that refer to nothing.
	cleaned := citationRE.ReplaceAllStringFunc(text, func(s string) string {
		n, err := strconv.Atoi(strings.Trim(s, "[]"))
		if err != nil || !valid[n] {
			return ""
		}
		return s
	})
	cleaned = strings.TrimSpace(collapseSpaces(cleaned))

	lower := strings.ToLower(cleaned)
	for _, p := range notFoundPhrases {
		if strings.Contains(lower, p) {
			// A refusal is a correct, grounded outcome — the model was asked
			// not to invent and did not. Marking it ungrounded would flag the
			// one behaviour worth encouraging.
			return cleaned, sources, true
		}
	}

	return cleaned, sources, len(valid) > 0
}

// OrderSourcesByCitation puts the passages the answer used first, keeping the
// numbering stable so the [n] markers in the text still resolve.
func OrderSourcesByCitation(sources []Source) []Source {
	out := append([]Source(nil), sources...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cited != out[j].Cited {
			return out[i].Cited
		}
		return out[i].N < out[j].N
	})
	return out
}

// collapseSpaces tidies the double spaces left where a citation was removed.
var multiSpace = regexp.MustCompile(`[ \t]{2,}`)

func collapseSpaces(s string) string {
	s = multiSpace.ReplaceAllString(s, " ")
	return strings.ReplaceAll(s, " .", ".")
}
