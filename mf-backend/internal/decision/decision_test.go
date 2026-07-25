package decision

import "testing"

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
		"https://example.com/direct":                                  "https://example.com/direct",
		"/relative/only":                                              "",
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
