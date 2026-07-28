package decision

import (
	"regexp"
	"strconv"
	"strings"
)

// dimensionPatterns maps normalised keys to the label variants a small model
// might emit in a BOYUTLAR block.
var dimensionPatterns = map[string]*regexp.Regexp{
	"pazar":         regexp.MustCompile(`(?im)^\s*[-*]?\s*pazar\s*[:=]\s*(\d)`),
	"rekabet":       regexp.MustCompile(`(?im)^\s*[-*]?\s*rekabet\s*[:=]\s*(\d)`),
	"moat":          regexp.MustCompile(`(?im)^\s*[-*]?\s*moat\s*[:=]\s*(\d)`),
	"ekip_traction": regexp.MustCompile(`(?im)^\s*[-*]?\s*(?:ekip(?:\s*&?\s*traction)?|traction|çekiş)\s*[:=]\s*(\d)`),
	"risk":          regexp.MustCompile(`(?im)^\s*[-*]?\s*risk\s*[:=]\s*(\d)`),
}

var (
	kararRE = regexp.MustCompile(`(?im)^KARAR:\s*.+$`)
	skorRE  = regexp.MustCompile(`(?im)^SKOR:\s*\d+\s*$`)
)

// parseDimensionScores extracts 0-5 ratings from a BOYUTLAR section. Missing
// dimensions are omitted rather than scored zero.
func parseDimensionScores(text string) map[string]int {
	out := make(map[string]int)
	for key, re := range dimensionPatterns {
		m := re.FindStringSubmatch(text)
		if len(m) < 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 0 || n > 5 {
			continue
		}
		out[key] = n
	}
	return out
}

// normalizeVerdict recomputes SKOR (and aligns KARAR) when the model supplied
// per-dimension ratings. Without a BOYUTLAR block the reply is returned as-is.
func normalizeVerdict(reply string) string {
	dims := parseDimensionScores(reply)
	if len(dims) == 0 {
		return reply
	}

	score, label, _ := scoreFromDimensions(dims)
	block := strings.Join([]string{
		"KARAR: " + label,
		"SKOR: " + strconv.Itoa(score),
	}, "\n")

	lines := strings.Split(reply, "\n")
	var out []string
	replacedKarar, replacedSkor := false, false
	for _, line := range lines {
		switch {
		case kararRE.MatchString(line):
			if !replacedKarar {
				out = append(out, "KARAR: "+label)
				replacedKarar = true
			}
		case skorRE.MatchString(line):
			if !replacedSkor {
				out = append(out, "SKOR: "+strconv.Itoa(score))
				replacedSkor = true
			}
		default:
			out = append(out, line)
		}
	}

	if replacedKarar || replacedSkor {
		return strings.TrimSpace(strings.Join(out, "\n"))
	}
	return strings.TrimSpace(reply + "\n\n" + block)
}
