# Colab pilot adapter'ını GPU kutusunda ölçmek

Colab pilotu 31 Tem 2026'da bir adapter üretti (`out/colab-pilot`, 15 adım,
60 satır geçişi). Eğitim ölçüldü, **rubrik metrikleri ölçülmedi**: Colab o gün
dördüncü oturumda T4 vermeyi reddetti (`assign … Service Unavailable`). Eksik
olan tek şey `rubric_eval.py`'nin base ile adapter'ı yan yana koyduğu koşu.

Bu kılavuz onu 1660 Ti'da koşturur. **WSL2 içindeki bash**, PowerShell değil.

Referans: [`PERSONA_RUNBOOK.md`](PERSONA_RUNBOOK.md) aynı makinedeki persona
hattını anlatır; buradaki tek iş ölçüm.

---

## 0. Önce kartı boşalt

6 GB'lık kartta MLC tek başına 5.4 GB tutuyor. Konteynerler ayaktayken eval
OOM'la ölür, ve hata mesajı bunu söylemez — model yükleme sırasında anlamsız
bir tahsis hatası olarak çıkar.

```bash
cd ~/dev/mf-capstone/mf-inference      # kutudaki yol neyse
docker compose stop mlc llamacpp
nvidia-smi                              # kart boş mu, gözle doğrula
```

Gateway, tünel ve observability GPU tutmuyor, onlar kalabilir. `LLM_BASE_URL`
bu sürede cevapsız kalacak; backend'in `POST /llm/generate`'i 503 döner, bu
desteklenen bir durum, tarayıcı yolu etkilenmez.

## 1. İki dosyayı taşı

İkisi de gitignore'da (`mf-inference/peft/.gitignore`: `data/`, `out/`), yani
`git pull` getirmez:

| Dosya | Boyut | Neden |
|---|---|---|
| `out/colab-pilot/adapter_model.safetensors` + `adapter_config.json` | 47 MB | ölçülecek şey |
| `data/pilot/rubric_eval.jsonl` | 246 KB, 40 satır | tutulmuş set |

Mac'ten, kutuya doğru:

```bash
# Mac'te
cd ~/dev/mf-capstone/mf-inference/peft
scp -r out/colab-pilot  <kutu>:~/dev/mf-capstone/mf-inference/peft/out/
scp data/pilot/rubric_eval.jsonl <kutu>:~/dev/mf-capstone/mf-inference/peft/data/pilot/
```

Eval setini yeniden üretmek yerine kopyalıyoruz: delta ancak **aynı 40 satırda**
anlam taşır.

## 2. Ortam

MLC'nin ortamından **ayrı** bir venv. Sebebi `requirements.txt`'in başında
yazıyor: MLC kendi torch build'ini ve `apache-tvm-ffi`'yi pinliyor, ikisini tek
ortamda çözmek kurulumu bir kez bozdu.

```bash
cd ~/dev/mf-capstone/mf-inference/peft
python3 -m venv .venv-eval && source .venv-eval/bin/activate

# torch CUDA build olmalı — düz PyPI wheel'i CPU-only çözerse ölçüm saatlerce sürer
pip install torch --index-url https://download.pytorch.org/whl/cu121
pip install 'transformers>=4.51,<5' 'peft>=0.11' 'bitsandbytes>=0.43' \
            'accelerate>=0.30' 'datasets>=2.19' sentencepiece protobuf

python -c "import torch;print(torch.cuda.get_device_name(0), torch.cuda.is_available())"
```

Beklenen: `NVIDIA GeForce GTX 1660 Ti True`.

## 3. Koşu

```bash
python rubric_eval.py \
  --data data/pilot/rubric_eval.jsonl \
  --adapter out/colab-pilot \
  --limit 40 \
  --four-bit \
  --out out/pilot_eval.json
```

