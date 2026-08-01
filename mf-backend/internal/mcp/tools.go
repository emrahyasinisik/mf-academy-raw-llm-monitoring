package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/emrah/mf-backend/internal/analysis"
)

// Analyzer is the analysis engine as this server needs it, declared
// consumer-side. *analysis.Handler satisfies it.
type Analyzer interface {
	ExecuteAnalysis(ctx context.Context, userID string, req analysis.AnalyzeRequest) (analysis.Assessment, error)
	Catalogue(ctx context.Context) ([]analysis.Domain, error)
	Report(ctx context.Context, userID, id string) (analysis.Assessment, error)
	Reports(ctx context.Context, userID, domainSlug string, limit int) (analysis.ListResult, error)
}

// instructions is handed to the client's model as context about this server.
//
// It is short and it is mostly one warning, because there is exactly one way to
// misread a report from this system and it is the obvious way: quoting the
// score without the coverage. A model told nothing will do that every time, and
// "this startup scored 77" is a materially different claim from "77, on the two
// thirds of the rubric the deck actually addressed".
const instructions = `Bu sunucu, bir vakayı tanımlı bir rubriğe göre puanlar.

Raporları okurken:
- overall_score ASLA tek başına aktarılmaz. Yanında mutlaka coverage verilir.
  0.9 coverage'da 75 güçlü bir vakadır; 0.3 coverage'da 75, rubriğin sadece
  üçte birine değinmiş bir vakadır. İkisi aynı sayı, farklı sonuçtur.
- evidence_found=false olan kriterler puana HİÇ dahil edilmez, sıfır sayılmaz.
  "Bu kriter kötü" değil, "vaka bu konuya değinmemiş" demektir.
- overall_score null ise vaka değerlendirilememiştir. Bu sıfır değildir.
- Her finding'in evidence dizisi vakadan birebir alıntıdır; bir iddiayı
  aktarırken dayanağını da göster.

Puanı model üretmez: model kriterleri kanıtla doldurur, ağırlıklı toplamı
sunucu deterministik olarak hesaplar. Aynı finding'ler her zaman aynı puanı
verir.`

// tools is the callable surface, fixed at compile time.
//
// Four operations, not a wrapper around every HTTP route. An MCP tool list is
// read by a model choosing what to call, and a long list of near-identical
// entries makes that choice worse, not better — every tool here answers a
// question somebody would actually ask.
func (s *Server) tools() []Tool {
	out := []Tool{
		{
			Name:        "list_analysis_domains",
			Title:       "Kullanılabilir rubrikleri listele",
			Description: "Analiz için tanımlı rubrikleri (alanları) ve her birinin kriterlerini döner. analyze_case çağırmadan önce hangi domain'in kullanılacağını buradan seç.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:  "analyze_case",
			Title: "Bir vakayı rubriğe göre analiz et",
			Description: "Verilen metni seçilen rubriğin her kriteri için değerlendirir ve kanıtlı, " +
				"kriter kırılımlı bir rapor döner. Yavaştır: yerel bir GPU'da çalışır ve bir dakikayı " +
				"aşabilir. Sonucu aktarırken overall_score'u coverage ile birlikte ver.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"domain": map[string]any{
						"type":        "string",
						"description": "Rubrik slug'ı; list_analysis_domains ile öğrenilir",
					},
					"subject": map[string]any{
						"type":        "string",
						"description": "Analiz edilecek vaka metni (örn. bir pitch deck'in metni)",
						"minLength":   40,
					},
					"subject_title": map[string]any{
						"type":        "string",
						"description": "Raporu tanımlayan kısa başlık",
					},
				},
				"required": []string{"domain", "subject"},
			},
		},
		{
			Name:        "get_report",
			Title:       "Bir raporu oku",
			Description: "Daha önce üretilmiş bir raporu id ile döner; kriter kırılımı ve alıntılar dahil.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "Rapor id'si"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "list_reports",
			Title:       "Raporları listele",
			Description: "Bu hesabın son raporlarını özet olarak listeler (puan, coverage, tarih).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"domain": map[string]any{"type": "string", "description": "İsteğe bağlı rubrik filtresi"},
					"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 10},
				},
			},
		},
	}

	// Appended rather than declared inline so a deployment without a knowledge
	// base advertises exactly the four analysis tools, unchanged.
	if s.wiki != nil {
		out = append(out, wikiTools()...)
	}
	return out
}

