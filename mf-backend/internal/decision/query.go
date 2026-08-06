package decision

import (
	"net/url"
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

func normalizeChat(s string) string {
	n := strings.ToLower(strings.TrimSpace(s))
	n = strings.Trim(n, "?.!…*")
	return strings.Join(strings.Fields(n), " ")
}

// isSelfAsk is a question about the persona itself. Those must not go to live
// search — "sen kimsin" otherwise retrieves a Turkish pop song on JioSaavn.
func isSelfAsk(s string) bool {
	switch normalizeChat(s) {
	case "sen kimsin", "kimsin", "sen nesin", "kendini tanıt", "kendini tanit",
		"who are you", "what are you":
		return true
	default:
		return false
	}
}

// isPersonaAddress is a joke/hitap aimed at the assistant ("sen armutsun").
// Searching it dredges song lyrics from a prior "kimsin" thread.
func isPersonaAddress(s string) bool {
	if isSelfAsk(s) {
		return false
	}
	n := normalizeChat(s)
	return strings.HasPrefix(n, "sen ") || strings.HasPrefix(n, "seni ")
}

// isTooVague is a message with no domain, no purpose keywords, and almost no
// substance after chat noise is stripped.
func isTooVague(s string) bool {
	if firstDomain(s) != "" || researchHint(s) != "" {
		return false
	}
	words := strings.Fields(stripAskNoise(s))
	if len(words) > 0 && strings.EqualFold(words[0], "sen") {
		words = words[1:]
	}
	return len(words) <= 2
}

// shouldClarify skips live search and asks the user to restate the ask.
// Established research threads still search thin follow-ups ("başka markalar?").
func shouldClarify(history []Turn, latest string) bool {
	if isPersonaAddress(latest) {
		return true
	}
	if !isTooVague(latest) {
		return false
	}
	if len(history) < 2 {
		return true
	}
	first := deriveSubject(history)
	if isSelfAsk(first) || isPersonaAddress(first) {
		return true
	}
	_, _, rest := parseIntake(first)
	body := rest
	if body == "" {
		body = first
	}
	if firstDomain(body) != "" || researchHint(body) != "" {
		return false
	}
	return len(strings.Fields(stripAskNoise(body))) < 4
}

// Generic Konu values operators type when the real entity lives in the question.
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
	return normalizeHost(domainRE.FindString(s))
}

// normalizeHost strips scheme/www and lowercases so www.VisEvent.com and
// visevent.com collapse to one prefer-host key.
func normalizeHost(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = u.Host
		}
	}
	s = strings.TrimPrefix(strings.ToLower(s), "www.")
	return strings.Trim(s, "/")
}

// domainOnlyReport is true when the message is essentially just a URL/host —
// the operator pasted the site as the subject.
func domainOnlyReport(s string) (host string, ok bool) {
	raw := strings.TrimSpace(s)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.Trim(raw, "/")
	fields := strings.Fields(raw)
	if len(fields) != 1 {
		return "", false
	}
	host = normalizeHost(fields[0])
	if host == "" || !strings.Contains(host, ".") {
		return "", false
	}
	// Reject paths: "visevent.com/foo"
	if strings.Contains(strings.TrimPrefix(fields[0], "www."), "/") {
		return "", false
	}
	return host, true
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
	case strings.Contains(p, "pazar") || strings.Contains(p, "rakipler") ||
		strings.Contains(p, "teslimat"):
		return "pazar rakipler Türkiye"
	default:
		return ""
	}
}

func isIdentityAsk(text string) bool {
	p := strings.ToLower(text)
	return strings.Contains(p, "biliyor") || strings.Contains(p, "nedir")
}

func isThinFollowUp(s string) bool {
	return len(strings.Fields(strings.TrimSpace(s))) <= 5
}

// researchQueries returns the primary live-search string and an optional
// entity-only fallback. The first user bubble anchors the entity; later turns
// keep that anchor so a one-word reply still researches the right brand.
func researchQueries(history []Turn, latest string) (primary, fallback string) {
	if isSelfAsk(latest) {
		return "", ""
	}

	first := deriveSubject(history)
	topic, purpose, rest := parseIntake(first)
	body := rest
	if body == "" {
		body = first
	}

	// Bare paste: www.visevent.com — search the host, then site:host so an
	// academic namesake does not crowd out the real homepage.
	if host, ok := domainOnlyReport(latest); ok {
		return host, "site:" + host
	}
	if host, ok := domainOnlyReport(body); ok && latest == first {
		return host, "site:" + host
	}

	entity := researchEntity(topic, body)
	if entity == "" {
		entity = firstWords(latest, 12)
	}
	entity = stripAskNoise(entity)
	if entity == "" {
		entity = firstWords(stripAskNoise(body), 8)
	}
	if d := firstDomain(entity); d != "" {
		entity = d
	}

	hint := researchHint(purpose)
	if hint == "" {
		hint = researchHint(body)
	}
	if hint == "" {
		hint = researchHint(latest)
	}
	identityAsk := hint == "" && (isIdentityAsk(body) || isIdentityAsk(latest))

	switch {
	case latest != first && strings.TrimSpace(latest) != "":
		cleaned := stripAskNoise(latest)
		if isThinFollowUp(cleaned) {
			// "başka markalar yok mu" must keep the thread's market, not search
			// only those three words.
			primary = strings.TrimSpace(entity + " " + hint + " rakipler alternatif " + cleaned)
		} else {
			primary = strings.TrimSpace(entity + " " + firstWords(cleaned, 10))
		}
	case identityAsk:
		primary = entity
	default:
		primary = strings.TrimSpace(entity + " " + hint)
	}
	primary = truncate(strings.Join(strings.Fields(primary), " "), 200)
	fallback = truncate(entity, 120)
	if fallback == "" || fallback == primary {
		return primary, ""
	}
	return primary, fallback
}

// preferHostMatch moves results whose URL host equals prefer to the front so
// visevent.com beats arxiv.org when the operator named the site.
func preferHostMatch(results []SearchResult, prefer string) []SearchResult {
	prefer = normalizeHost(prefer)
	if prefer == "" || len(results) < 2 {
		return results
	}
	var match, other []SearchResult
	for _, r := range results {
		if hostOf(r.URL) == prefer {
			match = append(match, r)
		} else {
			other = append(other, r)
		}
	}
	if len(match) == 0 {
		return results
	}
	return append(match, other...)
}

func anyHostMatch(results []SearchResult, prefer string) bool {
	prefer = normalizeHost(prefer)
	if prefer == "" {
		return false
	}
	for _, r := range results {
		if hostOf(r.URL) == prefer {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return normalizeHost(raw)
	}
	return normalizeHost(u.Host)
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

// Words that pad a chat ask but poison a search query.
var askNoise = map[string]struct{}{
	"biliyor": {}, "biliyormusun": {}, "biliyormusun?": {},
	"musun": {}, "musun?": {}, "misin": {}, "misin?": {},
	"nedir": {}, "nedir?": {}, "hakkında": {}, "hakkinda": {},
	"ne": {}, "nasıl": {}, "nasil": {}, "için": {}, "icin": {},
	"bir": {}, "bu": {}, "şu": {}, "su": {}, "var": {}, "mı": {}, "mi": {},
	"yok": {}, "mu": {}, "mü": {}, "mu?": {}, "mü?": {},
	"app'i": {}, "appi": {}, "uygulaması": {}, "uygulamasi": {},
	"uygulama": {}, "app": {},
	"görünüyor": {}, "gorunuyor": {}, "bakalım": {}, "bakalim": {},
}

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
