#!/usr/bin/env python3
"""Build a supervised fine-tuning set for the investment PERSONA.

This is the sibling of build_dataset.py. That one teaches the rubric engine to
fill a JSON schema; this one teaches the conversational persona (internal/
decision) to do the two things a 2B base model does badly when asked to judge an
investment from evidence:

1. Ground every claim in the evidence it was given, and cite it as [n]. The base
   model, handed five numbered sources, writes a fluent verdict drawn largely
   from its own training and cites nothing — indistinguishable, to a reader,
   from a sourced one. Worse, it will invent a citation number the evidence does
   not contain.

2. Ask instead of guessing. When the decisive fact is missing — the stage, the
   revenue, the budget — the base model still commits to a verdict rather than
   asking the one question that would change it. A confident verdict on absent
   evidence is the failure mode the whole product exists to avoid.

Ground truth is constructed, not collected: we choose each dimension's quality
first, assemble evidence that says exactly that, and emit the verdict (or the
question) that must follow. The label is the input, so it cannot be wrong.

Format discipline
-----------------
The messages written here are byte-identical to what internal/decision sends the
model at inference: the system prompt and the turn instruction are FETCHED from
the running backend (GET /decision/prompt), and the evidence block is assembled
in the exact layout of decision.Agent.gather. If either drifts, the adapter is
tuned for a prompt nothing sends — so the fetch, and the mirrored layout below,
are load-bearing, not conveniences.
"""

from __future__ import annotations

import argparse
import json
import os
import random
import sys
import urllib.error
import urllib.request

# ---------------------------------------------------------------------------
# Dimension fragments.
#
# Five dimensions, matching the persona's system prompt. Each fragment is one
# dimension's worth of evidence at a known quality, plus the one-clause claim
# the verdict will make when it cites it. `kind` decides whether it renders as a
# web source (with a URL) or a DeepKwiki passage (without), so the training
# distribution contains both shapes the agent actually produces.
#
# score is 0-5 and is the label: the fragment text and its score must agree by
# construction, because the verdict is computed from the score, not read from
# the text.
# ---------------------------------------------------------------------------

Fragment = dict

DIMENSIONS: dict[str, dict] = {
    "pazar": {
        "weight": 0.25,
        "fragments": [
            {"score": 5, "kind": "web",
             "text": "Hedef segment 2025'te 4,2 milyar dolara ulaştı ve yıllık %28 büyüyor; "
                     "penetrasyon hâlâ %6 seviyesinde.",
             "claim": "pazar büyük ve hızlı büyüyor"},
            {"score": 2, "kind": "web",
             "text": "Pazarın toplam büyüklüğüne dair yayınlanmış bir rakam yok; kurucu "
                     "'çok büyük bir fırsat' diyor ama kaynak vermiyor.",
             "claim": "pazar büyüklüğü doğrulanamıyor"},
        ],
    },
    "rekabet": {
        "weight": 0.20,
        "fragments": [
            {"score": 5, "kind": "web",
             "text": "Segmentte üç yerleşik oyuncu var ama hiçbiri KOBİ tarafına inmiyor; "
                     "şirket bu boşlukta tek dikey çözüm.",
             "claim": "rekabette savunulabilir bir boşlukta konumlanmış"},
            {"score": 1, "kind": "web",
             "text": "Aynı işi yapan en az sekiz startup ve iki büyük SaaS sağlayıcısı mevcut; "
                     "farklılaşma net değil.",
             "claim": "rekabet yoğun ve farklılaşma zayıf"},
        ],
    },
    "moat": {
        "weight": 0.20,
        "fragments": [
            {"score": 5, "kind": "wiki",
             "text": "Kendi topladığı işlem verisiyle eğitilmiş bir tahmin modeli ve iki yıllık "
                     "veri birikimi giriş bariyeri oluşturuyor; ağ etkisi büyüdükçe artıyor.",
             "claim": "veriye dayalı bir moat var"},
            {"score": 2, "kind": "web",
             "text": "Ürün büyük ölçüde açık kaynak bileşenlerin üzerine kurulu; taklit edilmesini "
                     "zorlaştıran belirgin bir avantaj görünmüyor.",
             "claim": "belirgin bir savunulabilir avantaj yok"},
        ],
    },
    "ekip_traction": {
        "weight": 0.25,
        "fragments": [
            {"score": 5, "kind": "web",
             "text": "Kurucular sektörde 10+ yıl deneyimli; son 6 ayda aylık yinelenen gelir "
                     "%22 büyüyerek 180 bin dolara ulaştı, müşteri kaybı %2.",
             "claim": "kanıtlanmış ekip ve güçlü çekiş var"},
            {"score": 2, "kind": "web",
             "text": "Ekip teknik olarak güçlü ama henüz ödeyen müşteri yok; yalnızca pilot "
                     "görüşmeleri sürüyor.",
             "claim": "çekiş henüz gelire dönüşmemiş"},
        ],
    },
    "risk": {
        "weight": 0.10,
        "fragments": [
            {"score": 4, "kind": "wiki",
             "text": "Ana risk düzenleyici belirsizlik; şirketin bir hukuk danışmanı ve iki "
                     "pazarda alınmış ön onayı var.",
             "claim": "başlıca risk için azaltma planı mevcut"},
            {"score": 1, "kind": "web",
             "text": "İş modeli tek bir platformun API'sine bağımlı ve o platform benzer bir "
                     "ürünü kendi geliştiriyor.",
             "claim": "tek noktaya bağımlılık ciddi bir risk"},
        ],
    },
}

