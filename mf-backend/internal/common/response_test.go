package common

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type decodeTarget struct {
	Name string `json:"name"`
}

func TestDecodeRejectsOversizedBody(t *testing.T) {
	body := `{"name":"` + strings.Repeat("x", maxBodyBytes+1) + `"}`
	r := httptest.NewRequest("POST", "/llm/runs", strings.NewReader(body))

	err := Decode(r, &decodeTarget{})
	if err == nil {
		t.Fatal("Decode accepted an oversized body, want it rejected")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 400 {
		t.Fatalf("err = %v, want a 400 APIError", err)
	}
}

func TestDecodeAcceptsNormalBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/llm/runs", strings.NewReader(`{"name":"gemma"}`))

	var dst decodeTarget
	if err := Decode(r, &dst); err != nil {
		t.Fatalf("Decode() = %v, want nil", err)
	}
	if dst.Name != "gemma" {
		t.Errorf("Name = %q, want %q", dst.Name, "gemma")
	}
}

// A body holding two JSON values must fail rather than have the second half
// silently dropped.
func TestDecodeRejectsTrailingData(t *testing.T) {
	r := httptest.NewRequest("POST", "/llm/runs", strings.NewReader(`{"name":"a"}{"name":"b"}`))

	if err := Decode(r, &decodeTarget{}); err == nil {
		t.Error("Decode accepted trailing data, want it rejected")
	}
}

// Buffering the response before writing is what lets the status code reflect an
// encoding failure; it also has to produce a correct Content-Length.
func TestJSONSetsContentLength(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, 200, map[string]string{"status": "ok"})

	res := w.Result()
	defer res.Body.Close()

	if got := res.Header.Get("Content-Length"); got == "" {
		t.Error("Content-Length is unset, want it present")
	}
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("response body did not decode: %v", err)
	}
	if out["status"] != "ok" {
		t.Errorf("status = %q, want %q", out["status"], "ok")
	}
}

// An unencodable value must not leave the client with a truncated body under a
// success status.
func TestJSONReportsEncodeFailure(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, 200, map[string]any{"bad": make(chan int)})

	if w.Result().StatusCode != 500 {
		t.Errorf("status = %d, want 500 when encoding fails", w.Result().StatusCode)
	}
}
