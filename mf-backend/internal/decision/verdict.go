package decision

import (
	"regexp"
	"strconv"
	"strings"
)

// The verdict lines the persona is instructed to close a final answer with. See
// personaSystemPrompt for the contract itself; this is the reader for it.
//
// Parsed here as well as in the browser, which is duplication with a reason: the
// conversation list shows a decision badge per thread, and it renders from a
// summary query that deliberately does not carry message bodies. Sending every
// transcript to the client so it could re-derive one label per thread would make
// the list the most expensive read in the product. The regexes are kept
// deliberately identical in shape to the frontend's so a change to the prompt
// contract is one grep away from both readers.
var (
	verdictRe = regexp.MustCompile(`(?i)KARAR:\s*([^\n]+)`)
	scoreRe   = regexp.MustCompile(`(?i)SKOR:\s*(\d{1,3})`)
)

// Verdict is a decision the persona committed to. Absent — the zero value with
// Found false — while it is still researching or asking clarifying questions,
// which is a normal state for a thread and not a parse failure.
type Verdict struct {
	Found bool
	Label string
	// Score is -1 when the persona named a decision without a number. Kept
	// distinct from 0, which is a real score meaning "certainly not".
	Score int
}

// parseVerdict reads the machine-readable lines out of a reply. sources is how
// many pieces of evidence the turn actually gathered, and it decides whether the
// number on the SKOR line means anything.
//
// A turn that researched nothing can still emit "SKOR: 0" — a 2B model asked for
// a KARAR/SKOR block produces one whatever the evidence says, and the empty-
// research instruction is a request, not a constraint. Recording that 0 would
// put a measured-looking number on a thread that read nothing, which is the
// same mistake the analysis path is built to avoid: absence of information is
// not a low score. So with no sources the score stays -1 and the column stays
// NULL. The label survives, because the model did commit to a word and the
// reader can see for themselves that nothing backs it.
func parseVerdict(reply string, sources int) Verdict {
	m := verdictRe.FindStringSubmatch(reply)
	if m == nil {
		return Verdict{Score: -1}
	}
	v := Verdict{Found: true, Label: strings.TrimSpace(m[1]), Score: -1}

	// Guard the label length rather than trusting the line. A model that ignores
	// the format can emit a paragraph after "KARAR:", and this string goes into
	// a badge — an unbounded one would be stored, listed and rendered.
	if len(v.Label) > 64 {
		v.Label = strings.TrimSpace(v.Label[:64])
	}

	if s := scoreRe.FindStringSubmatch(reply); s != nil && sources > 0 {
		if n, err := strconv.Atoi(s[1]); err == nil && n >= 0 && n <= 100 {
			v.Score = n
		}
	}
	return v
}