// call dispatches one tool invocation.
//
// Every failure below returns a callResult with IsError set rather than a
// JSON-RPC error. The distinction is load-bearing: a JSON-RPC error is consumed
// by the client's transport and never shown to the model, so a model that asked
// for a nonexistent domain would receive silence and retry the same call. As an
// isError result it reads the message and can correct itself.
func (s *Server) call(ctx context.Context, userID, name string, args json.RawMessage) callResult {
	switch name {
	case "list_analysis_domains":
		return s.listDomains(ctx)
	case "analyze_case":
		return s.analyze(ctx, userID, args)
	case "get_report":
		return s.getReport(ctx, userID, args)
	case "list_reports":
		return s.listReports(ctx, userID, args)
	case "search_wiki", "ask_wiki":
		// Guarded rather than assumed reachable. These names are only ever
		// advertised when a knowledge base is wired, but a client may call a
		// tool it remembers from an earlier session, and the honest answer is a
		// message rather than a nil dereference.
		if s.wiki == nil {
			return errorResult("bu dağıtımda bilgi tabanı yapılandırılmamış")
		}
		if name == "search_wiki" {
			return s.searchWiki(ctx, args)
		}
		return s.askWiki(ctx, args)
	default:
		return errorResult(fmt.Sprintf("bilinmeyen araç: %q", name))
	}
}

func (s *Server) listDomains(ctx context.Context) callResult {
	domains, err := s.analyzer.Catalogue(ctx)
	if err != nil {
		return errorResult("rubrikler okunamadı")
	}

	// Projected rather than returned whole. A rubric carries the guidance text
	// written for the analysing model — several hundred words per criterion —
	// and putting that in front of a *client's* model wastes its context on
	// instructions meant for a different model entirely.
	type crit struct {
		Key         string  `json:"key"`
		Label       string  `json:"label"`
		Weight      float64 `json:"weight"`
		ScaleMax    int     `json:"scale_max"`
		Description string  `json:"description"`
	}
	type dom struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     int    `json:"version"`
		Criteria    []crit `json:"criteria"`
	}

	out := make([]dom, 0, len(domains))
	for _, d := range domains {
		c := make([]crit, 0, len(d.Criteria))
		for _, x := range d.Criteria {
			c = append(c, crit{x.Key, x.Label, x.Weight, x.EffectiveScaleMax(), x.Description})
		}
		out = append(out, dom{d.Slug, d.Name, d.Description, d.Version, c})
	}
	return textResult(map[string]any{"domains": out})
}

func (s *Server) analyze(ctx context.Context, userID string, args json.RawMessage) callResult {
	var in struct {
		Domain       string `json:"domain"`
		Subject      string `json:"subject"`
		SubjectTitle string `json:"subject_title"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errorResult(fmt.Sprintf("argümanlar okunamadı: %v", err))
	}
	if strings.TrimSpace(in.Domain) == "" {
		return errorResult("domain gerekli; list_analysis_domains ile seç")
	}
	if strings.TrimSpace(in.Subject) == "" {
		return errorResult("subject gerekli: analiz edilecek vaka metni")
	}

	a, err := s.analyzer.ExecuteAnalysis(ctx, userID, analysis.AnalyzeRequest{
		Domain:       in.Domain,
		Subject:      in.Subject,
		SubjectTitle: in.SubjectTitle,
	})
	if err != nil {
		// The engine's errors already carry a message written for a person —
		// "the inference host is not configured", "no such analysis domain" —
		// and those are exactly what a model needs to choose its next move.
		return errorResult(err.Error())
	}
	return textResult(reportView(a))
}

func (s *Server) getReport(ctx context.Context, userID string, args json.RawMessage) callResult {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errorResult(fmt.Sprintf("argümanlar okunamadı: %v", err))
	}
	if in.ID == "" {
		return errorResult("id gerekli")
	}

	a, err := s.analyzer.Report(ctx, userID, in.ID)
	if errors.Is(err, analysis.ErrNoRows) {
		return errorResult("böyle bir rapor yok")
	}
	if err != nil {
		return errorResult("rapor okunamadı")
	}
	return textResult(reportView(a))
}

func (s *Server) listReports(ctx context.Context, userID string, args json.RawMessage) callResult {
	var in struct {
		Domain string `json:"domain"`
		Limit  int    `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return errorResult(fmt.Sprintf("argümanlar okunamadı: %v", err))
		}
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	if in.Limit > 50 {
		in.Limit = 50
	}

	res, err := s.analyzer.Reports(ctx, userID, in.Domain, in.Limit)
	if err != nil {
		return errorResult("raporlar listelenemedi")
	}
	return textResult(map[string]any{
		"reports": res.Assessments,
		"count":   len(res.Assessments),
	})
}

