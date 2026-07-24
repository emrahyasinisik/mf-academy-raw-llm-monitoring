package wiki

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitFollowsHeadings(t *testing.T) {
	doc := `# Yatırım Kriterleri

Erken aşama yatırımda ekip en ağır kriterdir.

## Rekabet

Rakip analizi olmayan sunumlar elenir.`

	got := Split(doc)
	if len(got) != 2 {
		t.Fatalf("want 2 chunks, one per section, got %d: %+v", len(got), got)
	}
	if got[0].Heading != "Yatırım Kriterleri" || got[1].Heading != "Rekabet" {
		t.Fatalf("headings not carried onto their sections: %+v", got)
	}
	// The heading must not also appear in the body, or it is weighted twice in
	// the index — once as a heading and once as ordinary text.
	if strings.Contains(got[1].Body, "Rekabet") {
		t.Errorf("heading duplicated into the body: %q", got[1].Body)
	}
	if got[0].Ordinal != 1 || got[1].Ordinal != 2 {
		t.Errorf("ordinals should be 1-based and sequential: %+v", got)
	}
}

// Pasted decks and PDFs lose their `#` markers, so an all-caps line is the only
// heading signal left. This is the common case for ingested material, not an
// edge case.
func TestSplitRecognisesUnmarkedHeadings(t *testing.T) {
	doc := "PAZAR BÜYÜKLÜĞÜ\n\n" + strings.Repeat("Türkiye pazarı hızla büyüyor. ", 12)

	got := Split(doc)
	if len(got) == 0 || got[0].Heading != "PAZAR BÜYÜKLÜĞÜ" {
		t.Fatalf("all-caps heading not recognised: %+v", got)
	}
}

// An ordinary short sentence must not be promoted to a heading, or every list
// item becomes a section boundary and the document shatters.
func TestShortSentencesAreNotHeadings(t *testing.T) {
	for _, line := range []string{
		"Ekip güçlü.",
		"Rakipler:",
		"bu küçük harfle başlıyor",
	} {
		if h, ok := markdownHeading(line); ok {
			t.Errorf("%q was treated as a heading (%q)", line, h)
		}
	}
}

func TestLongSectionSplitsOnParagraphs(t *testing.T) {
	para := strings.Repeat("Bu cümle pazar analizini anlatıyor. ", 20) // ~700 chars
	doc := "# Uzun\n\n" + para + "\n\n" + para + "\n\n" + para

	got := Split(doc)
	if len(got) < 2 {
		t.Fatalf("an over-long section should split, got %d chunks", len(got))
	}
	for _, c := range got {
		if len(c.Body) > maxChunkChars {
			t.Errorf("chunk of %d chars exceeds the %d limit", len(c.Body), maxChunkChars)
		}
		if c.Heading != "Uzun" {
			t.Errorf("every piece of a split section keeps its heading, got %q", c.Heading)
		}
	}
}

// A wall of text with no paragraph breaks is what a PDF paste looks like. It
// must still come out as valid, quotable UTF-8 — a byte-index cut through a
// Turkish character produces bytes Postgres rejects outright.
func TestWallOfTextStaysValidUTF8(t *testing.T) {
	doc := strings.Repeat("ığüşöçİĞÜŞÖÇ", 400) // no spaces, no sentence ends

	for _, c := range Split(doc) {
		if !utf8.ValidString(c.Body) {
			t.Fatalf("chunk is not valid UTF-8: %q", c.Body[:40])
		}
		if len(c.Body) > maxChunkChars {
			t.Errorf("chunk of %d chars exceeds the limit", len(c.Body))
		}
	}
}

func TestRuntsAreFoldedIntoTheirNeighbour(t *testing.T) {
	// Two paragraphs under one heading, the second far too short to retrieve
	// on its own.
	doc := "# Bölüm\n\n" + strings.Repeat("Uzun bir paragraf. ", 20) + "\n\nSonuç."

	got := Split(doc)
	for _, c := range got {
		if len(c.Body) < minChunkChars && len(got) > 1 {
			t.Errorf("a runt survived: %q", c.Body)
		}
	}
	if len(got) > 0 && !strings.Contains(got[len(got)-1].Body, "Sonuç.") {
		t.Error("the folded fragment was lost rather than merged")
	}
}

// Splitting must never drop text. A silently discarded paragraph is a citation
// that cannot be found and a fact the knowledge base claims not to have.
func TestSplitLosesNoContent(t *testing.T) {
	doc := `# Bir

Birinci paragraf burada.

## İki

İkinci paragraf burada.

### Üç

Üçüncü paragraf burada.`

	var joined strings.Builder
	for _, c := range Split(doc) {
		joined.WriteString(c.Body)
		joined.WriteString(" ")
	}
	all := joined.String()
	for _, want := range []string{"Birinci paragraf", "İkinci paragraf", "Üçüncü paragraf"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q was lost during splitting", want)
		}
	}
}

func TestEmptyDocumentProducesNoChunks(t *testing.T) {
	if got := Split("   \n\n  \n"); len(got) != 0 {
		t.Fatalf("want no chunks for an empty document, got %+v", got)
	}
}

func TestCRLFIsNormalised(t *testing.T) {
	got := Split("# Başlık\r\n\r\nGövde metni burada.\r\n")
	if len(got) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(got))
	}
	if strings.Contains(got[0].Body, "\r") {
		t.Error("carriage returns survived into the body")
	}
	if got[0].Heading != "Başlık" {
		t.Errorf("heading not parsed from a CRLF document: %q", got[0].Heading)
	}
}
