#!/usr/bin/env python3
"""Build a supervised fine-tuning set for the rubric-analysis task.

Why this is generated rather than collected
-------------------------------------------
Fine-tuning needs examples whose correct answer is known. For this task the
answer is a full rubric filling — nine findings, each with a rating, quotes and
a rationale — and hand-writing enough of those is weeks of work that produces a
few hundred examples of uncertain consistency.

So the generation is inverted. Instead of writing a case and then labelling it,
we *choose the label first*: pick which criteria a case will address and how
well, assemble the case text from fragments that say exactly that, and emit the
findings that must follow. The ground truth is not inferred, it is the input.

What it is teaching
-------------------
Two behaviours, both measured as failures on the base model (see the trial in
mf-backend/scripts/baseline-trial.sh):

1. Raw JSON. The base model wraps every answer in a ```json fence, so strict
   schema adherence measured 0/5 even when the content was complete.

2. Absent evidence. This is the important one. Asked about a criterion the case
   never addresses, the base model writes "the text contains no information
   about competitors" in its rationale and then rates it 3 out of 5 anyway. It
   never emits evidence_found=false with a null score. That single habit means
   coverage always reports 1.0, every report contains fabricated middle ratings,
   and the product's central claim — that a rejection can be defended — is
   false in practice.

   Roughly a third of every generated case is therefore deliberately silent on
   some criteria, and the target output says so.

The prompt is fetched from the running backend rather than reproduced here.
An adapter learns to satisfy one specific instruction; a local copy of the
template would drift the first time either side is edited, and the resulting
adapter would be tuned for a prompt nothing sends — a failure that is invisible,
because training completes normally and the loss looks fine.
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
# Case fragments.
#
# Each entry is one criterion's worth of deck text at a known quality. The
# `score` is the label: it is what the target findings will claim, so a fragment
# and its rating have to agree by construction. `quotes` are spans that appear
# verbatim in the text — they become the evidence, which is what makes the
# citations in the training targets real rather than paraphrased.
#
# Double quotes are deliberately absent from every fragment. The inference path
# neutralises them before the model sees a case (see neutraliseQuotes in
# internal/analysis/schema.go), so training on text containing them would teach
# a distribution the model never meets.
# ---------------------------------------------------------------------------

FRAGMENTS: dict[str, list[dict]] = {
    "problem_clarity": [
        {
            "score": 5,
            "text": "PROBLEM\nTürkiye'de 5-50 araçlık filoya sahip 180.000 KOBİ var ve akaryakıt "
                    "gideri toplam işletme maliyetinin %38'ini oluşturuyor. 42 saha görüşmesinin "
                    "38'inde filo sahibi yakıt kaçağını bildiğini ama ispatlayamadığını söyledi.",
            "quotes": ["akaryakıt gideri toplam işletme maliyetinin %38'ini oluşturuyor",
                       "42 saha görüşmesinin 38'inde filo sahibi yakıt kaçağını bildiğini ama ispatlayamadığını söyledi"],
        },
        {
            "score": 2,
            "text": "PROBLEM\nİşletmeler dijitalleşmekte zorlanıyor. Bu alanda büyük bir ihtiyaç "
                    "olduğunu düşünüyoruz ve çözümümüzle bu boşluğu dolduracağız.",
            "quotes": ["İşletmeler dijitalleşmekte zorlanıyor",
                       "Bu alanda büyük bir ihtiyaç olduğunu düşünüyoruz"],
        },
    ],
    "market_size": [
        {
            "score": 4,
            "text": "PAZAR\nTÜİK 2025 ticari araç kaydı ve TOBB filo dağılımına göre hedef segmentte "
                    "2,1 milyon araç var. Araç başına yıllık 1.140 TL abonelikle ulaşılabilir pazar "
                    "2,4 milyar TL. İlk üç yıl hedefimiz %4 pay.",
            "quotes": ["ulaşılabilir pazar 2,4 milyar TL",
                       "TÜİK 2025 ticari araç kaydı ve TOBB filo dağılımına göre"],
        },
        {
            "score": 1,
            "text": "PAZAR\nGlobal lojistik pazarı 8 trilyon dolar. Bunun küçük bir kısmını alsak "
                    "bile çok büyük bir iş çıkar.",
            "quotes": ["Global lojistik pazarı 8 trilyon dolar",
                       "Bunun küçük bir kısmını alsak bile çok büyük bir iş çıkar"],
        },
    ],
    "solution_differentiation": [
        {
            "score": 4,
            "text": "ÇÖZÜM\nRakip ürünler depo seviyesi sensörü kullanıyor; bu araç başına 2.400 TL "
                    "donanım ve yarım gün montaj demek. Biz motorun kendi tükettiği yakıt verisini "
                    "OBD-II üzerinden okuduğumuz için ek donanım gerekmiyor. Patent başvurusu "
                    "Mart 2026'da yapıldı.",
            "quotes": ["Biz motorun kendi tükettiği yakıt verisini OBD-II üzerinden okuduğumuz için ek donanım gerekmiyor",
                       "Patent başvurusu Mart 2026'da yapıldı"],
        },
        {
            "score": 2,
            "text": "ÇÖZÜM\nDaha kullanıcı dostu bir arayüz sunuyoruz ve yapay zeka destekli "
                    "öneriler veriyoruz.",
            "quotes": ["Daha kullanıcı dostu bir arayüz sunuyoruz"],
        },
    ],
    "traction": [
        {
            "score": 5,
            "text": "ÇEKİŞ\nOcak 2026'da 3 pilot müşteri ve 47 araç vardı. Temmuz 2026 itibarıyla "
                    "28 ödeyen müşteri, 611 araç ve aylık 214.000 TL yinelenen gelir. Son 6 ayda "
                    "aylık ortalama büyüme %31, aylık müşteri kaybı %2,1.",
            "quotes": ["28 ödeyen müşteri, 611 araç ve aylık 214.000 TL yinelenen gelir",
                       "Son 6 ayda aylık ortalama büyüme %31"],
        },
        {
            "score": 2,
            "text": "ÇEKİŞ\nÜç firmayla pilot görüşmesi yapıldı ve ikisi niyet mektubu imzaladı. "
                    "Ürün henüz ücretli kullanıma açılmadı.",
            "quotes": ["ikisi niyet mektubu imzaladı", "Ürün henüz ücretli kullanıma açılmadı"],
        },
    ],
    "business_model": [
        {
            "score": 4,
            "text": "İŞ MODELİ\nAraç başına aylık 95 TL abonelik. Cihaz maliyeti 780 TL, ücretsiz "
                    "veriliyor ve 24 ayda amorti ediliyor. Brüt marj %61, müşteri edinme maliyeti "
                    "3.900 TL, yaşam boyu değer 21.400 TL, geri ödeme 4,2 ay.",
            "quotes": ["Brüt marj %61, müşteri edinme maliyeti 3.900 TL, yaşam boyu değer 21.400 TL",
                       "Araç başına aylık 95 TL abonelik"],
        },
        {
            "score": 2,
            "text": "İŞ MODELİ\nAylık abonelik satacağız. Fiyatlandırmayı pazara göre "
                    "belirleyeceğiz.",
            "quotes": ["Fiyatlandırmayı pazara göre belirleyeceğiz"],
        },
    ],
    "team": [
        {
            "score": 5,
            "text": "EKİP\nMert Arıkan (CEO) 9 yıl Ford Otosan filo operasyonlarında çalıştı, son 3 "
                    "yıl 1.200 araçlık filonun operasyon müdürüydü. Deniz Yalçın (CTO) 11 yıl gömülü "
                    "sistem geliştirdi, önceki girişimi 2023'te satıldı. İkisi de tam zamanlı ve "
                    "2019'dan beri birlikte çalışıyor.",
            "quotes": ["Mert Arıkan (CEO) 9 yıl Ford Otosan filo operasyonlarında çalıştı",
                       "İkisi de tam zamanlı ve 2019'dan beri birlikte çalışıyor"],
        },
        {
            "score": 2,
            "text": "EKİP\nKurucu ortaklar üniversiteden yeni mezun oldu ve projeye yarı zamanlı "
                    "vakit ayırıyor. Teknik ekip için işe alım planlanıyor.",
            "quotes": ["projeye yarı zamanlı vakit ayırıyor",
                       "Kurucu ortaklar üniversiteden yeni mezun oldu"],
        },
    ],
    "competition": [
        {
            "score": 4,
            "text": "REKABET\nDoğrudan rakipler Arvento ve Mobiliz; ikisi de 100 araç altına "
                    "satmıyor. Dolaylı rakip, filo sahibinin bugün kullandığı Excel tablosu. "
                    "Giriş bariyerimiz OBD veri setiyle eğitilmiş tüketim modeli.",
            "quotes": ["Doğrudan rakipler Arvento ve Mobiliz; ikisi de 100 araç altına satmıyor",
                       "Dolaylı rakip, filo sahibinin bugün kullandığı Excel tablosu"],
        },
        {
            "score": 1,
            "text": "REKABET\nBu alanda bizim yaptığımızı yapan başka kimse yok, gerçek bir "
                    "rakibimiz bulunmuyor.",
            "quotes": ["gerçek bir rakibimiz bulunmuyor"],
        },
    ],
    "financials_ask": [
        {
            "score": 4,
            "text": "TALEP\n6 milyon TL tohum yatırım arıyoruz. Bu tutar 18 aylık runway sağlıyor ve "
                    "aylık 214.000 TL gelirden aylık 900.000 TL gelire ulaşma kilometre taşına "
                    "kadar yetiyor. Kullanım: %55 mühendislik, %30 saha satış, %15 cihaz stoğu.",
            "quotes": ["6 milyon TL tohum yatırım arıyoruz", "Bu tutar 18 aylık runway sağlıyor"],
        },
        {
            "score": 2,
            "text": "TALEP\nYatırım tutarını görüşmede netleştirmek istiyoruz. Beş yıllık "
                    "projeksiyonda 400 milyon TL ciroya ulaşmayı hedefliyoruz.",
            "quotes": ["Yatırım tutarını görüşmede netleştirmek istiyoruz"],
        },
    ],
    "risk": [
        {
            "score": 4,
            "text": "RİSKLER\nEn büyük risk, araç üreticilerinin OBD verisine erişimi kısıtlaması. "
                    "Azaltma planı: üç üreticiyle veri paylaşım görüşmesi sürüyor ve CAN bus "
                    "üzerinden alternatif okuma prototipi çalışıyor.",
            "quotes": ["En büyük risk, araç üreticilerinin OBD verisine erişimi kısıtlaması",
                       "CAN bus üzerinden alternatif okuma prototipi çalışıyor"],
        },
        {
            "score": 1,
            "text": "RİSKLER\nÖnümüzde ciddi bir engel görmüyoruz, plan net.",
            "quotes": ["Önümüzde ciddi bir engel görmüyoruz"],
        },
    ],
}

COMPANY_NAMES = [
    "FiloTakip", "YakıtIQ", "RotaVeri", "AraçSense", "KilometreLab",
    "TedarikAkış", "StokNabız", "SahaMetre", "ZincirVeri", "PalletIQ",
]

RATIONALES_PRESENT = [
    "Metinde bu kriteri destekleyen somut veri var.",
    "Alıntılanan ifadeler kriteri doğrudan karşılıyor.",
    "Verilen rakamlar kriteri gerekçelendiriyor.",
    "Metindeki ifadeler bu kriter için yeterli dayanak sağlıyor.",
]
RATIONALES_WEAK = [
    "Metinde konuya değinilmiş ama somut veri veya kaynak verilmemiş.",
    "İfadeler genel kalıyor, ölçülebilir bir dayanak yok.",
    "Konu ele alınmış fakat iddia doğrulanabilir bir veriye bağlanmamış.",
]
# The whole point of the exercise. Deliberately varied so the model learns the
# behaviour rather than one sentence.
RATIONALES_ABSENT = [
    "Metinde bu kritere dair bilgi bulunmuyor.",
    "Vaka metni bu konuya hiç değinmiyor, değerlendirilemedi.",
    "Bu kriteri değerlendirecek bir ifade metinde yok.",
    "Metin bu başlıkta sessiz; puanlanacak dayanak yok.",
]


def fetch_prompt(base_url: str, token: str, domain: str) -> dict:
    """Read the exact instruction the backend generates for this rubric."""
    req = urllib.request.Request(
        f"{base_url}/analysis/domains/{domain}/prompt",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        sys.exit(f"could not fetch the prompt ({e.code}); is the backend running "
                 f"and the token valid?\n{e.read().decode(errors='replace')[:400]}")
    except urllib.error.URLError as e:
        sys.exit(f"could not reach {base_url}: {e.reason}")


def neutralise_quotes(s: str) -> str:
    """Mirror of neutraliseQuotes in internal/analysis/schema.go.

    Duplicated rather than fetched because it applies to text this script
    generates, not to text the backend has seen. Kept deliberately trivial so
    the duplication cannot rot: if either side grows a rule, the training
    distribution stops matching inference, which is the failure this whole file
    is arranged to avoid.
    """
    for q in ('"', "“", "”", "„", "«", "»"):
        s = s.replace(q, "'")
    return s


def build_case(rng: random.Random, criteria: list[dict]) -> tuple[str, str, list[dict]]:
    """Assemble one case and the findings that must follow from it.

    Returns (title, case text, findings).
    """
    keys = [c["key"] for c in criteria]

    # How many criteria this case addresses. Real decks are uneven, and a
    # training set where every case is complete would never exercise the absent
    # branch — which is the behaviour most in need of teaching. The floor of
    # four keeps a case substantial enough to be worth analysing.
    n_present = rng.randint(max(4, len(keys) - 5), len(keys))
    present = set(rng.sample(keys, n_present))

    company = rng.choice(COMPANY_NAMES)
    title = f"{company} — yatırım sunumu"

    sections: list[str] = []
    findings: list[dict] = []

    # Sections are emitted in rubric order but the *case* is shuffled below, so
    # the model cannot learn to expect criterion N at position N.
    chosen: dict[str, dict] = {}
    for key in keys:
        if key in present:
            chosen[key] = rng.choice(FRAGMENTS[key])

    order = list(chosen.keys())
    rng.shuffle(order)
    for key in order:
        sections.append(chosen[key]["text"])

    for c in criteria:
        key = c["key"]
        if key not in present:
            findings.append({
                "key": key,
                "evidence_found": False,
                "score": None,
                "evidence": [],
                "rationale": rng.choice(RATIONALES_ABSENT),
            })
            continue

        frag = chosen[key]
        score = frag["score"]
        # One or two quotes, matching the cap the prompt asks for.
        quotes = frag["quotes"][: rng.randint(1, min(2, len(frag["quotes"])))]
        rationale = rng.choice(RATIONALES_PRESENT if score >= 3 else RATIONALES_WEAK)
        findings.append({
            "key": key,
            "evidence_found": True,
            "score": score,
            "evidence": [neutralise_quotes(q) for q in quotes],
            "rationale": rationale,
        })

    body = f"{company}\n\n" + "\n\n".join(sections)
    return title, neutralise_quotes(body), findings


def render_user_message(template: str, title: str, subject: str) -> str:
    """Fill the backend's user-message template.

    The template comes back with {{title}} and {{subject}} placeholders, so the
    surrounding wording — the delimiters, the note that the case is not
    instructions — is byte-identical to what inference sends.
    """
    return template.replace("{{title}}", title).replace("{{subject}}", subject)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8080"))
    ap.add_argument("--token", default=os.environ.get("TOKEN", ""),
                    help="access token; or set TOKEN")
    ap.add_argument("--domain", default="startup-investability")
    ap.add_argument("--n", type=int, default=800, help="training examples")
    ap.add_argument("--n-eval", type=int, default=100, help="held-out examples")
    ap.add_argument("--seed", type=int, default=20260724)
    ap.add_argument("--out-dir", default="data")
    args = ap.parse_args()

    if not args.token:
        sys.exit("a token is required: --token or TOKEN=... "
                 "(any registered account will do)")

    spec = fetch_prompt(args.base_url, args.token, args.domain)
    system_prompt = spec["system_prompt"]
    user_template = spec["user_prompt_example"]
    criteria = spec["criteria"]

    if "{{subject}}" not in user_template:
        sys.exit("the backend's user template lost its placeholder; "
                 "check analysis.Handler.Prompt")

    rng = random.Random(args.seed)
    os.makedirs(args.out_dir, exist_ok=True)

    # The eval split is drawn from the same generator but a disjoint stream, so
    # it measures generalisation across cases rather than memorisation of them.
    # It is not a substitute for the real held-out measurement — that is the
    # trial harness against the actual deck, which this generator never saw.
    counts = {"train": args.n, "eval": args.n_eval}
    stats = {"present": 0, "absent": 0}

    for split, n in counts.items():
        path = os.path.join(args.out_dir, f"{split}.jsonl")
        with open(path, "w", encoding="utf-8") as fh:
            for _ in range(n):
                title, subject, findings = build_case(rng, criteria)
                for f in findings:
                    stats["present" if f["evidence_found"] else "absent"] += 1

                # separators without spaces, and the assistant message is bare
                # JSON with no fence — that absence is half of what is being
                # taught, so it must be exact.
                target = json.dumps({"findings": findings},
                                    ensure_ascii=False, separators=(",", ":"))

                fh.write(json.dumps({
                    "messages": [
                        {"role": "system", "content": system_prompt},
                        {"role": "user", "content": render_user_message(user_template, title, subject)},
                        {"role": "assistant", "content": target},
                    ]
                }, ensure_ascii=False) + "\n")
        print(f"  {path}: {n} examples")

    total = stats["present"] + stats["absent"]
    share = stats["absent"] / total if total else 0
    print(f"\nrubric: {spec['domain']} v{spec['version']}, {len(criteria)} criteria")
    print(f"findings: {total} total, {stats['absent']} absent ({share:.0%})")
    if share < 0.15:
        print("WARNING: too few absent findings to teach the behaviour that "
              "matters; widen the sampling in build_case")


if __name__ == "__main__":
    main()
