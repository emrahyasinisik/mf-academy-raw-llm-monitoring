package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/emrah/mf-backend/internal/wiki"
)

// DeepKwiki over MCP.
//
// Two tools, because a client's model wants two different things from a
// knowledge base and conflating them costs it either accuracy or context:
//
//	search_wiki  raw passages. The model reads them itself and reasons over
//	             them with its own — usually much larger — capacity.
//	ask_wiki     an answer already produced by the local 2B model.
//
// search_wiki is the better tool for a capable client and is described so that
// a model picks it by default. ask_wiki exists because an agent orchestrating
// many calls does not always want to spend its own context on raw text, and
// because it is the same code path the browser uses — so what an agent gets and
// what a person sees cannot drift apart.

// Librarian is DeepKwiki as this server needs it, declared consumer-side.
// *wiki.Handler satisfies it.
type Librarian interface {
	Lookup(ctx context.Context, query string, limit int) ([]wiki.Hit, error)
	Answer(ctx context.Context, req wiki.AskRequest) (wiki.Answer, error)
}

// wikiInstructions is appended to the server's instructions when a knowledge
// base is wired. Like the analysis warning, it is short and mostly about the
// one way this can be misread: treating an ungrounded answer as sourced.
const wikiInstructions = `

Bilgi tabanı (DeepKwiki) araçları:
- search_wiki ham pasajları döndürür. Kendi muhakemeni yapacaksan bunu kullan.
- ask_wiki yerel küçük modelin ürettiği yanıtı döndürür; hızlıdır ama 2B bir
  modelin çıktısıdır.
- grounded=false ise yanıt hiçbir kaynağa dayanmıyordur. Böyle bir yanıtı
  bilgi tabanının cevabı gibi AKTARMA.
- no_results=true, bilgi tabanının bu konuyu kapsamadığı anlamına gelir. Bu bir
  hata değildir ve kendi genel bilginle doldurulmamalıdır.
- matched alanı pasajın nasıl bulunduğunu söyler: "all" tüm terimler geçiyor,
  "any" bir kısmı, "fuzzy" sadece benziyor. "fuzzy" sonuçlara ihtiyatlı yaklaş.`

func wikiTools() []Tool {
	return []Tool{
		{
			Name:  "search_wiki",
			Title: "Bilgi tabanında ara",
			Description: "Bilgi tabanındaki belgelerde arama yapar ve eşleşen pasajları " +
				"birebir döndürür. Kendi muhakemeni yapmak istiyorsan tercih et.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Aranacak ifade veya soru",
						"minLength":   2,
					},
					"limit": map[string]any{
						"type": "integer", "minimum": 1, "maximum": 20, "default": 8,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:  "ask_wiki",
			Title: "Bilgi tabanına soru sor",
			Description: "Soruyu bilgi tabanındaki belgelere dayanarak yanıtlar ve kullandığı " +
				"pasajları kaynak olarak döndürür. Yanıtı yerel küçük model üretir; " +
				"grounded=false ise yanıt hiçbir kaynağa dayanmamaktadır.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Sorulacak soru",
						"minLength":   2,
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (s *Server) searchWiki(ctx context.Context, args json.RawMessage) callResult {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errorResult(fmt.Sprintf("argümanlar okunamadı: %v", err))
	}
	if in.Query == "" {
		return errorResult("query zorunludur")
	}

	hits, err := s.wiki.Lookup(ctx, in.Query, in.Limit)
	if err != nil {
		return errorResult("arama başarısız oldu")
	}

	// Projected, and the snippet is dropped. It carries « » markers meant for
	// highlighting in a browser; a model reading them would either quote the
	// markup or treat it as part of the source text.
	type passage struct {
		Document string  `json:"document"`
		Title    string  `json:"title"`
		Heading  string  `json:"heading"`
		Ordinal  int     `json:"ordinal"`
		Body     string  `json:"body"`
		Rank     float64 `json:"rank"`
		Matched  string  `json:"matched"`
	}
	out := make([]passage, 0, len(hits))
	for _, h := range hits {
		out = append(out, passage{
			Document: h.DocumentSlug, Title: h.Title, Heading: h.Heading,
			Ordinal: h.Ordinal, Body: h.Body, Rank: h.Rank, Matched: h.Matched,
		})
	}

	if len(out) == 0 {
		// Said in words as well as in an empty array. A model handed `[]` tends
		// to retry with a rephrased query; told the corpus does not cover this,
		// it stops and says so.
		return textResult(map[string]any{
			"query":    in.Query,
			"passages": out,
			"count":    0,
			"note":     "Bilgi tabanında bu sorguyla eşleşen pasaj yok. Kendi genel bilginle doldurma.",
		})
	}
	return textResult(map[string]any{"query": in.Query, "passages": out, "count": len(out)})
}

func (s *Server) askWiki(ctx context.Context, args json.RawMessage) callResult {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return errorResult(fmt.Sprintf("argümanlar okunamadı: %v", err))
	}
	if in.Query == "" {
		return errorResult("query zorunludur")
	}

	ans, err := s.wiki.Answer(ctx, wiki.AskRequest{Query: in.Query})
	if err != nil {
		// The inference host being switched off is a state the operator knows
		// about, not a fault the model should retry around — so it is reported
		// as a message the model can pass on rather than a bare failure.
		return errorResult("yanıt üretilemedi: " + err.Error())
	}

	// The full source bodies are returned alongside the answer, not just their
	// titles. The whole point is that the caller can check the answer against
	// the text it came from, and a citation the reader cannot open is the thing
	// this feature exists to avoid.
	return textResult(ans)
}
