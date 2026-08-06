package decision

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/emrah/mf-backend/internal/wiki"
)

func TestSanitize(t *testing.T) {
	t.Run("drops empty turns and keeps order", func(t *testing.T) {
		out, err := sanitize([]Turn{
			{Role: "user", Content: "Acme AI"},
			{Role: "assistant", Content: "  "},
			{Role: "assistant", Content: "Aşaman nedir?"},
			{Role: "user", Content: "Seed"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 3 {
			t.Fatalf("want 3 turns, got %d", len(out))
		}
		if out[len(out)-1].Content != "Seed" {
			t.Fatalf("last turn should be the user's, got %q", out[len(out)-1].Content)
		}
	})

	t.Run("rejects a transcript ending on the assistant", func(t *testing.T) {
		_, err := sanitize([]Turn{
			{Role: "user", Content: "Acme AI"},
			{Role: "assistant", Content: "Aşaman nedir?"},
		})
		if err == nil {
			t.Fatal("want error when the last turn is not the user's")
		}
	})

	t.Run("rejects an empty conversation", func(t *testing.T) {
		if _, err := sanitize(nil); err == nil {
			t.Fatal("want error for no messages")
		}
	})
}

func TestBuildQuery(t *testing.T) {
	if got := buildQuery("Acme AI", "Acme AI"); got != "Acme AI" {
		t.Fatalf("subject should not be repeated: %q", got)
	}
	if got := buildQuery("Acme AI", "500K bütçe"); got != "Acme AI 500K bütçe" {
		t.Fatalf("want subject anchored to the latest message, got %q", got)
	}
}

func TestResearchQueriesPrefersDomainOverGenericKonu(t *testing.T) {
	// Legacy primed turn: Konu=marka, brand only in the ask.
	msg := "Konu: marka\nAmaç: pazarlama\n\nhepsiburada.com için hangi platformlarda reklama yapsak"
	primary, fallback := researchQueries([]Turn{{Role: "user", Content: msg}}, msg)
	if !strings.Contains(strings.ToLower(primary), "hepsiburada.com") {
		t.Fatalf("primary query must anchor on the domain, got %q", primary)
	}
	if strings.Contains(strings.ToLower(primary), "konu:") {
		t.Fatalf("intake labels must not be searched: %q", primary)
	}
	if fallback != "hepsiburada.com" {
		t.Fatalf("fallback should be bare domain, got %q", fallback)
	}
	if !strings.Contains(strings.ToLower(primary), "pazarlama") &&
		!strings.Contains(strings.ToLower(primary), "reklam") {
		t.Fatalf("marketing purpose should shape the query, got %q", primary)
	}
}

func TestResearchQueriesFromFreeTextBubble(t *testing.T) {
	msg := "hepsiburada.com için hangi platformlarda reklama yapsak?"
	primary, fallback := researchQueries([]Turn{{Role: "user", Content: msg}}, msg)
	if !strings.Contains(strings.ToLower(primary), "hepsiburada.com") {
		t.Fatalf("free-text bubble must still search the domain, got %q", primary)
	}
	if fallback != "hepsiburada.com" {
		t.Fatalf("fallback should be bare domain, got %q", fallback)
	}
	if !strings.Contains(strings.ToLower(primary), "reklam") &&
		!strings.Contains(strings.ToLower(primary), "pazarlama") {
		t.Fatalf("marketing words in the bubble should shape the query, got %q", primary)
	}
}

func TestResearchQueriesIdentityAskStripsChatNoise(t *testing.T) {
	msg := "visevent app'i biliyormusun?"
	primary, fallback := researchQueries([]Turn{{Role: "user", Content: msg}}, msg)
	low := strings.ToLower(primary)
	if !strings.Contains(low, "visevent") {
		t.Fatalf("identity ask must search the brand, got %q", primary)
	}
	if strings.Contains(low, "biliyor") {
		t.Fatalf("chat wrapper must not be searched: %q", primary)
	}
	if strings.Contains(low, "pazarlama") || strings.Contains(low, "reklam") {
		t.Fatalf("identity ask must not get a marketing hint: %q", primary)
	}
	// Fallback is omitted when it would equal primary — fine for a short brand query.
	if fallback != "" && !strings.Contains(strings.ToLower(fallback), "visevent") {
		t.Fatalf("fallback should keep the brand when present, got %q", fallback)
	}
}

func TestIsSelfAsk(t *testing.T) {
	if !isSelfAsk("sen kimsin") || !isSelfAsk("Sen kimsin?") {
		t.Fatal("persona meta-asks must be detected")
	}
	if isSelfAsk("visevent app'i biliyormusun?") {
		t.Fatal("brand identity asks are not self-asks")
	}
	primary, fallback := researchQueries(
		[]Turn{{Role: "user", Content: "sen kimsin"}},
		"sen kimsin",
	)
	if primary != "" || fallback != "" {
		t.Fatalf("self-ask must not build a web query, got %q / %q", primary, fallback)
	}
}

func TestShouldClarifyPersonaJoke(t *testing.T) {
	// The failure: "sen armutsun" after a kimsin thread searched Hidra lyrics.
	hist := []Turn{
		{Role: "user", Content: "sen kimsin"},
		{Role: "assistant", Content: "Araştırma personasıyım."},
		{Role: "user", Content: "sen armutsun"},
	}
	if !shouldClarify(hist, "sen armutsun") {
		t.Fatal("persona jokes must clarify, not search")
	}
	if !isPersonaAddress("sen armutsun **") {
		t.Fatal("punctuation must not hide a persona address")
	}
	if !isGreeting("selam") || !shouldClarify(nil, "selam") {
		t.Fatal("greetings must clarify warmly, not search")
	}
	// After selam, "olur" must not invent a song research thread.
	afterHi := []Turn{
		{Role: "user", Content: "selam"},
		{Role: "assistant", Content: greetingReply},
		{Role: "user", Content: "olur"},
	}
	if !shouldClarify(afterHi, "olur") {
		t.Fatal("vague follow-up after a greeting must clarify")
	}
	// Thin follow-up in a real market thread still researches.
	market := []Turn{
		{Role: "user", Content: "Türkiye'de hızlı market teslimatı pazarı nasıl görünüyor?"},
		{Role: "assistant", Content: "Trendyol ve Getir…"},
		{Role: "user", Content: "başka markalar yok mu"},
	}
	if shouldClarify(market, "başka markalar yok mu") {
		t.Fatal("market follow-ups must still research")
	}
}

func TestResearchQueriesDomainPaste(t *testing.T) {
	msg := "www.visevent.com"
	primary, fallback := researchQueries([]Turn{{Role: "user", Content: msg}}, msg)
	if primary != "visevent.com" {
		t.Fatalf("domain paste primary = %q, want visevent.com", primary)
	}
	if fallback != "site:visevent.com" {
		t.Fatalf("domain paste fallback = %q, want site:visevent.com", fallback)
	}
}

func TestPreferHostMatch(t *testing.T) {
	in := []SearchResult{
		{URL: "https://arxiv.org/abs/2108.05015", Title: "paper"},
		{URL: "https://www.visevent.com/", Title: "home"},
	}
	out := preferHostMatch(in, "visevent.com")
	if hostOf(out[0].URL) != "visevent.com" {
		t.Fatalf("matching host should sort first, got %q", out[0].URL)
	}
}

func TestDeriveSubject(t *testing.T) {
	got := deriveSubject([]Turn{
		{Role: "assistant", Content: "Merhaba"},
		{Role: "user", Content: "Acme AI"},
	})
	if got != "Acme AI" {
		t.Fatalf("subject should be the first user turn, got %q", got)
	}
}

func TestUnwrapDDG(t *testing.T) {
	cases := map[string]string{
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&rut=x": "https://example.com/a",
		"https://example.com/direct":                                   "https://example.com/direct",
		"/relative/only":                                               "",
	}
	for in, want := range cases {
		if got := unwrapDDG(in); got != want {
			t.Errorf("unwrapDDG(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanHTML(t *testing.T) {
	if got := cleanHTML("<b>Acme</b> &amp; Co"); got != "Acme & Co" {
		t.Fatalf("cleanHTML stripped/unescaped wrong: %q", got)
	}
}

// ---- prompt budget ----
//
// The persona is the one path whose prompt grows without anybody writing a long
// message: it re-sends the conversation and staples fresh evidence to every
// turn. The engine rejects an over-long prompt with a 400 instead of answering
// short, so a turn that outgrows the window does not degrade — it stops working.
// These tests pin the budget that keeps it inside the window.

type fakeChatter struct {
	got []llm.Message
}

func (f *fakeChatter) Chat(_ context.Context, _ string, msgs []llm.Message, _ float64, _ int) (llm.Completion, error) {
	f.got = msgs
	return llm.Completion{Content: "KARAR: Temkinli"}, nil
}

// fatSearcher returns the maximum number of results, each with a snippet longer
// than any per-source cap, so the budget is the only thing bounding the prompt.
type fatSearcher struct{}

func (fatSearcher) Name() string { return "fake" }

func (fatSearcher) Search(_ context.Context, _ string, limit int) ([]SearchResult, error) {
	out := make([]SearchResult, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, SearchResult{
			Title:   fmt.Sprintf("Kaynak %d — pazar büyüklüğü ve rekabet değerlendirmesi", i),
			URL:     fmt.Sprintf("https://example.com/kaynak/%d", i),
			Snippet: strings.Repeat("Türkçe kanıt metni, diyakritikli. ", 60),
		})
	}
	return out, nil
}

type fatWiki struct{}

func (fatWiki) Search(_ context.Context, _ string, limit int) ([]wiki.Hit, error) {
	out := make([]wiki.Hit, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, wiki.Hit{
			Title:   fmt.Sprintf("Rubrik %d", i),
			Heading: "Pazar",
			Body:    strings.Repeat("Rubrik maddesi ve puanlama esası. ", 60),
		})
	}
	return out, nil
}

type fixedSettings struct{}

func (fixedSettings) Get(context.Context) (settings.Settings, error) {
	return settings.Settings{}, nil
}

// promptChars sums what the agent actually sent, in the same unit the budget is
// kept in.
func promptChars(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

func TestRespondKeepsPromptInsideTheWindow(t *testing.T) {
	// A long conversation and fat evidence together: either one alone used to
	// fit, and the failure only appeared once both did not.
	history := make([]Turn, 0, 21)
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		history = append(history, Turn{Role: role, Content: strings.Repeat("önceki tur metni. ", 40)})
	}
	history = append(history, Turn{Role: "user", Content: "Tesla 2026 için yatırılabilir mi?"})

	// 1366 is what the Qwen3-4B build on the 6 GB card actually serves.
	window := 1366
	chat := &fakeChatter{}
	agent := NewAgent(chat, fatSearcher{}, fatWiki{}, fixedSettings{}, window, 120*time.Second)

	res, err := agent.Respond(context.Background(), history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	budget := int(charsPerToken * float64(window))
	if got := promptChars(chat.got); got > budget {
		t.Fatalf("prompt is %d chars, over the %d-char budget for a %d-token window", got, budget, window)
	}

	// Trimming must not cost the turn its grounding: the system prompt, the new
	// user message and the instruction all have to survive.
	if chat.got[0].Role != "system" || chat.got[0].Content != personaSystemPrompt {
		t.Fatalf("the persona prompt did not survive the trim")
	}
	final := chat.got[len(chat.got)-1].Content
	for _, want := range []string{"Tesla 2026 için yatırılabilir mi?", turnInstruction, evidenceHeader} {
		if !strings.Contains(final, want) {
			t.Fatalf("the final turn lost %q", want)
		}
	}
	if len(res.Sources) == 0 {
		t.Fatalf("a trimmed turn still has to carry at least one source")
	}
}

func TestGatherCitationsMatchTheSourceList(t *testing.T) {
	// A citation the UI cannot resolve is the one failure this feature cannot
	// have, so the numbering in the block and the list rendered beside it are
	// checked against each other rather than each against itself.
	agent := NewAgent(&fakeChatter{}, fatSearcher{}, fatWiki{}, fixedSettings{}, 1366, 120*time.Second)
	plan := agent.plan([]Turn{{Role: "user", Content: "Acme AI"}})

	sources, evidence, steps := agent.gather(context.Background(), "Acme AI", "", plan.evidence)

	if len(evidence) > plan.evidence {
		t.Fatalf("evidence is %d chars, over its %d-char budget", len(evidence), plan.evidence)
	}
	if len(steps) != 2 {
		t.Fatalf("both tools must be reported even when their results are trimmed, got %d", len(steps))
	}
	for i, s := range sources {
		if s.N != i+1 {
			t.Fatalf("source %d is numbered %d; the list must be gapless", i, s.N)
		}
		if !strings.Contains(evidence, fmt.Sprintf("[%d]", s.N)) {
			t.Fatalf("source [%d] is listed but never cited in the block", s.N)
		}
	}
	// The converse: nothing cited that is not listed.
	if strings.Contains(evidence, fmt.Sprintf("[%d]", len(sources)+1)) {
		t.Fatalf("the block cites [%d] but only %d sources are listed", len(sources)+1, len(sources))
	}
}

func TestPlanTruncatesAMessageThatAloneExceedsTheWindow(t *testing.T) {
	agent := NewAgent(&fakeChatter{}, fatSearcher{}, fatWiki{}, fixedSettings{}, 1366, 120*time.Second)
	huge := strings.Repeat("ş", maxTurnChars)

	plan := agent.plan([]Turn{{Role: "user", Content: huge}})

	if len(plan.latest) >= len(huge) {
		t.Fatalf("a message larger than the window must be truncated, got %d chars", len(plan.latest))
	}
	if !utf8.ValidString(plan.latest) {
		t.Fatalf("truncation split a rune: the Turkish text is no longer valid UTF-8")
	}
	if plan.evidence != 0 || plan.history != 0 {
		t.Fatalf("nothing is left to spend on evidence or history, got %d/%d", plan.evidence, plan.history)
	}
}
