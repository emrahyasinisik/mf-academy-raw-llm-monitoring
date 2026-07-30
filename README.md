
# Rubric Analysis Engine

<!-- GitHub strips <iframe>, so the badge is a linked <img> (both are allowed by GitHub's sanitizer). -->
<a href="https://academy.masterfabric.co">
  <img src="https://academy.masterfabric.co/academy-badge.png" alt="MasterFabric Academy" width="220">
</a>

Feed it a case. Get back a report scored criterion by criterion against a
published rubric, with the supporting quote under every rating — on your own
hardware.

**The model does not score.** It fills in a rubric: one rating and its evidence
per criterion. The weighted total is then computed deterministically in Go
([`internal/analysis/scoring.go`](./mf-backend/internal/analysis/scoring.go)).

That split is the whole point. *"Why should I trust an LLM's investment score?"*
— you don't. You trust the rubric, you audit the evidence the model collected,
and the arithmetic is open and ours. Which makes a rejection defensible: not
"the AI said no", but *"criterion 4 scored 2 of 5, because of this figure on
page 12 of the deck."*

```
case  →  rubric criteria retrieved            (RAG)
      →  model writes EVIDENCE + rating per criterion   (LoRA enforces the schema)
      →  schema validated
      →  weighted total computed DETERMINISTICALLY      (scoring.go)
      →  report with source quotes
```

One more rule that matters more than it looks: **absence of information is not a
low score.** When the text says nothing about a criterion, the model must write
`evidence_found: false` and leave the score `null`. A missing slide is a gap to
report, not a penalty to invent.

## The rubrics

Two ship seeded, in [`005_analysis.sql`](./mf-backend/migrations/005_analysis.sql).
Both are readable, weighted, and editable — a closed rubric would contradict the
claim.

**`startup-investability`** — traction 0.20 · market size 0.15 · solution
differentiation 0.15 · team 0.15 · business model 0.12 · problem clarity 0.10 ·
competition 0.05 · financials & ask 0.05 · risk awareness 0.03

**`digital-marketing`** — channel fit 0.25 · audience clarity 0.20 · budget
realism 0.15 · message differentiation 0.15 · measurement plan 0.15 ·
competitive context 0.10

Psychological assessment is deliberately out of scope: diagnosis is regulated,
and beyond what this hardware can honestly do.

## Consistency is measured, not asserted

`POST /analysis/trial` runs the same case repeatedly (5 by default, 10 max) and
reports the spread — including per-criterion standard deviation, so an unstable
rubric row can be found rather than guessed at. "Consistent" is a number here.

## Sovereignty

Nothing leaves your machine. Inference runs either in the visitor's browser via
WebGPU, or on a self-hosted GPU behind your own gateway. There is no third-party
model API in the path, which is what makes the engine usable on an applicant's
financials at all.

## Layout

| | |
| --- | --- |
| [`mf-backend/`](./mf-backend) | Go 1.26 + Postgres → **Render**. Rubric engine, scoring, auth, MCP server, wiki RAG |
| [`mf-frontend/`](./mf-frontend) | Next.js 16 SPA → **Vercel**. In-browser WebLLM, persona, metrics, admin |
| [`mf-inference/`](./mf-inference) | Self-hosted MLC + llama.cpp behind a Caddy gateway and a Cloudflare tunnel, plus the QLoRA training pipeline |
| [`mf-observability/`](./mf-observability) | Prometheus · Loki · Alloy · Grafana |
| **Live app** | https://mf-academy-raw-llm-monitoring-ten.vercel.app |
| **Live API** | https://mf-backend-zqsa.onrender.com |

The API is mounted as `/auth`, `/analysis`, `/decision`, `/llm`, `/wiki`,
`/admin`, `/mcp`, plus `/health` and `/metrics`.

## Use it from Claude or Cursor

The backend speaks MCP at `/mcp` and exposes six tools —
`list_analysis_domains`, `analyze_case`, `get_report`, `list_reports`,
`search_wiki`, `ask_wiki` — so the engine can be driven from any MCP client
without touching the UI.

## Quick start (local)

```bash
# 1. Backend (Postgres running; migrations apply on boot, rubrics seed themselves)
cd mf-backend && cp .env.example .env && go run ./cmd/server   # :8080

# 2. Frontend
cd ../mf-frontend
echo "NEXT_PUBLIC_API_URL=http://localhost:8080" > .env.local
npm install && npm run dev                                     # :3000
```

Open http://localhost:3000 in a **WebGPU browser** (Chrome/Edge 111+), register,
and run a case against a rubric.

Server-side inference is optional. With `LLM_BASE_URL` unset, `POST
/llm/generate` answers 503 and everything else — including the browser path —
keeps working, so the API survives the GPU machine being switched off.

## Deployment

- **Backend → Render:** [`render.yaml`](./render.yaml) provisions the web
  service (`rootDir: mf-backend`) and a managed Postgres, wiring `DATABASE_URL`
  and generating `JWT_SECRET`.
- **Frontend → Vercel:** set **Root Directory** to `mf-frontend` and
  `NEXT_PUBLIC_API_URL` to the Render URL.
- Then set the backend's `CORS_ORIGINS` to the Vercel origin.

The two build at different speeds, so a contract change leaves a window where
they disagree — decide which side lands first from the fields each reads on the
other's response.

## Documentation

- [docs/nasil-calisiyor.md](./docs/nasil-calisiyor.md) — architecture tour,
  question by question. Includes the measurements and, at the end, an honest
  list of what we did **not** build and why.
- [docs/urun-ve-pazarlama.md](./docs/urun-ve-pazarlama.md) — who this is for,
  positioning, pricing, and the plan's weak points.
- [docs/peft-nedir.md](./docs/peft-nedir.md) — fine-tuning, from scratch.
- [mf-backend/README.md](./mf-backend/README.md) ·
  [mf-frontend/README.md](./mf-frontend/README.md)

The docs are written to be read, not skimmed — including the parts where
something failed. `least_conn` behind Caddy's dynamic upstreams, two MLC
replicas exhausting a 6 GB card, a child context that could not extend its
parent's deadline: all of it is written down where it happened.

## Origin

Built as a MasterFabric Academy capstone (Go track, roadmap phases 36–60 and
96–100), then taken past it.
