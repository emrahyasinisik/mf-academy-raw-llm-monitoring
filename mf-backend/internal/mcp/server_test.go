package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/analysis"
	"github.com/emrah/mf-backend/internal/common"
)

// fakeAnalyzer stands in for the engine so the protocol can be exercised
// without a database or a GPU.
type fakeAnalyzer struct {
	report analysis.Assessment
	err    error
	calls  int
}

func (f *fakeAnalyzer) ExecuteAnalysis(_ context.Context, _ string, _ analysis.AnalyzeRequest) (analysis.Assessment, error) {
	f.calls++
	return f.report, f.err
}
func (f *fakeAnalyzer) Catalogue(context.Context) ([]analysis.Domain, error) {
	return []analysis.Domain{{
		Slug: "startup-investability", Name: "Yatırım", Version: 1,
		Criteria: []analysis.Criterion{{Key: "traction", Label: "Çekiş", Weight: 0.2, ScaleMax: 5,
			Guidance: "bu metin istemcinin modeline gitmemeli"}},
	}}, nil
}
func (f *fakeAnalyzer) Report(context.Context, string, string) (analysis.Assessment, error) {
	return f.report, f.err
}
func (f *fakeAnalyzer) Reports(context.Context, string, string, int) (analysis.ListResult, error) {
	return analysis.ListResult{Assessments: []analysis.AssessmentSummary{}}, nil
}

func newTestServer(a Analyzer) http.Handler {
	s := NewServer(a, nil, "test", "0.0.1")
	// Claims are injected directly: RequireAuth is the router's job and is
	// tested where it lives.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := common.ContextWithClaims(r.Context(),
			common.AuthClaims{UserID: "u-1", Email: "a@b.c", Role: "user"})
		s.Handler(w, r.WithContext(ctx))
	})
}

func post(t *testing.T, h http.Handler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("response was not JSON: %v\n%s", err, rec.Body.String())
		}
	}
	return rec, out
}

func TestInitializeEchoesASupportedVersion(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize",
	  "params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"test","version":"1"}}}`)

	res := out["result"].(map[string]any)
	if got := res["protocolVersion"]; got != "2025-03-26" {
		t.Errorf("protocolVersion = %v, want the client's own supported version", got)
	}
	if res["instructions"] == nil {
		t.Error("instructions absent; the client's model is not told how to read a score")
	}
}

// Agreeing to a version we do not implement produces responses the client
// cannot parse, and the failure surfaces far from its cause.
func TestInitializeFallsBackOnUnknownVersion(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize",
	  "params":{"protocolVersion":"1999-01-01"}}`)

	res := out["result"].(map[string]any)
	if got := res["protocolVersion"]; got != LatestVersion {
		t.Errorf("protocolVersion = %v, want %v", got, LatestVersion)
	}
}

// The id must come back byte-identical: a client matches responses to calls by
// it, and a string id decoded into a number would never match.
func TestStringIDIsEchoedUnchanged(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	rec, _ := post(t, h, `{"jsonrpc":"2.0","id":"abc-123","method":"ping"}`)
	if !strings.Contains(rec.Body.String(), `"id":"abc-123"`) {
		t.Errorf("id not echoed as a string: %s", rec.Body.String())
	}
}

func TestLargeNumericIDKeepsPrecision(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	rec, _ := post(t, h, `{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`)
	if !strings.Contains(rec.Body.String(), "9007199254740993") {
		t.Errorf("large id lost precision: %s", rec.Body.String())
	}
}

// Answering a notification is a protocol violation; some clients treat an
// unexpected response to notifications/initialized as a fatal handshake error.
func TestNotificationGetsNoBody(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	rec, _ := post(t, h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Body.Len() != 0 {
		t.Errorf("a notification was answered: %s", rec.Body.String())
	}
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", rec.Code)
	}
}

func TestToolsListNamesEveryTool(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	tools := out["result"].(map[string]any)["tools"].([]any)
	seen := map[string]bool{}
	for _, tl := range tools {
		m := tl.(map[string]any)
		seen[m["name"].(string)] = true
		if m["inputSchema"] == nil {
			t.Errorf("tool %v has no inputSchema; a client cannot call it", m["name"])
		}
	}
	for _, want := range []string{"list_analysis_domains", "analyze_case", "get_report", "list_reports"} {
		if !seen[want] {
			t.Errorf("tool %q missing", want)
		}
	}
}

func TestUnknownMethodIsJSONRPCError(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)

	e, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON-RPC error, got %v", out)
	}
	if int(e["code"].(float64)) != codeMethodNotFound {
		t.Errorf("code = %v, want %d", e["code"], codeMethodNotFound)
	}
}

// A tool that ran and failed has a result, and that result must reach the
// model. Reported as a JSON-RPC error it would be swallowed by the client's
// transport and the model would retry the same call into the same silence.
func TestToolFailureIsAResultNotAnError(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{err: common.ErrUnavailable("inference host is off")})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
	  "params":{"name":"analyze_case","arguments":{"domain":"d","subject":"`+strings.Repeat("x", 60)+`"}}}`)

	if out["error"] != nil {
		t.Fatalf("a tool failure became a JSON-RPC error: %v", out["error"])
	}
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("isError not set on a failed tool call")
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "inference host is off") {
		t.Errorf("the engine's message did not reach the model: %q", text)
	}
}

