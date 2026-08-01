package decision

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		name      string
		reply     string
		wantFound bool
		wantLabel string
		wantScore int
	}{
		{
			name:      "a clarifying question is not a verdict",
			reply:     "Hangi aşamada olduğunu söyler misin — tohum öncesi mi, Seri A mı?",
			wantFound: false,
			wantScore: -1,
		},
		{
			name: "the documented format",
			reply: "Pazar büyüyor [2], ekip deneyimli [4].\n\n" +
				"KARAR: Yatırılabilir\nSKOR: 72\nGEREKÇE: Çekiş güçlü [4].",
			wantFound: true,
			wantLabel: "Yatırılabilir",
			wantScore: 72,
		},
		{
			// A decision without a number is a real state — the persona is told to
			// give both, and a model that gives one is still saying something. -1
			// keeps it distinct from a score of 0, which means the opposite.
			name:      "a label with no score scores -1, not 0",
			reply:     "KARAR: Yatırılamaz\nGEREKÇE: Kanıt yok.",
			wantFound: true,
			wantLabel: "Yatırılamaz",
			wantScore: -1,
		},
		{
			// mlc_llm serves a model that may not honour casing, and the browser's
			// reader is case-insensitive. Two readers of one contract disagreeing
			// about case would show a badge on one side and not the other.
			name:      "case-insensitive, as the browser's reader is",
			reply:     "karar: Temkinli\nskor: 55",
			wantFound: true,
			wantLabel: "Temkinli",
			wantScore: 55,
		},
		{
			// The column is CHECKed to 0..100. A model that writes 900 must not
			// take the whole write down with it — the reply is still worth storing.
			name:      "an out-of-range score is dropped, the label is kept",
			reply:     "KARAR: Temkinli\nSKOR: 900",
			wantFound: true,
			wantLabel: "Temkinli",
			wantScore: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseVerdict(tc.reply)
			if got.Found != tc.wantFound {
				t.Fatalf("Found = %v, want %v", got.Found, tc.wantFound)
			}
			if got.Label != tc.wantLabel {
				t.Errorf("Label = %q, want %q", got.Label, tc.wantLabel)
			}
			if got.Score != tc.wantScore {
				t.Errorf("Score = %d, want %d", got.Score, tc.wantScore)
			}
		})
	}
}

// A model that ignores the format can put a paragraph after "KARAR:", and that
// string is stored and then rendered in a badge.
func TestParseVerdictBoundsTheLabel(t *testing.T) {
	got := parseVerdict("KARAR: " + strings.Repeat("uzun ", 200))
	if !got.Found {
		t.Fatal("expected a verdict")
	}
	if len(got.Label) > 64 {
		t.Errorf("label is %d bytes, want <= 64", len(got.Label))
	}
}

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		name  string
		first string
		want  string
	}{
		{
			name:  "the opening line is already a subject",
			first: "Acme AI — seed aşaması B2B SaaS",
			want:  "Acme AI — seed aşaması B2B SaaS",
		},
		{
			// A brief pasted from a deck carries its whole summary behind a
			// newline; only the first line names the subject.
			name:  "only the first line",
			first: "Palet IQ\n\nB2B SaaS, Seri A öncesi, aylık gelir 180 bin dolar.",
			want:  "Palet IQ",
		},
		{
			name:  "whitespace only falls back rather than titling a thread with nothing",
			first: "   \n  ",
			want:  "Adsız değerlendirme",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveTitle(tc.first); got != tc.want {
				t.Errorf("DeriveTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Titles here are Turkish by default, so the cut has to be in runes. A byte cut
// lands inside ğ, ş or ı and produces a title the browser renders as U+FFFD.
func TestClampTitleCutsRunesNotBytes(t *testing.T) {
	long := strings.Repeat("ğşıçöü", 40)
	got := clampTitle(long)

	if !utf8.ValidString(got) {
		t.Fatalf("clampTitle produced invalid UTF-8: %q", got)
	}
	// titleMaxRunes plus the ellipsis it appends.
	if n := utf8.RuneCountInString(got); n > titleMaxRunes+1 {
		t.Errorf("title is %d runes, want <= %d", n, titleMaxRunes+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated title should say so; got %q", got)
	}
}

// Short titles must survive untouched — the ellipsis is a truncation marker, not
// decoration.
func TestClampTitleLeavesShortTitlesAlone(t *testing.T) {
	const in = "Katı hal batarya üreticileri"
	if got := clampTitle(in); got != in {
		t.Errorf("clampTitle(%q) = %q, want it unchanged", in, got)
	}
}
