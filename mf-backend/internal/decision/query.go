package decision

import (
	"regexp"
	"strings"
	"unicode"
)

// Optional Konu:/Amaç: headers from older primed turns. New chats are free
// text in the bubble; parseIntake still strips legacy headers when present.
func parseIntake(content string) (topic, purpose, rest string) {
	lines := strings.Split(content, "\n")
	i := 0
	if i < len(lines) && strings.HasPrefix(lines[i], "Konu:") {
		topic = strings.TrimSpace(strings.TrimPrefix(lines[i], "Konu:"))
		i++
	}
	if i < len(lines) && strings.HasPrefix(lines[i], "Amaç:") {
		purpose = strings.TrimSpace(strings.TrimPrefix(lines[i], "Amaç:"))
		i++
	}
	if i < len(lines) && lines[i] == "" {
		i++
	}
	rest = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	return topic, purpose, rest
}

// Generic Konu values operators type when the real entity lives in the question
// ("Konu: marka" + "hepsiburada.com için…"). Searching those words wastes the
// live query on noise.
var genericTopics = map[string]struct{}{
	"marka": {}, "ürün": {}, "urun": {}, "şirket": {}, "sirket": {},
	"brand": {}, "product": {}, "company": {}, "pazar": {}, "market": {},
	"teknoloji": {}, "startup": {}, "konu": {},
}

func isGenericTopic(topic string) bool {
	_, ok := genericTopics[strings.ToLower(strings.TrimSpace(topic))]
	return ok
}

var domainRE = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}\b`)

func firstDomain(s string) string {
	return domainRE.FindString(s)
}

// researchEntity picks what the web search should be about: a domain in the
// question beats a generic Konu, and a specific Konu beats a long free-text ask.
func researchEntity(topic, rest string) string {
	if d := firstDomain(rest); d != "" {
		return d
	}
	if d := firstDomain(topic); d != "" {
		return d
	}
	topic = strings.TrimSpace(topic)
	rest = strings.TrimSpace(rest)
	if topic != "" && !isGenericTopic(topic) {
		if rest == "" {
			return topic
		}
		return strings.TrimSpace(topic + " " + firstWords(rest, 8))
	}
	if rest != "" {
		return firstWords(rest, 12)
	}
	return topic
}

// researchHint infers search keywords from free text (the bubble) or a legacy
// Amaç line — marketing words beat investability when both appear.
func researchHint(text string) string {
	p := strings.ToLower(strings.TrimSpace(text))
	switch {
	case p == "":
		return ""
	case strings.Contains(p, "pazarlama") || strings.Contains(p, "reklam") ||
		strings.Contains(p, "kanal") || strings.Contains(p, "platform") ||
		strings.Contains(p, "medya"):
		return "dijital reklam pazarlama platform Türkiye"
	case strings.Contains(p, "yatır") || strings.Contains(p, "seed") ||
		strings.Contains(p, "invest"):
		return "şirket yatırım pazar"
	default:
		return ""
	}
}

// researchQueries returns the primary live-search string and an optional
// entity-only fallback. The first user bubble anchors the entity; later turns
// keep that anchor so a one-word reply still researches the right brand.
func researchQueries(history []Turn, latest string) (primary, fallback string) {
	first := deriveSubject(history)
	topic, purpose, rest := parseIntake(first)
	body := rest
	if body == "" {
		body = first
	}
	entity := researchEntity(topic, body)
	if entity == "" {
		entity = firstWords(latest, 12)
	}
	entity = stripAskNoise(entity)
	if entity == "" {
		entity = firstWords(stripAskNoise(body), 8)
	}

	hint := researchHint(purpose)
	if hint == "" {
		hint = researchHint(body)
	}
	if hint == "" {
		hint = researchHint(latest)
	}
	// "X'i biliyor musun?" is an identity ask — don't staple marketing keywords.
	identityAsk := hint == "" && (strings.Contains(strings.ToLower(body), "biliyor") ||
		strings.Contains(strings.ToLower(latest), "biliyor") ||
		strings.Contains(strings.ToLower(body), "nedir"))

	if latest != first && strings.TrimSpace(latest) != "" {
		primary = strings.TrimSpace(entity + " " + firstWords(stripAskNoise(latest), 10))
	} else if identityAsk {
		primary = entity
	} else {
		primary = strings.TrimSpace(entity + " " + hint)
	}
	primary = truncate(primary, 200)
	fallback = truncate(entity, 120)
	if fallback == "" || fallback == primary {
		return primary, ""
	}
	return primary, fallback
}

func firstWords(s string, n int) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	if len(fields) <= n {
		return strings.Join(fields, " ")
	}
	return strings.Join(fields[:n], " ")
}

// Words that pad a chat ask but poison a search query ("visevent app'i
// biliyor musun?" must search VisEvent, not the polite wrapper).
var askNoise = map[string]struct{}{
	"biliyor": {}, "biliyormusun": {}, "biliyormusun?": {},
	"musun": {}, "musun?": {}, "misin": {}, "misin?": {},
	"nedir": {}, "nedir?": {}, "hakkında": {}, "hakkinda": {},
	"ne": {}, "nasıl": {}, "nasil": {}, "için": {}, "icin": {},
	"bir": {}, "bu": {}, "şu": {}, "su": {}, "var": {}, "mı": {}, "mi": {},
	"app'i": {}, "appi": {}, "uygulaması": {}, "uygulamasi": {},
	"uygulama": {}, "app": {}, // keep brand; "app" alone is noise next to a name
}

// stripAskNoise keeps proper nouns / domains and drops chat fillers. "app" is
// dropped only when another token remains — otherwise "event app" would vanish.
func stripAskNoise(s string) string {
	fields := strings.Fields(s)
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		key := strings.ToLower(strings.Trim(f, "?.!,;'\"“”"))
		if _, noise := askNoise[key]; noise {
			continue
		}
		kept = append(kept, f)
	}
	if len(kept) == 0 {
		return strings.TrimSpace(s)
	}
	return strings.Join(kept, " ")
}
