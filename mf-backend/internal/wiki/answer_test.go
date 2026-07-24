package wiki

import (
	"strings"
	"testing"
)

func srcs(n int) []Source {
	out := make([]Source, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, Source{N: i, Title: "Belge", Body: "gövde"})
	}
	return out
}

func TestGroundMarksTheCitedSources(t *testing.T) {
	text, sources, grounded := Ground("Ekip en ağır kriterdir [2]. Rekabet de bakılır [3].", srcs(3))

	if !grounded {
		t.Fatal("an answer citing supplied sources is grounded")
	}
	if sources[0].Cited {
		t.Error("source 1 was not cited and must not be marked")
	}
	if !sources[1].Cited || !sources[2].Cited {
		t.Error("sources 2 and 3 were cited and must be marked")
	}
	if !strings.Contains(text, "[2]") {
		t.Errorf("valid citations must survive: %q", text)
	}
}

// The most dangerous failure this parser exists to catch. A [7] next to a
// sentence reads as evidence to anyone skimming, and there is no seventh
// source — so the marker has to go, not just be ignored.
func TestDanglingCitationsAreStripped(t *testing.T) {
	text, sources, grounded := Ground("Pazar büyüyor [7].", srcs(3))

	if strings.Contains(text, "[7]") {
		t.Errorf("a citation pointing at nothing must not survive: %q", text)
	}
	if grounded {
		t.Error("an answer whose only citation was invalid is not grounded")
	}
	for _, s := range sources {
		if s.Cited {
			t.Errorf("no source should be marked cited, got %+v", s)
		}
	}
}

func TestAnswerWithNoCitationsIsUngrounded(t *testing.T) {
	_, _, grounded := Ground("Yatırımcılar genelde ekibe bakar.", srcs(3))
	if grounded {
		t.Fatal("an uncited claim is exactly what this feature exists to catch")
	}
}

// Refusing to answer is the behaviour the prompt asks for. Flagging it as
// ungrounded would mark the one response worth encouraging as a fault.
func TestARefusalCountsAsGrounded(t *testing.T) {
	_, _, grounded := Ground("Bu soru bilgi tabanındaki belgelerde geçmiyor.", srcs(3))
	if !grounded {
		t.Fatal("a refusal is a correct, grounded outcome")
	}
}

func TestStrippingLeavesReadableText(t *testing.T) {
	text, _, _ := Ground("Ekip önemli [9]. Pazar da [1].", srcs(2))
	if strings.Contains(text, "  ") {
		t.Errorf("double spaces left where a citation was removed: %q", text)
	}
	if !strings.Contains(text, "Ekip önemli.") {
		t.Errorf("want a clean sentence, got %q", text)
	}
}

func TestSourcesFromCapsWhatReachesTheModel(t *testing.T) {
	hits := make([]Hit, 9)
	for i := range hits {
		hits[i] = Hit{DocumentSlug: "d", Title: "T", Body: "b"}
	}
	got := SourcesFrom(hits)
	if len(got) != maxSourcesInPrompt {
		t.Fatalf("want at most %d sources in the prompt, got %d", maxSourcesInPrompt, len(got))
	}
	if got[0].N != 1 || got[len(got)-1].N != maxSourcesInPrompt {
		t.Errorf("sources must be numbered from 1: %+v", got)
	}
}

// The passages are fenced rather than quoted because ingested text is full of
// quotation marks; a quoted block would end at the document's first one.
func TestPromptFencesPassagesAndKeepsThemVerbatim(t *testing.T) {
	body := `Kurucu "biz pazarın lideriyiz" dedi.`
	prompt := BuildPrompt("lider kim?", []Source{{N: 1, Title: "Deck", Body: body}})

	if !strings.Contains(prompt, body) {
		t.Error("the passage must reach the model verbatim, quotes and all")
	}
	if !strings.Contains(prompt, "<<<") || !strings.Contains(prompt, ">>>") {
		t.Error("passages must be fenced with markers the corpus cannot contain")
	}
	if !strings.Contains(prompt, "lider kim?") {
		t.Error("the question is missing from the prompt")
	}
}

func TestCitedSourcesAreOrderedFirstWithStableNumbers(t *testing.T) {
	in := []Source{{N: 1}, {N: 2, Cited: true}, {N: 3}}
	got := OrderSourcesByCitation(in)

	if got[0].N != 2 {
		t.Fatalf("the cited source should lead, got %+v", got)
	}
	// Renumbering would break every [n] marker already written into the answer.
	if got[1].N != 1 || got[2].N != 3 {
		t.Errorf("numbers must stay attached to their source: %+v", got)
	}
}
