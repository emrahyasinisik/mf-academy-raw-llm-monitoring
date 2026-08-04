# Persona + yan rapor paneli — uygulama planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persona sohbetinde verdict sonrası onayla rubrik raporu üretmek ve Cursor tarzı resizable sağ panelde göstermek; thread’e `assessment_id` bağlamak.

**Architecture:** FE orkestrasyon — intake + chat → CTA → `POST /analysis/run` → `ReportPanel` içinde paylaşılan `Rapor`. BE yalnızca `conversations.assessment_id` + genişletilmiş PATCH. Yeni analysis/decision orchestrator endpoint yok.

**Tech Stack:** Go 1.26.5 (chi, pgx v5), PostgreSQL, Next.js 16 + React, TypeScript, Tailwind.

**Spec:** [`docs/superpowers/specs/2026-08-04-persona-yan-rapor-design.md`](../specs/2026-08-04-persona-yan-rapor-design.md)

## Global Constraints

- **Veritabanı destekli test YOK.** Store SQL’i deploy’da doğrulanır; handler/pure logic fake’lerle test edilir.
- **Frontend testleri yalnızca `src/lib/*.test.ts`** (`npm test`). Bileşen testi yok; UI `npm run lint` + `npm run build`.
- **API alan adları snake_case** (`assessment_id`, `subject_title`).
- **UI metinleri Türkçe.** Donanım / GPU / tünel kullanıcıya görünmez. Dark-only.
- **Domain v1 sabit:** `startup-investability`.
- **Onaysız analysis yok.** Verdict CTA → kullanıcı onayı → run.
- **Yeni mimari yok** (autoscaling, ikinci orchestrator route, junction tablosu yok).
- Comments explain WHY.
- Branches are never deleted.

## File map

| Path | Responsibility |
|---|---|
| `mf-backend/migrations/015_persona_assessment.sql` | `conversations.assessment_id` FK |
| `mf-backend/internal/decision/store.go` | Read/write `assessment_id` on list/get/patch |
| `mf-backend/internal/decision/handler.go` | PATCH title ve/veya assessment_id; ownership |
| `mf-backend/internal/decision/patch.go` | Pure PATCH body parse/validate (testable) |
| `mf-backend/internal/decision/patch_test.go` | PATCH validation + handler tests |
| `mf-backend/internal/analysis/store.go` | `OwnsAssessment` |
| `mf-backend/internal/decision/persona.go` | Intake checklist in system prompt |
| `mf-backend/cmd/server/main.go` | Wire AssessmentOwner into decision handler |
| `mf-frontend/src/lib/types.ts` | `assessment_id` on Conversation* |
| `mf-frontend/src/lib/api.ts` | `patchConversation` |
| `mf-frontend/src/lib/personaCase.ts` | Assemble + truncate case subject |
| `mf-frontend/src/lib/personaCase.test.ts` | Case assembly tests |
| `mf-frontend/src/lib/reportPanelWidth.ts` | Clamp + localStorage width |
| `mf-frontend/src/lib/reportPanelWidth.test.ts` | Width helper tests |
| `mf-frontend/src/components/ui/Rapor.tsx` | Extracted report UI |
| `mf-frontend/src/components/ui/ReportPanel.tsx` | Resizable right shell |
| `mf-frontend/src/components/ui/IntakeFields.tsx` | Konu + Amaç |
| `mf-frontend/src/components/views/AnalizView.tsx` | Import shared `Rapor` |
| `mf-frontend/src/components/views/PersonaView.tsx` | Three-column workspace orchestration |

---

### Task 1: Migration + store `assessment_id`

**Files:**
- Create: `mf-backend/migrations/015_persona_assessment.sql`
- Modify: `mf-backend/internal/decision/store.go`
- Modify: `mf-backend/internal/analysis/store.go` (add `OwnsAssessment`)

**Interfaces:**
- Consumes: existing `conversations`, `assessments` tables
- Produces:
  - `ConversationSummary.AssessmentID *string \`json:"assessment_id,omitempty"\``
  - `Conversation.AssessmentID *string \`json:"assessment_id,omitempty"\``
  - `(*Store).SetAssessmentID(ctx, userID, conversationID string, assessmentID *string) error` — `nil` clears
  - `(*analysis.Store).OwnsAssessment(ctx, userID, id string) (bool, error)`