func TestMissingArgumentsAreReportedToTheModel(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
	  "params":{"name":"analyze_case","arguments":{"subject":"yeterince uzun bir vaka metni burada"}}}`)

	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatal("a missing required argument was accepted")
	}
}

// Batching was removed from MCP in 2025-06-18. A client sending one should be
// told, not handed a decode error that reads like our bug.
func TestBatchIsRefusedClearly(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)

	e := out["error"].(map[string]any)
	if !strings.Contains(e["message"].(string), "batch") {
		t.Errorf("message does not explain the refusal: %v", e["message"])
	}
}

func TestWrongJSONRPCVersionIsRefused(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	_, out := post(t, h, `{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	if out["error"] == nil {
		t.Error("a non-2.0 message was accepted")
	}
}

// The rubric's guidance is written for the analysing model and runs to hundreds
// of words per criterion. Sending it to the client's model spends its context
// on instructions meant for a different model entirely.
func TestDomainListOmitsPromptGuidance(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{})
	rec, _ := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
	  "params":{"name":"list_analysis_domains","arguments":{}}}`)

	if strings.Contains(rec.Body.String(), "istemcinin modeline gitmemeli") {
		t.Error("the analysing model's guidance leaked into the client's tool result")
	}
	if !strings.Contains(rec.Body.String(), "traction") {
		t.Error("criteria are missing from the domain list")
	}
}

// A score without its coverage is the one way to misread these reports, so the
// warning travels attached to the number rather than only in the instructions.
func TestReportCarriesACoverageNote(t *testing.T) {
	score := 75.0
	h := newTestServer(&fakeAnalyzer{report: analysis.Assessment{
		ID: "r1", DomainSlug: "d", OverallScore: &score, Coverage: 0.4,
		SchemaValid: true, CreatedAt: time.Now(),
		Findings: []analysis.Finding{
			{Key: "a", EvidenceFound: true, Score: &score},
			{Key: "b", EvidenceFound: false},
			{Key: "c", EvidenceFound: false},
		},
	}})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
	  "params":{"name":"get_report","arguments":{"id":"r1"}}}`)

	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "coverage_note") {
		t.Fatal("no coverage note on the report")
	}
	if !strings.Contains(text, "DİKKAT") {
		t.Errorf("low coverage did not raise a warning:\n%s", text)
	}
}

func TestUnassessableReportSaysSoRatherThanScoringZero(t *testing.T) {
	h := newTestServer(&fakeAnalyzer{report: analysis.Assessment{
		ID: "r2", OverallScore: nil, Coverage: 0,
	}})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
	  "params":{"name":"get_report","arguments":{"id":"r2"}}}`)

	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "sıfır puan DEĞİL") {
		t.Errorf("a nil score was not distinguished from zero:\n%s", text)
	}
}

// A repaired answer is still worth reading, but somebody relaying it into a
// funding decision should know it was not clean.
func TestRepairedReportCarriesAWarning(t *testing.T) {
	score := 60.0
	h := newTestServer(&fakeAnalyzer{report: analysis.Assessment{
		ID: "r3", OverallScore: &score, Coverage: 1, SchemaValid: false,
	}})
	_, out := post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call",
	  "params":{"name":"get_report","arguments":{"id":"r3"}}}`)

	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "schema_warning") {
		t.Errorf("a repaired report was presented as clean:\n%s", text)
	}
}

func TestUnauthenticatedRequestIsRejected(t *testing.T) {
	s := NewServer(&fakeAnalyzer{}, nil, "test", "0.0.1")
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	rec := httptest.NewRecorder()
	s.Handler(rec, req) // no claims on the context

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