# The dimension whose absence should trigger a question rather than a guess.
# Stage/revenue is the decisive fact for an early-stage verdict, so a case that
# hides it must be answered with a question, not a score.
CLARIFY_DIMENSION = "ekip_traction"

CLARIFY_QUESTIONS = [
    "Kararı netleştirmek için: şirketin aşaması ve aylık yinelenen geliri (MRR) nedir?",
    "Değerlendirmeyi tamamlamak için çekiş verisine ihtiyacım var — kaç ödeyen müşteri ve ne kadar gelir var?",
    "Tek bir kritik bilgi eksik: şirket hangi aşamada ve şu anki gelir/kullanıcı sayısı ne?",
]

SECTORS = [
    "B2B SaaS", "fintech", "lojistik teknolojisi", "sağlık teknolojisi",
    "yapay zeka altyapısı", "e-ticaret altyapısı", "siber güvenlik", "iklim teknolojisi",
]
STAGES = ["pre-seed", "seed", "Seri A öncesi"]
COMPANY_NAMES = [
    "Nexora", "Volt AI", "Kargamo", "Finfleet", "Datça Labs", "Meridyen",
    "Sentio", "Akıntı", "Palet IQ", "Rota Analytics", "Kıvılcım", "Terra Grid",
]

# Fixed synthetic hostnames for web sources. Real enough to look like citations,
# obviously synthetic so no example points at a live page that could change.
WEB_HOSTS = ["techinsider.example", "marketwatch.example", "startupdaily.example",
             "vcnotes.example", "sectorreport.example"]