- [ ] **Step 1: Write migration**

```sql
-- 015_persona_assessment.sql — link a persona thread to its latest rubric report.
--
-- The persona conversation gathers the case; analysis/run produces the auditable
-- report. Without this column the side panel cannot reopen "this thread's
-- report" after a reload. ON DELETE SET NULL: deleting the assessment row must
-- not delete the conversation the operator still wants to read. Redaction does
-- not DELETE the row — the FE clears the link when GET returns redacted/404.

ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS assessment_id UUID
    REFERENCES assessments(id) ON DELETE SET NULL;
```

- [ ] **Step 2: Extend store structs and SELECT lists**

In `List` and `Get` SELECT, add `c.assessment_id`. Scan into `*string` (pgx → nil for NULL).

- [ ] **Step 3: Add `SetAssessmentID`**

```go
// SetAssessmentID links or clears the report attached to a thread the user owns.
// assessmentID == nil clears the column.
func (s *Store) SetAssessmentID(ctx context.Context, userID, conversationID string, assessmentID *string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE conversations SET assessment_id = $3
		  WHERE id = $1 AND user_id = $2`,
		conversationID, userID, assessmentID)
	// RowsAffected == 0 → ErrNoRows
	...
}
```

Keep `Rename` as-is for now; Task 2 will call SetAssessmentID from Patch.

- [ ] **Step 4: Add `OwnsAssessment` on analysis store**

```go
func (s *Store) OwnsAssessment(ctx context.Context, userID, id string) (bool, error) {
	var one int
	err := s.db.QueryRow(ctx,
		`SELECT 1 FROM assessments WHERE id = $1 AND user_id = $2`, id, userID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
```

- [ ] **Step 5: Compile**

Run: `cd mf-backend && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add mf-backend/migrations/015_persona_assessment.sql \
  mf-backend/internal/decision/store.go \
  mf-backend/internal/analysis/store.go
git commit -m "$(cat <<'EOF'
feat(decision): link conversations to assessments

EOF
)"
```

---

### Task 2: PATCH handler (title ve/veya assessment_id)

**Files:**
- Create: `mf-backend/internal/decision/patch.go`
- Create: `mf-backend/internal/decision/patch_test.go`
- Modify: `mf-backend/internal/decision/handler.go`
- Modify: `mf-backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `Store.Rename`, `Store.SetAssessmentID`, `analysis.Store.OwnsAssessment`
- Produces:
  - `type AssessmentOwner interface { OwnsAssessment(ctx context.Context, userID, id string) (bool, error) }`
  - `NewHandler(agent *Agent, store *Store, assessments AssessmentOwner) *Handler`
  - `Patch` replaces `Rename` on `PATCH /decision/conversations/{id}`
  - `parsePatchBody(data []byte) (patchOp, error)` where:

```go
type patchOp struct {
	Title            *string // set title when non-nil
	SetAssessment    bool    // true if assessment_id key was present
	AssessmentID     *string // nil means clear when SetAssessment
}
```

- [ ] **Step 1: Write failing tests for `parsePatchBody`**

```go
func TestParsePatchBodyTitleOnly(t *testing.T) {
	op, err := parsePatchBody([]byte(`{"title":"Acme"}`))
	if err != nil { t.Fatal(err) }
	if op.Title == nil || *op.Title != "Acme" { t.Fatalf("title=%v", op.Title) }
	if op.SetAssessment { t.Fatal("assessment must be omitted") }
}

func TestParsePatchBodyClearAssessment(t *testing.T) {
	op, err := parsePatchBody([]byte(`{"assessment_id":null}`))
	if err != nil { t.Fatal(err) }
	if !op.SetAssessment || op.AssessmentID != nil {
		t.Fatalf("want clear, got %+v", op)
	}
}

func TestParsePatchBodyEmptyRejected(t *testing.T) {
	_, err := parsePatchBody([]byte(`{}`))
	if err == nil { t.Fatal("expected error") }
}

func TestParsePatchBodyBlankTitleRejected(t *testing.T) {
	_, err := parsePatchBody([]byte(`{"title":"  "}`))
	if err == nil { t.Fatal("expected error") }
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `cd mf-backend && go test ./internal/decision/ -run TestParsePatchBody -count=1`
Expected: FAIL (undefined `parsePatchBody`).

- [ ] **Step 3: Implement `parsePatchBody`**

Use `json.Decoder` / raw map, or unmarshal into:

```go
var raw map[string]json.RawMessage
```

- If `title` key present → unmarshal string, trim, reject empty.
- If `assessment_id` key present → if raw is `null` then clear; else unmarshal UUID string.
- If neither key → error `"title or assessment_id is required"`.

- [ ] **Step 4: Run parse tests — expect PASS**

Run: `cd mf-backend && go test ./internal/decision/ -run TestParsePatchBody -count=1`
Expected: PASS.

- [ ] **Step 5: Write handler tests with fakes**

```go
type fakeConvStore struct {
	// embed nothing — only methods Patch needs via a narrow interface, OR
	// keep Handler calling store.Rename + store.SetAssessmentID and use a
	// test-only stub type that the handler accepts through an interface.
}
```

Preferred shape (keeps production `*Store`): extract a small interface used only by Patch tests is optional. Mirror `analysis/handler_test.go`: define `patchStore` with `Rename` + `SetAssessmentID`, change Handler to hold:

```go
type conversationPatchStore interface {
	Rename(ctx context.Context, userID, id, title string) error
	SetAssessmentID(ctx context.Context, userID, id string, assessmentID *string) error
}
```

Production `*Store` already satisfies it. Handler field type = interface for testability **only if** it does not churn the whole Handler — alternatively keep `*Store` and test only `parsePatchBody` + a thin `applyPatch(op, …)` pure function that returns the calls to make. **Choose the thin `applyPatch` path if Handler wiring fights you; do not invent a second store package.**

Minimum required handler-level coverage via httptest (like auth/analysis):

```go
func TestPatchSetsAssessmentWhenOwned(t *testing.T) { ... expect 204 ... }
func TestPatchRejectsForeignAssessment(t *testing.T) { ... expect 404 ... }
func TestPatchClearsAssessment(t *testing.T) { ... expect 204, SetAssessmentID nil ... }
```

Use `common.ContextWithClaims` + chi URL param as in `analysis/handler_test.go`.

- [ ] **Step 6: Implement Patch; rename Rename handler → Patch; keep route**

```go
func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	// decode raw body → parsePatchBody
	// if SetAssessment && AssessmentID != nil → OwnsAssessment; false → 404
	// if Title != nil → store.Rename
	// if SetAssessment → store.SetAssessmentID
	// 204
}
```

Wire: `sr.Patch("/conversations/{id}", h.Patch)`.

- [ ] **Step 7: Update `NewHandler` + main.go**

```go
decisionHandler := decision.NewHandler(decisionAgent, decisionStore, analysisStore)
```

- [ ] **Step 8: Run all decision + build**

Run: `cd mf-backend && go test ./internal/decision/ ./internal/analysis/ -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add mf-backend/internal/decision/patch.go \
  mf-backend/internal/decision/patch_test.go \
  mf-backend/internal/decision/handler.go \
  mf-backend/cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(decision): patch conversation assessment link

