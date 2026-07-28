package decision

import (
	"strings"
	"testing"
)

func TestScoreFromDimensions(t *testing.T) {
	// All strong — should land in Yatırılabilir.
	score, label, cov := scoreFromDimensions(map[string]int{
		"pazar": 5, "rekabet": 5, "moat": 4, "ekip_traction": 5, "risk": 4,
	})
	if label != "Yatırılabilir" {
		t.Fatalf("label = %q, want Yatırılabilir (score=%d)", label, score)
	}
	if score < 65 {
		t.Fatalf("score = %d, want >= 65", score)
	}
	if cov <= 0 || cov > 1 {
		t.Fatalf("coverage = %v, want (0,1]", cov)
	}

	// Mixed weak — Temkinli band, not stuck at 40.
	score, label, _ = scoreFromDimensions(map[string]int{
		"pazar": 2, "rekabet": 2, "moat": 2, "ekip_traction": 3, "risk": 3,
	})
	if label != "Temkinli" {
		t.Fatalf("label = %q, want Temkinli (score=%d)", label, score)
	}
	if score == 40 {
		t.Fatal("score should vary with inputs, not always 40")
	}

	// Very weak — Yatırılamaz.
	_, label, _ = scoreFromDimensions(map[string]int{
		"pazar": 1, "rekabet": 1, "moat": 1, "ekip_traction": 1, "risk": 1,
	})
	if label != "Yatırılamaz" {
		t.Fatalf("label = %q, want Yatırılamaz", label)
	}
}

func TestParseDimensionScores(t *testing.T) {
	raw := `Kısa özet [1].

BOYUTLAR (0-5):
Pazar: 4
Rekabet: 2
Moat: 3
Ekip: 5
Risk: 2

KARAR: Temkinli
SKOR: 40
GEREKÇE: test`
	got := parseDimensionScores(raw)
	if len(got) != 5 {
		t.Fatalf("parsed %d dimensions, want 5: %v", len(got), got)
	}
	if got["pazar"] != 4 || got["ekip_traction"] != 5 {
		t.Fatalf("unexpected parse: %v", got)
	}
}

func TestNormalizeVerdictRecomputesScore(t *testing.T) {
	raw := `Özet metin [1].

BOYUTLAR:
Pazar: 5
Rekabet: 5
Moat: 5
Ekip: 5
Risk: 5

KARAR: Temkinli
SKOR: 40
GEREKÇE: güçlü kanıtlar`
	out := normalizeVerdict(raw)
	if !strings.Contains(out, "KARAR: Yatırılabilir") {
		t.Fatalf("expected upgraded label, got:\n%s", out)
	}
	if strings.Contains(out, "SKOR: 40") {
		t.Fatalf("stale SKOR: 40 should be replaced, got:\n%s", out)
	}
	if !strings.Contains(out, "SKOR: 100") {
		t.Fatalf("expected SKOR: 100, got:\n%s", out)
	}
}
