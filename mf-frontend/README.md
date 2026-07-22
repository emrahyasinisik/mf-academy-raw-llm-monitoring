# mf-frontend — Raw LLM Monitoring & Decision Scoring (SPA)

Next.js **single-page app** for the MasterFabric capstone. Runs **Gemma** in the
browser via **WebLLM / MLC-LLM** (WebGPU), records each run to the Go backend,
and shows its **decision score**.

- **Live app:** **https://mf-academy-raw-llm-monitoring-ten.vercel.app** ✅ live
- **Live API:** **https://mf-backend-zqsa.onrender.com** ✅ live (Render)
- **Stack:** Next.js 16 · React 19 · Tailwind v4 · `@mlc-ai/web-llm`

> ⚠️ Requires a **WebGPU** browser (Chrome / Edge 111+). The model (~1.4 GB for
> Gemma 2 2B) downloads once into the browser cache on first load.

---

## Architecture (SPA)

One page (`/`) renders `AppShell`, a client-side router that swaps **master
views** in place with no full-page reload. Both navigation levels are mirrored
into the URL hash as `#master/subview`, so every subview is deep-linkable and
the browser's back button works:

| Master view | Subviews |
| --- | --- |
| **Auth** (logged out) | Login · Register |
| **LLM Playground** | Run & score · Model runtime |
| **Monitoring** | Overview · Run history |
| **Decision Scoring** | Scoring queue · Rubric analysis · Compare runs |

Monitoring answers *what did the model do*; Decision Scoring answers *how good
was it* — it owns the unscored backlog (drained through a bounded worker pool),
the per-dimension rubric across the whole cohort, and side-by-side run
comparison.

```
src/
  app/            layout, providers, single page → AppShell
  components/     AppShell (router) + views/ + ui/
  lib/            api.ts (typed client), webllm.ts (browser LLM), types.ts
  store/          auth.tsx (React context)
```

## Local development

```bash
npm install
echo "NEXT_PUBLIC_API_URL=http://localhost:8080" > .env.local
npm run dev          # → http://localhost:3000
```

The backend ([`mf-backend`](../mf-backend)) must be running and its
`CORS_ORIGINS` must include `http://localhost:3000`.

## Token storage (a deliberate tradeoff)

The typed API client ([`src/lib/api.ts`](src/lib/api.ts)) stores the JWT access
and refresh tokens in **`localStorage`**. This is a conscious tradeoff, not an
oversight:

- **Why localStorage:** the frontend deploys to Vercel as a static SPA on a
  *different origin* from the Render API. `httpOnly` cookies would need
  `SameSite=None; Secure` plus CORS credentials and CSRF protection — more
  moving parts than this capstone warrants, and cross-site cookies are
  increasingly blocked by browsers.
- **The cost:** tokens in `localStorage` are readable by any JavaScript running
  on the page, so a successful **XSS** would expose them. We mitigate by keeping
  the access token short-lived (15 min) with **rotating** refresh tokens, so a
  leaked token has a small blast radius and stolen refresh tokens are
  single-use.
- **Hardening path:** for a production system, move the refresh token to an
  `httpOnly` cookie (same-site if the API and app share a domain) and keep only
  the access token in memory.

## Deploy (Vercel)

Vercel auto-detects Next.js. **Set one environment variable** in the project
settings before deploying:

```
NEXT_PUBLIC_API_URL = https://mf-backend.onrender.com
```

Then deploy via the Vercel MCP or `vercel --prod`. After the frontend URL is
known, update the backend's `CORS_ORIGINS` on Render to match.