EOF
)"
```

---

### Task 3: Persona prompt — sabit intake checklist

**Files:**
- Modify: `mf-backend/internal/decision/persona.go`
- Modify: `mf-backend/internal/decision/history_test.go` only if prompt constants are asserted (usually not)

**Interfaces:**
- Consumes: existing `personaSystemPrompt`, `turnInstruction`
- Produces: updated prompt text (no new Go API)

- [ ] **Step 1: Extend DAVRANIŞ / checklist in `personaSystemPrompt`**

Keep **tur başına tek soru**. Add explicit checklist the model must pick from before verdict:

```
NETLEŞTİRME (sırayla, tur başına TEK soru; listedekilerden seç, uydurma):
1. Aşama (pre-seed / seed / A / …)
2. Coğrafya / odak pazar
3. Bütçe veya ticket büyüklüğü
4. Zaman ufku (ne kadar sürede karar)
Kullanıcı zaten cevapladıysa o maddeyi atla. Dördü de netse ve kanıt yeterliyse nihai KARAR ver.
```

Do **not** remove evidence rules or KARAR/SKOR/GEREKÇE format.

- [ ] **Step 2: Align `turnInstruction` one line**

Mention: netleştirme checklist’inden tek soru veya nihai karar.

- [ ] **Step 3: Run decision tests**

Run: `cd mf-backend && go test ./internal/decision/ -count=1`
Expected: PASS (verdict parser unchanged).

- [ ] **Step 4: Commit**

```bash
git add mf-backend/internal/decision/persona.go
git commit -m "$(cat <<'EOF'
feat(decision): fixed clarifying checklist before verdict