Tek komut hem base'i hem adapter'ı ölçer ve deltayı kendi basar. **İki ayrı
koşuya bölme** — aynı süreçte, aynı kütüphane sürümleriyle ölçülmesi bu
script'in varlık sebebi.

Süre ölçülmedi. T4'te 40 satırlık forward-only eval 360 saniyeydi; burada
80 üretim (40 base + 40 adapter), her biri `--max-new-tokens 900`, ve kart T4'ün
altında. Saatlik mertebede olmasına şaşırma; arka planda bırak.

## 4. Neye bakılacak

Script'in bastığı delta bloğu. Eşikler `rubric_eval.py`'nin başında:

```
absent_rate 0.89 · schema_valid 0.95 · completed 0.95
present_score_mae 0.77 · hallucinated_quotes 0.013
```

- **`present_score_mae`** — adapter'ın hareket ettirmesi gereken tek sayı.
  Base 0.77, yani 1–5 bandında neredeyse tam bir band sapma. Düşmesi iyi.
- **`absent_rate` ve `schema_valid` taban, hedef değil.** Base zaten 0.89 ve
  0.95. Bunları MAE için takas eden bir adapter sevk edilmez.
- `hallucinated_quotes` — base %1.3. Ürünün iddiası "kanıtı denetleyebilirsin"
  olduğu için uydurma alıntı en pahalı hata.

Sonuç `out/pilot_eval.json`'a yazılır. Sonraki koşularda `--baseline
out/pilot_eval.json` verirsen base'i yeniden ölçmez.

## 5. Tuzaklar

- **`--four-bit` opsiyonel değil.** Varsayılan fp16 ve script'in kendi yardım
  metni "fp16 sunucunun koştuğu şey olduğu için varsayılan" diyor — ama 4B model
  fp16'da bu karta sığmaz. Sığdığı için 4-bit koşuyoruz, tercih ettiğimiz için
  değil. **Çıkan sayı 4-bit'i tarif eder**; rapora yazarken bunu birlikte yaz,
  yoksa fp16 sunucuyla kıyaslandığında sayı sessizce yanlış olur.
- **`transformers` tavanı `<5`.** Eğitim ve eval script'leri 4.5x API'sine göre
  yazıldı. Not: Colab VM'i 5.13.1 ile geldi ve pilot orada koştu — yani eğitim
  ile ölçüm farklı sürümlerde olacak. Bu bilinen bir tutarsızlık; sayı sevk
  edilecekse ikisini aynı sürüme çekip tekrar ölçmek gerekir.
- **Kart boş değilse hata yanıltıcıdır.** OOM, model yükleme sırasında alakasız
  görünen bir tahsis hatası olarak çıkar. `nvidia-smi` ilk bakılacak yer.
- **Exit code 0 kanıt değil.** `out/pilot_eval.json` yazıldı mı ve içinde
  `present_score_mae` var mı, ona bak. Kaggle'da bir koşu boş çıktı dizini
  üzerine COMPLETE yazmıştı.

## 6. Bitince kartı geri ver

```bash
deactivate
cd ~/dev/mf-capstone/mf-inference
docker compose start mlc llamacpp
curl -s localhost:8080/health   # gateway "ok" demeli (Caddyfile'da açık uçlu)
```

---

## Bu sayının anlamı ve anlamadığı

Adapter 15 adım, 60 satır geçişi, epoch 0.15 gördü. Bu bir pilot: hattın uçtan
uca çalıştığını ve bir saatin ne satın aldığını ölçmek için koşuldu, adapter'ın
öğrendiğini iddia etmek için değil. `present_score_mae` düşmezse bu "PEFT
çalışmıyor" demek değil — 60 satır geçişinin bir şey öğretmediği demek.

Asıl koşu `docs/urun-ve-pazarlama.md` §7'deki tutarlılık sayısını besleyecek
olan; onun bütçesi `colab/pilot_math.py`'deki `project_full_run_hours` ile
buradan ölçülen s/row'dan çıkar.
