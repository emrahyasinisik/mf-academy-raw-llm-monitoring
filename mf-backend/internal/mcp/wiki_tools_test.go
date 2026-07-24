package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/emrah/mf-backend/internal/wiki"
)

type fakeLibrarian struct {
	hits []wiki.Hit
	ans  wiki.Answer
	err  error
}

func (f *fakeLibrarian) Lookup(context.Context, string, int) ([]wiki.Hit, error) {
	return f.hits, f.err
}

func (f *fakeLibrarian) Answer(context.Context, wiki.AskRequest) (wiki.Answer, error) {
	return f.ans, f.err
}

// A deployment with no knowledge base must not advertise knowledge-base tools.
// A model shown a tool will call it, and one that can only ever answer "empty"
// costs a turn every time.
func TestWikiToolsAreHiddenWithoutAKnowledgeBase(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, nil, "test", "0.0.1")
	for _, tl := range s.tools() {
		if strings.HasSuffix(tl.Name, "_wiki") {
			t.Fatalf("%s advertised with no knowledge base wired", tl.Name)
		}
	}
	if strings.Contains(s.serverInstructions(), "DeepKwiki") {
		t.Error("instructions describe tools that are not offered")
	}
}

func TestWikiToolsAppearWhenWired(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, &fakeLibrarian{}, "test", "0.0.1")

	names := map[string]bool{}
	for _, tl := range s.tools() {
		names[tl.Name] = true
	}
	for _, want := range []string{"search_wiki", "ask_wiki", "analyze_case"} {
		if !names[want] {
			t.Errorf("%s missing from the tool list", want)
		}
	}
	if !strings.Contains(s.serverInstructions(), "grounded=false") {
		t.Error("instructions must warn about ungrounded answers")
	}
}

// Calling a remembered tool against a deployment that has none must produce a
// message, not a nil dereference.
func TestCallingAWikiToolWithoutAKnowledgeBaseIsAMessage(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, nil, "test", "0.0.1")

	res := s.call(context.Background(), "u", "search_wiki", json.RawMessage(`{"query":"x"}`))
	if !res.IsError {
		t.Fatal("want an error result")
	}
}

// The « » highlight markers are for a browser. A model handed them either
// quotes the markup or treats it as part of the source.
func TestSearchResultsCarryVerbatimBodiesNotHighlightedSnippets(t *testing.T) {
	body := "Erken aşama yatırımda ekip en ağır kriterdir."
	s := NewServer(&fakeAnalyzer{}, &fakeLibrarian{hits: []wiki.Hit{{
		DocumentSlug: "d", Title: "T", Body: body,
		Snippet: "Erken aşama «yatırımda» ekip", Rank: 0.4, Matched: "all",
	}}}, "test", "0.0.1")

	res := s.call(context.Background(), "u", "search_wiki", json.RawMessage(`{"query":"ekip"}`))
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	text := res.Content[0].Text
	if !strings.Contains(text, body) {
		t.Error("the verbatim passage must reach the model")
	}
	if strings.Contains(text, "«") {
		t.Error("display markup leaked into the tool result")
	}
}

// An empty result is stated in words as well as an empty array. A model handed
// `[]` rephrases and retries; told the corpus does not cover this, it stops.
func TestEmptySearchSaysSoInWords(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, &fakeLibrarian{hits: nil}, "test", "0.0.1")

	res := s.call(context.Background(), "u", "search_wiki", json.RawMessage(`{"query":"enginar"}`))
	if res.IsError {
		t.Fatalf("an empty corpus is not an error: %+v", res)
	}
	if !strings.Contains(res.Content[0].Text, "Kendi genel bilginle doldurma") {
		t.Errorf("want an explicit instruction not to fill the gap, got: %s", res.Content[0].Text)
	}
}

func TestAskFailureIsReportedNotSwallowed(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, &fakeLibrarian{err: errors.New("inference host is off")},
		"test", "0.0.1")

	res := s.call(context.Background(), "u", "ask_wiki", json.RawMessage(`{"query":"x"}`))
	if !res.IsError {
		t.Fatal("a failed answer must be reported")
	}
	if !strings.Contains(res.Content[0].Text, "inference host is off") {
		t.Errorf("the reason must survive to the model, got: %s", res.Content[0].Text)
	}
}

func TestMissingQueryIsRejected(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, &fakeLibrarian{}, "test", "0.0.1")
	for _, name := range []string{"search_wiki", "ask_wiki"} {
		res := s.call(context.Background(), "u", name, json.RawMessage(`{}`))
		if !res.IsError {
			t.Errorf("%s accepted an empty query", name)
		}
	}
}