EOF
)"
```

---

### Task 4: FE lib — types, API, case assembly, panel width

**Files:**
- Modify: `mf-frontend/src/lib/types.ts`
- Modify: `mf-frontend/src/lib/api.ts`
- Create: `mf-frontend/src/lib/personaCase.ts`
- Create: `mf-frontend/src/lib/personaCase.test.ts`
- Create: `mf-frontend/src/lib/reportPanelWidth.ts`
- Create: `mf-frontend/src/lib/reportPanelWidth.test.ts`

**Interfaces:**
- Consumes: `caseBudgetChars`, `estimateTokens` from `rubric.ts`; `DecisionSource` types
- Produces:

```ts
// types
assessment_id?: string | null; // on ConversationSummary + Conversation

// api
patchConversation: (
  id: string,
  body: { title?: string; assessment_id?: string | null },
) => Promise<void>;
// keep renameConversation as thin wrapper: patchConversation(id, { title })

export type PersonaCaseInput = {
  topic: string;
  purpose: string;
  userReplies: string[];
  lastAssistantBody: string; // stripVerdictLines already applied by caller
  sources: { title: string; url: string }[];
  budgetChars: number;
};

export function assemblePersonaCase(input: PersonaCaseInput): {
  subject_title: string;
  subject: string;
};

export const REPORT_PANEL_WIDTH_KEY = "persona.reportPanelWidth";
export function clampReportPanelWidth(px: number, viewportWidth: number): number;
export function loadReportPanelWidth(viewportWidth: number): number;
export function saveReportPanelWidth(px: number): void;
```

- [ ] **Step 1: Write failing `personaCase` tests**

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { assemblePersonaCase } from "./personaCase.ts";

test("puts topic in subject_title and keeps purpose section", () => {
  const { subject_title, subject } = assemblePersonaCase({
    topic: "Acme AI",
    purpose: "seed değerlendirme",
    userReplies: ["B2B SaaS"],
    lastAssistantBody: "Pazar büyüyor.",
    sources: [{ title: "Haber", url: "https://example.com" }],
    budgetChars: 10_000,
  });
  assert.equal(subject_title, "Acme AI");
  assert.match(subject, /## Konu\nAcme AI/);
  assert.match(subject, /## Amaç\nseed değerlendirme/);
  assert.match(subject, /## Kaynaklar/);
});

test("truncates middle chat before dropping konu/amaç/kaynaklar", () => {
  const long = "x".repeat(500);
  const { subject } = assemblePersonaCase({
    topic: "T",
    purpose: "P",
    userReplies: [long, long, long],
    lastAssistantBody: long,
    sources: [{ title: "S", url: "https://s.test" }],
    budgetChars: 400,
  });
  assert.ok(subject.length <= 400);
  assert.match(subject, /## Konu\nT/);
  assert.match(subject, /## Amaç\nP/);
  assert.match(subject, /## Kaynaklar/);
});
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd mf-frontend && node --experimental-strip-types --test src/lib/personaCase.test.ts`
(or project’s `npm test` filter if available)

Expected: FAIL module not found.

- [ ] **Step 3: Implement `assemblePersonaCase`**

