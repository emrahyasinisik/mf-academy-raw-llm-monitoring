package decision

// A tool that ran and found nothing, and a tool that never ran, are different
// facts about the world. The persona screen showed both as "0" — which is how a
// keyless DuckDuckGo blocked at the datacentre IP and an empty DeepKwiki corpus
// both read as "the market has no coverage" for weeks. These tests pin the
// distinction at the only place it can still be observed: the research step and
// the evidence block the model is given.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/wiki"
)

type erroringSearcher struct{}

func (erroringSearcher) Name() string { return "duckduckgo" }

func (erroringSearcher) Search(context.Context, string, int) ([]SearchResult, error) {
	return nil, errors.New("duckduckgo returned 403")
}

type emptySearcher struct{}

func (emptySearcher) Name() string { return "duckduckgo" }

func (emptySearcher) Search(context.Context, string, int) ([]SearchResult, error) {
	return nil, nil
}

type erroringWiki struct{}

func (erroringWiki) Search(context.Context, string, int) ([]wiki.Hit, error) {
	return nil, errors.New("connection refused")
}

type emptyWiki struct{}

func (emptyWiki) Search(context.Context, string, int) ([]wiki.Hit, error) {
	return nil, nil
}

func stepFor(steps []ResearchStep, tool string) (ResearchStep, bool) {
	for _, s := range steps {
		if s.Tool == tool {
			return s, true
		}
	}
	return ResearchStep{}, false
}

func gatherWith(t *testing.T, s Searcher, w WikiRetriever) ([]ResearchStep, string) {
	t.Helper()
	agent := NewAgent(&fakeChatter{}, s, w, fixedSettings{}, 1366, 120*time.Second)
	plan := agent.plan([]Turn{{Role: "user", Content: "Acme AI"}})
	_, evidence, steps := agent.gather(context.Background(), "Acme AI", "", plan.evidence)
	return steps, evidence
}

func TestGatherRecordsWhyAToolReturnedNothing(t *testing.T) {
	// The failure the operator has to be able to act on: search is broken, not
	// the market is uncovered.
	steps, _ := gatherWith(t, erroringSearcher{}, emptyWiki{})

	web, ok := stepFor(steps, "web_research")
	if !ok {
		t.Fatal("the web step must be reported even when the tool failed")
	}
	if web.Error == "" {
		t.Fatalf("a failed search must carry its error, got step %+v", web)
	}

	// The same shape of step, from a tool that worked: no error to report.
	steps, _ = gatherWith(t, emptySearcher{}, emptyWiki{})
	web, _ = stepFor(steps, "web_research")
	if web.Error != "" {
		t.Errorf("a search that ran and found nothing must not report an error, got %q", web.Error)
	}
}

func TestGatherRecordsAWikiFailure(t *testing.T) {
	// DeepKwiki's error was dropped on the floor: the turn continued with no
	// trace of it in the step or in the prompt.
	steps, evidence := gatherWith(t, emptySearcher{}, erroringWiki{})

	w, ok := stepFor(steps, "wiki_retrieve")
	if !ok {
		t.Fatal("the wiki step must be reported")
	}
	if w.Error == "" {
		t.Fatalf("a failed DeepKwiki search must carry its error, got step %+v", w)
	}
	if !strings.Contains(evidence, "DeepKwiki") {
		t.Error("the model must be told DeepKwiki was unavailable, not left to assume it was empty")
	}
}

func TestGatherNamesTheProviderThatRan(t *testing.T) {
	// A thin answer traced to the keyless fallback is a configuration finding;
	// the same answer with no provider named is a mystery.
	steps, _ := gatherWith(t, emptySearcher{}, emptyWiki{})

	web, _ := stepFor(steps, "web_research")
	if web.Provider != "duckduckgo" {
		t.Errorf("web step provider = %q, want the searcher's name", web.Provider)
	}
	w, _ := stepFor(steps, "wiki_retrieve")
	if w.Provider != "deepkwiki" {
		t.Errorf("wiki step provider = %q, want %q", w.Provider, "deepkwiki")
	}
}

func TestEvidenceBlockSeparatesFailureFromEmptiness(t *testing.T) {
	_, broken := gatherWith(t, erroringSearcher{}, emptyWiki{})
	_, empty := gatherWith(t, emptySearcher{}, emptyWiki{})

	if broken == empty {
		t.Fatal("a failed search and an empty one produce the same prompt; the model cannot tell them apart either")
	}
}