// reportView is what a report looks like to an MCP client.
//
// The raw response and the full case text are dropped: the first is a debugging
// artefact and the second is what the caller just sent. Both would be large,
// and context spent echoing an input back is context not spent on the answer.
//
// coverage_note is generated rather than left to the reader. The instructions
// above already say a score must not travel without its coverage, but
// instructions are advisory and a sentence attached to the number itself is
// not — it travels with the figure into whatever the model writes next.
//
// redacted_at is carried even though it is empty on almost every report. This
// is a hand-built projection, so a column added to Assessment does not appear
// here on its own — and the one column whose absence actively misleads is this
// one. A redacted report keeps its score and its coverage but has an empty
// findings list, which reads exactly like a report with nothing to say.
func reportView(a analysis.Assessment) map[string]any {
	view := map[string]any{
		"id":             a.ID,
		"domain":         a.DomainSlug,
		"domain_version": a.DomainVersion,
		"subject_title":  a.SubjectTitle,
		"overall_score":  a.OverallScore,
		"coverage":       a.Coverage,
		"coverage_note":  coverageNote(a),
		"findings":       a.Findings,
		"model":          a.Model,
		"created_at":     a.CreatedAt,
		"redacted_at":    a.RedactedAt,
	}
	if !a.SchemaValid {
		// Surfaced, not hidden. A report the model had to be coaxed into
		// shape is still worth reading, but a client relaying it to somebody
		// making a funding decision should know it was not clean.
		view["schema_warning"] = "Model çıktısı şemaya birebir uymadı ve onarıldı; " +
			"bu raporu aktarırken dikkatli ol."
	}
	return view
}

func coverageNote(a analysis.Assessment) string {
	// First, before anything derived from Findings. Redaction empties that list
	// while leaving the score and the coverage in place, so every count below
	// reads zero unassessed criteria and the note would announce "the whole
	// rubric was evidenced" about a report whose evidence no longer exists —
	// the most confident sentence this function can produce, attached to the
	// one case where nothing can be checked.
	if a.RedactedAt != nil {
		return "Bu raporun içeriği silindi: puan ve kapsam duruyor, ama kriter " +
			"kırılımı ve kanıt alıntıları kaldırıldı. Puanın neye dayandığı " +
			"artık gösterilemiyor; bu sayıyı aktarırken bunu da söyle."
	}
	if a.OverallScore == nil {
		return "Hiçbir kriter değerlendirilemedi; bu bir sıfır puan DEĞİL, puanlanamamış bir vakadır."
	}
	unassessed := 0
	for _, f := range a.Findings {
		if !f.EvidenceFound {
			unassessed++
		}
	}
	switch {
	case unassessed == 0:
		return "Rubriğin tamamı kanıtlandı."
	case a.Coverage < 0.6:
		return fmt.Sprintf(
			"DİKKAT: %d kriter vakada hiç geçmiyor ve puana dahil edilmedi (coverage %.2f). "+
				"Bu puan, rubriğin yalnızca bir bölümü hakkındadır.", unassessed, a.Coverage)
	default:
		return fmt.Sprintf(
			"%d kriter vakada geçmiyor ve puana dahil edilmedi (coverage %.2f).",
			unassessed, a.Coverage)
	}
}