Build sections; if `subject.length > budgetChars`, shrink `## Sohbet özeti` first (drop oldest user replies, then trim assistant), never drop Konu/Amaç/Kaynaklar headers. If still over, truncate Sohbet özeti with a `…` marker.

- [ ] **Step 4: Write + implement `reportPanelWidth`**

```ts
const MIN = 280;
export function clampReportPanelWidth(px: number, viewportWidth: number): number {
  const max = Math.floor(viewportWidth * 0.55);
  return Math.max(MIN, Math.min(max, Math.round(px)));
}
```

`loadReportPanelWidth`: read `localStorage`, parse int, clamp; default `Math.floor(viewportWidth * 0.38)`.
`saveReportPanelWidth`: `localStorage.setItem(REPORT_PANEL_WIDTH_KEY, String(px))`.
In tests, stub `globalThis.localStorage` with a simple map.

- [ ] **Step 5: Update types + api**

```ts
patchConversation: (id, body) =>
  request<void>(`/decision/conversations/${id}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  }),
renameConversation: (id, title) => api.patchConversation(id, { title }),
```

Careful: `api` object literal — either inline PATCH in both or define `patchConversation` first then reference. Prefer both methods calling the same `request` shape (duplicate one-liner OK; do not create circular `api` ref).

- [ ] **Step 6: Run frontend lib tests**

Run: `cd mf-frontend && npm test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add mf-frontend/src/lib/types.ts mf-frontend/src/lib/api.ts \
  mf-frontend/src/lib/personaCase.ts mf-frontend/src/lib/personaCase.test.ts \
  mf-frontend/src/lib/reportPanelWidth.ts mf-frontend/src/lib/reportPanelWidth.test.ts
git commit -m "$(cat <<'EOF'
feat(frontend): persona case assembly and report panel width helpers

EOF
)"
```

---

### Task 5: Extract `Rapor` + build `ReportPanel`

**Files:**
- Create: `mf-frontend/src/components/ui/Rapor.tsx`
- Create: `mf-frontend/src/components/ui/ReportPanel.tsx`
- Modify: `mf-frontend/src/components/views/AnalizView.tsx` (remove local `Rapor`, import shared)

**Interfaces:**
- Consumes: `Assessment`, `breakdown`, `isRedacted`, `CriterionContinuum`
- Produces:

```tsx
export function Rapor({
  assessment,
  className,
}: {
  assessment: Assessment;
  className?: string;
}): JSX.Element;

export type ReportPanelProps = {
  open: boolean;
  width: number;
  onWidthChange: (px: number) => void;
  onClose: () => void;
  assessment: Assessment | null;
  loading: boolean;
  error: string;
  onRetry?: () => void;
};
export function ReportPanel(props: ReportPanelProps): JSX.Element | null;
```

- [ ] **Step 1: Move `Rapor` (+ local `pct` helper) to `components/ui/Rapor.tsx`**

Export it. Adjust outer spacing: accept optional `className`; default keep `mt-6` for AnalizView compatibility. In panel context caller passes `className="mt-0"` or panel wraps without double margin.

- [ ] **Step 2: AnalizView imports `Rapor` from `@/components/ui/Rapor`**

Delete the private function from AnalizView.

- [ ] **Step 3: Implement `ReportPanel`**

Behavior:
- `open === false` → return `null` (desktop). Mobile drawer: still render when open as fixed inset panel (`lg:static` / `max-lg:fixed max-lg:inset-0 max-lg:z-40`).
- Left edge: `div` resize handle (`w-1 cursor-col-resize`, `aria-orientation="vertical"`).
- `pointerdown` → window `pointermove` / `pointerup`; compute width = `viewportRight - clientX`; call `onWidthChange(clampReportPanelWidth(...))`; on `pointerup` parent persists via existing save helper.
- Header: “Rapor”, close button (“Kapat”), optional overall score from `breakdown` when assessment present.
- Body states: loading (“Rapor üretiliyor…”), error + Yeniden dene, else `<Rapor assessment={…} className="mt-0 border-0 shadow-none" />` or redacted message inside Rapor.
- Width applied as `style={{ width }}` on `lg+`; full width on small screens when open.

- [ ] **Step 4: Lint + build**

Run: `cd mf-frontend && npm run lint && npm run build`
Expected: PASS. AnalizView still renders reports.

- [ ] **Step 5: Commit**

```bash
git add mf-frontend/src/components/ui/Rapor.tsx \
  mf-frontend/src/components/ui/ReportPanel.tsx \
  mf-frontend/src/components/views/AnalizView.tsx
