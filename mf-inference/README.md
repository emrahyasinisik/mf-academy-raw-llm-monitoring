# mf-inference — self-hosted MLC-LLM

The server-side half of the LLM. The browser path (WebLLM/WebGPU) stays exactly
as it is; this adds a second target so the same model can be run — and measured —
in two places.

```
Vercel (frontend) ──> Render (Go backend) ──> Cloudflare Tunnel ──> this box
                                                                     │
                                                        cloudflared ─┤
                                                            gateway ─┤  (Caddy, X-API-Key)
                                                                mlc ─┘  (mlc_llm serve, CUDA)
```

Nothing on Render moves. The backend gains one env var pointing at the tunnel
hostname.

## Why it is not on Render

Render's free instance is 512 MB of RAM with no GPU; the Gemma 2 2B weights
alone are ~1.4 GB. Render offers no GPU on any plan, a second container would
need a paid private service, and a free service sleeps after 15 minutes —
waking it would reload the weights from scratch.

## Target hardware

Verified against: GTX 1660 Ti (6 GB), 16 GB RAM, Windows 11 Pro, driver 610.62.

The card is Turing (compute 7.5), which is the *minimum* architecture CUDA 13
still supports — Maxwell, Pascal and Volta were dropped in 13.0. One generation
older and this would not work at all.

6 GB is the real constraint. Gemma 2 2B q4f16_1 needs ~1.5 GB and leaves plenty
of headroom, but Windows is drawing the desktop on the same card, so do not
reach for 7-8B models here.

## Setup (once)

1. **WSL2 + Docker Desktop.** In Docker Desktop: Settings → General → *Use the
   WSL 2 based engine*. GPU passthrough does not work with the Hyper-V backend.

2. **Verify the GPU is visible to containers.** This is the step that catches
   a broken NVIDIA Container Toolkit install, and it is much easier to debug
   here than inside the MLC build:

   ```powershell
   docker run --rm --gpus all nvidia/cuda:12.2.2-base-ubuntu22.04 nvidia-smi
   ```

   You should see the 1660 Ti listed. If not, stop and fix this first.

3. **Create the Cloudflare tunnel.** Zero Trust dashboard → Networks → Tunnels →
   Create a tunnel → Docker. Copy the token. Point the tunnel's public hostname
   at `http://gateway:8080`.

4. **Configure.**

   ```powershell
   copy .env.example .env
   # fill in LLM_API_KEY and TUNNEL_TOKEN
   ```

5. **Start.**

   ```powershell
   docker compose up -d --build
   docker compose logs -f mlc
   ```

   The first start downloads ~1.5 GB of weights into the `model-cache` volume.
   Wait for the log line saying the server is listening before testing.

## Verify

Locally on the Windows box:

```powershell
curl.exe http://127.0.0.1:8080/health

curl.exe -X POST http://127.0.0.1:8080/v1/chat/completions `
  -H "Content-Type: application/json" `
  -H "X-API-Key: <your LLM_API_KEY>" `
  -d '{\"model\":\"gemma-2-2b-it-q4f16_1-MLC\",\"messages\":[{\"role\":\"user\",\"content\":\"Explain what a goroutine is in one paragraph.\"}]}'
```

Then the same request against the tunnel hostname from anywhere. Check that
**omitting** `X-API-Key` returns 401 — if it returns a completion, the GPU is
open to the internet.

Record how long the request takes. That number decides the backend's
`LLM_TIMEOUT`, and whether the synchronous request/response design holds at all
or the endpoint has to stream.

## Known adjustment points

- **Wheel tag.** MLC publishes one wheel per CUDA minor version and the set
  changes over time. If the image build fails on the `pip install`, check
  <https://mlc.ai/wheels> for the current tags and set `CUDA_TAG` in `.env` plus
  the matching `nvidia/cuda:<version>-runtime-ubuntu22.04` base in
  `mlc/Dockerfile`. The host driver does not need to match — it is newer than
  any of these and backward compatible.
- **Out of memory.** Drop to `Llama-3.2-1B-Instruct-q4f16_1-MLC`, or close
  whatever else is using the card.
- **The box must stay awake.** Sleep drops the tunnel and the backend starts
  returning 502. Disable sleep in Windows power settings.

## Security notes

- `mlc` publishes no host port; the only route in is through the gateway.
- The gateway rejects any request without `X-API-Key`. Caddy has no built-in
  rate limiting, so a leaked key means unmetered use of the card — rotate it by
  changing `.env` on this side and `LLM_API_KEY` on the backend.
- `.env` holds both the tunnel token and the shared secret. It is gitignored;
  keep it that way.