def fetch_persona_prompt(base_url: str, token: str) -> dict:
    """Read the exact system prompt, turn instruction and evidence header the
    backend sends. Duplicating them here would drift; see the module docstring."""
    req = urllib.request.Request(
        f"{base_url}/decision/prompt",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        sys.exit(f"could not fetch the persona prompt ({e.code}); is the backend "
                 f"running and the token valid?\n{e.read().decode(errors='replace')[:400]}")
    except urllib.error.URLError as e:
        sys.exit(f"could not reach {base_url}: {e.reason}")


def render_evidence(header: str, sources: list[dict]) -> str:
    """Mirror of decision.Agent.gather's layout.

    Kept deliberately mechanical so the duplication cannot rot: a web source is
    `[n] (web) title — url` then the snippet; a wiki source is `[n] (DeepKwiki)
    title` then the body. If gather grows a field, this stops matching and the
    adapter is trained on a shape inference never sends.
    """
    lines = [header]
    for s in sources:
        if s["kind"] == "web":
            lines.append(f"[{s['n']}] (web) {s['title']} — {s['url']}")
        else:
            lines.append(f"[{s['n']}] (DeepKwiki) {s['title']}")
        lines.append(s["text"])
    return "\n".join(lines)


def build_decide(rng: random.Random) -> tuple[list[dict], str, str, dict]:
    """A case with enough evidence to decide. Returns (sources, user_msg,
    target, meta)."""
    company = rng.choice(COMPANY_NAMES)
    sector = rng.choice(SECTORS)
    stage = rng.choice(STAGES)

    dims = list(DIMENSIONS.keys())
    # Most cases cover four or five dimensions; a covered one may still be weak.
    # A dimension left uncovered has no source and must NOT be cited — that is
    # the grounding the model has to learn.
    n_present = rng.randint(4, len(dims))
    present = rng.sample(dims, n_present)

    sources: list[dict] = []
    cited: list[dict] = []
    n = 0
    weight_sum = 0.0
    score_sum = 0.0
    for dim in dims:
        if dim not in present:
            continue
        frag = rng.choice(DIMENSIONS[dim]["fragments"])
        n += 1
        title = f"{company} — {dim} değerlendirmesi"
        src = {"n": n, "kind": frag["kind"], "title": title, "text": frag["text"]}
        if frag["kind"] == "web":
            src["url"] = f"https://{rng.choice(WEB_HOSTS)}/{company.lower().replace(' ', '-')}"
        sources.append(src)
        cited.append({"n": n, "dim": dim, "score": frag["score"], "claim": frag["claim"]})
        w = DIMENSIONS[dim]["weight"]
        weight_sum += w
        score_sum += w * frag["score"] / 5.0

    rng.shuffle(sources)
    # Renumber after the shuffle so [n] matches display order, and carry the new
    # numbers back onto the citations.
    remap = {}
    for i, s in enumerate(sources, start=1):
        remap[s["n"]] = i
        s["n"] = i
    for c in cited:
        c["n"] = remap[c["n"]]

    score100 = round(100 * score_sum / weight_sum) if weight_sum else 0
    if score100 >= 65:
        label = "Yatırılabilir"
    elif score100 >= 40:
        label = "Temkinli"
    else:
        label = "Yatırılamaz"

    missing = [d for d in dims if d not in present]
    target = render_verdict(rng, cited, label, score100, missing)
    user_msg = f"{company}, {stage} aşamasında bir {sector} girişimi. Yatırılabilir mi?"
    meta = {"mode": "decide", "label": label, "score": score100, "n_sources": len(sources)}
    return sources, user_msg, target, meta


def render_verdict(rng: random.Random, cited: list[dict], label: str,
                   score: int, missing: list[str]) -> str:
    """The assistant target for a decide case: a short synthesis with [n]
    citations, then the KARAR/SKOR/GEREKÇE block the UI parses."""
    strong = [c for c in cited if c["score"] >= 4]
    weak = [c for c in cited if c["score"] <= 2]

    synth_parts = []
    for c in strong[:2]:
        synth_parts.append(f"{c['claim']} [{c['n']}]")
    for c in weak[:2]:
        synth_parts.append(f"ancak {c['claim']} [{c['n']}]")
    if not synth_parts:
        synth_parts.append(f"kanıtlar karışık [{cited[0]['n']}]")
    synthesis = "Kanıtlara göre " + "; ".join(synth_parts) + "."

    gaps = ""
    if missing:
        human = {"pazar": "pazar büyüklüğü", "rekabet": "rekabet", "moat": "moat",
                 "ekip_traction": "çekiş", "risk": "risk"}
        names = ", ".join(human.get(m, m) for m in missing)
        gaps = f" {names} konusunda elde kanıt yok; bu boyut(lar) düşük güvenle bırakıldı."

    reason = synthesis + gaps
    return (f"{synthesis}\n\n"
            f"KARAR: {label}\n"
            f"SKOR: {score}\n"
            f"GEREKÇE: {reason}")


def build_clarify(rng: random.Random) -> tuple[list[dict], str, str, dict]:
    """A case missing the decisive fact. The target is one question, no verdict."""
    company = rng.choice(COMPANY_NAMES)
    sector = rng.choice(SECTORS)

    # Cover some dimensions but never the decisive one, and never state the
    # stage in the user message — so the only correct move is to ask.
    dims = [d for d in DIMENSIONS if d != CLARIFY_DIMENSION]
    present = rng.sample(dims, rng.randint(2, len(dims)))

    sources: list[dict] = []
    n = 0
    for dim in dims:
        if dim not in present:
            continue
        frag = rng.choice(DIMENSIONS[dim]["fragments"])
        n += 1
        src = {"n": n, "kind": frag["kind"],
               "title": f"{company} — {dim} değerlendirmesi", "text": frag["text"]}
        if frag["kind"] == "web":
            src["url"] = f"https://{rng.choice(WEB_HOSTS)}/{company.lower().replace(' ', '-')}"
        sources.append(src)

    rng.shuffle(sources)
    for i, s in enumerate(sources, start=1):
        s["n"] = i

    target = rng.choice(CLARIFY_QUESTIONS)
    user_msg = f"{company} adlı bir {sector} girişimine yatırım yapmalı mıyım?"
    meta = {"mode": "clarify", "label": None, "score": None, "n_sources": len(sources)}
    return sources, user_msg, target, meta


def build_example(rng: random.Random, header: str, turn_instruction: str,
                  clarify: bool) -> tuple[dict, dict]:
    if clarify:
        sources, user_msg, target, meta = build_clarify(rng)
    else:
        sources, user_msg, target, meta = build_decide(rng)

    evidence = render_evidence(header, sources)
    final_user = f"{evidence}\n\nKULLANICI: {user_msg}\n\n{turn_instruction}"
    example = {"messages": [
        {"role": "user", "content": final_user},
        {"role": "assistant", "content": target},
    ]}
    return example, meta


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8090"))
    ap.add_argument("--token", default=os.environ.get("TOKEN", ""),
                    help="access token; or set TOKEN")
    ap.add_argument("--n", type=int, default=800, help="training examples")
    ap.add_argument("--n-eval", type=int, default=100, help="held-out examples")
    ap.add_argument("--clarify-share", type=float, default=0.3,
                    help="fraction of cases whose correct answer is a question")
    ap.add_argument("--seed", type=int, default=20260724)
    ap.add_argument("--out-dir", default="data")
    args = ap.parse_args()

    if not args.token:
        sys.exit("a token is required: --token or TOKEN=... (any account works)")

    spec = fetch_persona_prompt(args.base_url, args.token)
    system_prompt = spec["system_prompt"]
    turn_instruction = spec["turn_instruction"]
    header = spec["evidence_header"]

    rng = random.Random(args.seed)
    os.makedirs(args.out_dir, exist_ok=True)

    counts = {"persona_train": args.n, "persona_eval": args.n_eval}
    stats = {"decide": 0, "clarify": 0}

    for split, n in counts.items():
        path = os.path.join(args.out_dir, f"{split}.jsonl")
        meta_path = os.path.join(args.out_dir, f"{split}_meta.jsonl")
        with open(path, "w", encoding="utf-8") as fh, \
                open(meta_path, "w", encoding="utf-8") as mfh:
            for _ in range(n):
                clarify = rng.random() < args.clarify_share
                example, meta = build_example(rng, header, turn_instruction, clarify)
                # The system prompt is prepended here rather than inside
                # build_example so the fetched string is the single source.
                example["messages"].insert(0, {"role": "system", "content": system_prompt})
                stats[meta["mode"]] += 1
                fh.write(json.dumps(example, ensure_ascii=False) + "\n")
                mfh.write(json.dumps(meta, ensure_ascii=False) + "\n")
        print(f"  {path}: {n} examples (+ {meta_path})")

    total = stats["decide"] + stats["clarify"]
    share = stats["clarify"] / total if total else 0
    print(f"\npersona dataset: {total} examples, {stats['clarify']} clarify ({share:.0%})")
    if share < 0.15:
        print("WARNING: too few clarify cases to teach asking over guessing; "
              "raise --clarify-share")


if __name__ == "__main__":
    main()