git commit -m "$(cat <<'EOF'
feat(frontend): shared Rapor and resizable ReportPanel

EOF
)"
```

---

### Task 6: PersonaView workspace — intake, CTA, run, hydrate

**Files:**
- Create: `mf-frontend/src/components/ui/IntakeFields.tsx`
- Modify: `mf-frontend/src/components/views/PersonaView.tsx`
- Modify: `mf-frontend/src/components/views/PersonaView.tsx` header comment (research+decision+report)

**Interfaces:**
- Consumes: Task 4–5 APIs/components; `parseVerdict` / `stripVerdictLines`; `api.analysisRun`, `api.analysisGet`, `api.analysisPrompt`, `api.patchConversation`
- Produces: integrated UX per spec (no new exported API)

- [ ] **Step 1: `IntakeFields`**

```tsx
export function IntakeFields(props: {
  topic: string;
  purpose: string;
  onTopic: (v: string) => void;
  onPurpose: (v: string) => void;
  disabled?: boolean;
}): JSX.Element;
```

Two labeled inputs (Konu, Amaç). Turkish labels. No card chrome beyond existing well/input styles in the file.

- [ ] **Step 2: PersonaView state additions**

```ts
const [topic, setTopic] = useState("");
const [purpose, setPurpose] = useState("");
const [panelOpen, setPanelOpen] = useState(false);
const [panelWidth, setPanelWidth] = useState(420);
const [report, setReport] = useState<Assessment | null>(null);
const [reportLoading, setReportLoading] = useState(false);
const [reportError, setReportError] = useState("");
const [linkedAssessmentId, setLinkedAssessmentId] = useState<string | null>(null);
```

On mount / resize: `setPanelWidth(loadReportPanelWidth(window.innerWidth))`.

- [ ] **Step 3: Gate first send on intake**

When `messages.length === 0`, require `topic.trim()` and `purpose.trim()`. Compose first user content:

```ts
const primed = `Konu: ${topic.trim()}\nAmaç: ${purpose.trim()}\n\n${userText}`.trim();
```

Intro openers must also set topic (opener text → topic) or fill topic from opener and leave purpose to be filled — **rule:** opener click sets `topic` to opener string and focuses purpose if empty; does not call `ask` until purpose present. Simpler acceptable rule: openers only fill the composer + topic field; user still adds purpose.

- [ ] **Step 4: Layout**

```tsx
<div className="h-full flex min-h-0">
  <HistoryPanel ... />
  <div className="flex-1 min-w-0 flex flex-col ..."> ... chat ... </div>
  <ReportPanel
    open={panelOpen}
    width={panelWidth}
    onWidthChange={(px) => {
      const c = clampReportPanelWidth(px, window.innerWidth);
      setPanelWidth(c);
      saveReportPanelWidth(c);
    }}
    onClose={() => setPanelOpen(false)}
    assessment={report}
    loading={reportLoading}
    error={reportError}
    onRetry={() => void produceReport()}
  />
