# mf-frontend — Raw LLM Monitoring & Decision Scoring (SPA)

Next.js **single-page app** for the MasterFabric capstone. Runs **Gemma** in the
browser via **WebLLM / MLC-LLM** (WebGPU), records each run to the Go backend,
and shows its **decision score**.

- **Live app:** `https://mf-frontend.vercel.app` _(update after deploy)_
- **API:** [`mf-backend`](../mf-backend) → Render
- **Stack:** Next.js 16 · React 19 · Tailwind v4 · `@mlc-ai/web-llm`

> ⚠️ Requires a **WebGPU** browser (Chrome / Edge 111+). The model (~1.4 GB for
> Gemma 2 2B) downloads once into the browser cache on first load.

---

## Architecture (SPA)

One page (`/`) renders `AppShell`, a client-side router that swaps **master
views** in place with no full-page reload:

| Master view | Subviews / content |
| --- | --- |
| **Auth** | Login · Register subviews |
| **LLM Playground** | Load Gemma → prompt → run → auto-score |
| **Monitoring Dashboard** | Aggregate metrics · run history · score breakdowns |

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

## Deploy (Vercel)

Vercel auto-detects Next.js. **Set one environment variable** in the project
settings before deploying:

```
NEXT_PUBLIC_API_URL = https://mf-backend.onrender.com
```

Then deploy via the Vercel MCP or `vercel --prod`. After the frontend URL is
known, update the backend's `CORS_ORIGINS` on Render to match.
