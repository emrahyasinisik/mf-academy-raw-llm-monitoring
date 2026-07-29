#!/usr/bin/env python3
"""Dump the fragment banks as markdown, for review by whoever owns the domain.

The banks encode a judgement — what makes a claim worth 4 rather than 2 — and
that judgement is a business decision, not a technical one. It is also nearly
invisible where it lives: fifty-one fragments inside a 700-line generator, each
a Python dict with escaped newlines. This renders them as prose so the call can
actually be checked, criterion by criterion, by someone who is not reading
Python.

Regenerated rather than committed as a document, so it can never drift from the
bank it describes.

Usage:
    python3 review_fragments.py                    # to stdout
    python3 review_fragments.py --out review.md
"""

from __future__ import annotations

import argparse
import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load_banks():
    """Import build_dataset without running it."""
    spec = importlib.util.spec_from_file_location("build_dataset", HERE / "build_dataset.py")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod.DOMAIN_BANKS


def render(banks) -> str:
    out: list[str] = [
        "# Fragment bankaları — gözden geçirme",
        "",
        "Her fragment bir vaka bölümü ve ona **etiket** olarak verilen puan. "
        "Metin puanı hak etmek zorunda: bir fragmentin puanı yanlışsa, o puanı "
        "veren her eğitim satırı yanlış olur.",
        "",
        "Bakılacak üç şey:",
        "",
        "1. **Puan doğru mu** — bu metin gerçekten o puanı mı alır?",
        "2. **Gerekçe metni gösteriyor mu** — puanı, metindeki hangi şeyin "
        "kazandırdığını söylüyor mu?",
        "3. **Sektör sızıyor mu** — metin belirli bir sektöre demir atıyorsa "
        "model kanıt kalitesi yerine o sektörün kelimelerini öğrenir.",
        "",
        "Düzeltmek için `build_dataset.py` içindeki `DOMAIN_BANKS`, sonra veriyi "
        "yeniden üret.",
        "",
    ]

    for domain, bank in banks.items():
        frags = bank["fragments"]
        total = sum(len(v) for v in frags.values())
        out += [
            "---",
            "",
            f"## `{domain}`",
            "",
            f"{len(frags)} kriter, {total} fragment. Vaka başlığı: "
            f"*… — {bank['title_suffix']}*.",
            "",
        ]
        for key, variants in frags.items():
            out += [f"### `{key}`", ""]
            for f in sorted(variants, key=lambda x: -x["score"]):
                body = f["text"].split("\n", 1)
                header = body[0]
                text = body[1] if len(body) > 1 else ""
                out += [
                    f"**{f['score']}/5** · bölüm `{header}`",
                    "",
                    f"> {text}",
                    "",
                    f"*Gerekçe:* {f['rationale']}",
                    "",
                    "*Alıntılar:*",
                ]
                out += [f"- {q}" for q in f["quotes"]]
                out.append("")
    return "\n".join(out)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--out", default="", help="write here instead of stdout")
    args = ap.parse_args()

    text = render(load_banks())
    if args.out:
        Path(args.out).write_text(text, encoding="utf-8")
        print(f"wrote {args.out}")
    else:
        sys.stdout.write(text)


if __name__ == "__main__":
    main()