</div>
```

Remove `max-w-3xl mx-auto` from the outer chat column when panel open (keep readable `max-w-3xl` inside scroll area).

- [ ] **Step 5: Verdict CTA on last assistant bubble**

If `parseVerdict(content, sources.length)` non-null on the latest assistant message, show button **Rapor üret**. If `linkedAssessmentId` and report loaded, show **Raporu göster** → `setPanelOpen(true)`.

- [ ] **Step 6: `produceReport`**

```ts
async function produceReport() {
  if (!threadId || reportLoading) return;
  setPanelOpen(true);
  setReportLoading(true);
  setReportError("");
  const done = begin("Rapor üretiliyor");
  try {
    const prompt = await api.analysisPrompt("startup-investability");
    // limits: reuse AnalizView pattern — api host limits if needed; budget from prompt length + default window
    const budget = caseBudgetChars(/* windowTokens from machine or 8192 fallback */, prompt.system_prompt.length);
    const lastAssistant = [...messages].reverse().find((m) => m.role === "assistant");
    const body = assemblePersonaCase({
      topic: topic || threads.find(t => t.id === threadId)?.title || "Vaka",
      purpose,
      userReplies: messages.filter(m => m.role === "user").map(m => m.content),
      lastAssistantBody: lastAssistant ? stripVerdictLines(lastAssistant.content) : "",
      sources: (lastAssistant && lastAssistant.role === "assistant" ? lastAssistant.sources : []).map(s => ({
        title: s.title || s.url,
        url: s.url,
      })),
      budgetChars: budget,
    });
    const a = await api.analysisRun({
      domain: "startup-investability",
      subject_title: body.subject_title,
      subject: body.subject,
    });
    setReport(a);
    setLinkedAssessmentId(a.id);
    await api.patchConversation(threadId, { assessment_id: a.id });
    setThreads(prev => prev.map(t => t.id === threadId ? { ...t, assessment_id: a.id } : t));
  } catch (e) {
    setReportError(e instanceof ApiError ? e.message : "Rapor üretilemedi.");
  } finally {
    setReportLoading(false);
    done();
  }
}
```

While `reportLoading`, disable composer send (spec).

- [ ] **Step 7: Hydrate on `openThread`**

When conversation loads:
- set `linkedAssessmentId` from `c.assessment_id ?? null`
- restore topic/purpose by parsing first user message lines `Konu:` / `Amaç:` if present (small helper in `personaCase.ts`: `parseIntake(content) => {topic, purpose, rest}`)
- if `assessment_id`: `setPanelOpen(true)`, `analysisGet`; on redacted/404 → clear link via `patchConversation(id, { assessment_id: null })`, show panel message through `reportError` or redacted `Rapor`

- [ ] **Step 8: `newThread` resets intake + panel state**

- [ ] **Step 9: Lint + build + lib tests**

Run: `cd mf-frontend && npm test && npm run lint && npm run build`
Expected: PASS.

Run: `cd mf-backend && go test ./... -count=1 && go build -o app ./cmd/server`
Expected: PASS.

- [ ] **Step 10: Manual smoke (operator)**

1. `#persona` → Konu+Amaç → sohbet → verdict → Rapor üret → panel + scores.
2. Drag resize; reload → width persists; thread reopen → panel hydrates.
3. `#analiz` form hâlâ rapor basıyor.
4. Dar viewport: panel overlay.

- [ ] **Step 11: Commit**

```bash
git add mf-frontend/src/components/ui/IntakeFields.tsx \
  mf-frontend/src/components/views/PersonaView.tsx \
  mf-frontend/src/lib/personaCase.ts mf-frontend/src/lib/personaCase.test.ts
git commit -m "$(cat <<'EOF'
feat(frontend): persona workspace with side rubric report

EOF
)"
```

---

## Spec coverage checklist

| Spec requirement | Task |
|---|---|
| History \| Chat \| ReportPanel layout | 6 |
| Resizable panel + localStorage | 4, 5, 6 |
| Intake Konu + Amaç | 6 |
| Checklist clarifying Qs (one/turn) | 3 |
| Hybrid CTA after verdict | 6 |
| `analysis/run` + shared Rapor | 5, 6 |
| `assessment_id` migration + PATCH | 1, 2 |
| Hydrate / clear on redacted | 6 |
| `#analiz` preserved | 5 |
| Domain fixed startup-investability | 6 |
| Non-goals respected | — |

## Placeholder / consistency self-review

- No TBD left in tasks.
- `patchConversation` / `SetAssessmentID` / `assessment_id` naming consistent FE↔BE.
- `NewHandler` third arg wired in Task 2 before PersonaView depends on PATCH.
- `parseIntake` added in Task 6 Step 7 — implement in `personaCase.ts` in that step (add a focused unit test in the same step before wiring).
