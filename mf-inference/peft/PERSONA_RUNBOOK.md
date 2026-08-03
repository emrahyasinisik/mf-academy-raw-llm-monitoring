# Persona v1 — GPU kutusunda uçtan uca koşu kılavuzu

> **Kaggle varyantı (3 Ağustos 2026).** Aşağıdaki kılavuz Gemma-2-2B'yi kutuda
> eğitiyor ve eğitim boyunca ürünü çıkarımsız bırakıyor. Ürünün servis ettiği
> base artık **Qwen3-4B-Instruct-2507** ve aynı hat Kaggle'da koşuyor:
> `kaggle/persona/persona-qlora.ipynb`, veri seti `emrahik/persona-dataset`,
> `kaggle/push_persona.sh` ile yayınlanıyor. Kutu boşta kalıyor, T4 de 1660 Ti
> gibi sm_75 olduğu için orada alınan derleme burada geçerli.
>
> O koşu **3 saatlik bir bütçeye** yazıldı ve bütçe `--max-steps` ile tahmin
> edilip `--max-minutes` ile garanti ediliyor. İkisinin farkını `rubric-curve`
> ödedi: adım sayısı ölçülmüş bir s/satır'dan hesaplanmıştı ama satır/adım
> çarpanı yarısı kadar yazılmıştı, koşu oturum duvarına `exit 137` ile çarptı ve
> hiç ağırlık yazılmadı. `--max-minutes` Trainer'dan nazikçe çıkıyor, yani
> `trainer.train()` döner ve `save_pretrained` yine koşar.
>
> Ölçüm de orada: `persona_eval.py --local` ağırlıkları doğrudan yüklüyor, çünkü
> Kaggle'da çıkarım sunucusu yok. Aşağıdaki 7. adımın tünel üzerinden koşan
> hali **yerine geçmiyor** — o, ürünün servis ettiği MLC derlemesini ölçer ve
> yayına alma kararını o verir.

Yatırım personasını (`mf-backend/internal/decision`) eğitip yayına alan tek
oturumluk kılavuz. Baştan sona, hiçbir adımı Mac'te bırakmadan burada koşulur —
tek istisna ölçüm, o tünel üzerinden her yerden koşabilir.

Her şey **WSL2 içindeki bash'te** çalışır, PowerShell'de değil. Docker Desktop
açık olmalı.

Referans: [`README.md`](README.md) hattın tamamını ve rubrik varyantını anlatır;
burada sadece persona yolu, sırayla ve karar noktalarıyla var.

---

## Neden veri seti burada yeniden üretiliyor

`data/` gitignore'da. Mac'te üretilen `persona_train.jsonl` push'la gelmez, ve
zaten gelmesine gerek yok: üreteç sabit tohumlu (`--seed 20260724`) ve prompt'u
çalışan backend'den çekiyor, yani aynı commit'te aynı veri çıkıyor. Dosya
taşımak yerine komutu tekrar koşmak hem daha az adım hem de kanıtlanabilir
biçimde hizalı — kopyalanan bir dosyanın hangi prompt sürümünden üretildiği
dosyanın içine yazmıyor.

---

## 0. Hazırlık — bir kereye mahsus

```bash
cd ~/mf-capstone
git fetch origin
git checkout feat/investment-persona     # merge edildiyse: main
git pull
```

Eğitim ortamı **mlc container'ından ayrı** olmak zorunda; aynı ortamı
paylaştıklarında setuptools üzerinden çakışıyorlar (mlc imajının conda
kullanmasının sebebi tam olarak bu).

```bash
cd mf-inference/peft
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt

python3 -c "import torch;print('CUDA:', torch.cuda.is_available(), torch.cuda.get_device_name(0))"
```

Çıktı `CUDA: True NVIDIA GeForce GTX 1660 Ti` değilse **devam etme** —
`train_qlora.py` kart görmezse zaten çıkıyor, ama saatler sonra değil, burada
öğren. CPU-only torch geldiyse:

```bash
pip install torch --index-url https://download.pytorch.org/whl/cu121
```

Gemma kapılı model, HF hesabıyla erişim onayı gerekiyor:

```bash
huggingface-cli login        # token: huggingface.co/settings/tokens
```

---

## 1. Backend'i kaldır ve token al

Veri üreteci sistem prompt'unu backend'den okuyor, o yüzden ayakta olmalı.

```bash
cd ~/mf-capstone/mf-backend
PORT=8090 go run ./cmd/server &          # migration'lar açılışta koşar

export BASE_URL=http://localhost:8090
export TOKEN=$(curl -s -X POST $BASE_URL/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"baseline@example.com","password":"baseline-pass-12345"}' | jq -r .access_token)
echo "${TOKEN:0:20}..."
```

Token boş çıkarsa hesap yok demektir; aynı gövdeyle `/auth/register` çağır,
sonra login'i tekrarla. Sağlık ucunun adı `/health` (`/healthz` değil).

---

## 2. Veri setini üret

```bash
cd ~/mf-capstone/mf-inference/peft
source .venv/bin/activate
python3 build_persona_dataset.py --n 800 --n-eval 100 --clarify-share 0.3
```

Beklenen:

```
  data/persona_train.jsonl: 800 examples (+ data/persona_train_meta.jsonl)
  data/persona_eval.jsonl: 100 examples (+ data/persona_eval_meta.jsonl)

persona dataset: 900 examples, 284 clarify (32%)
```

`_meta` dosyaları yer gerçeğini taşıyor; ölçüm adımı onları okuyacak, silme.

---

## 3. Kartı boşalt

6 GB'ı eğitim ile MLC paylaşamaz. Bu komuttan sonra **tünelin arkasındaki model
kapanır** — Render'daki backend ve panel bu süre boyunca inference'sız kalır,
eğitim bitene kadar öyle kalacağını bilerek başlat.

```bash
cd ~/mf-capstone/mf-inference && docker compose stop mlc
```

---

## 4. Eğit

```bash
cd peft && source .venv/bin/activate
python3 train_qlora.py \
  --train data/persona_train.jsonl \
  --eval  data/persona_eval.jsonl \
  --out-dir ../models/persona-v1 \
  --epochs 3
```

İlk saniyelerde şunu **doğrula ve ancak sonra masadan kalk**:

```
device: NVIDIA GeForce GTX 1660 Ti  sm_75  6.0 GB
bf16 unsupported on this card; training in fp16 with loss scaling
trainable: 5,914,624 of 2,620,342,528 (0.226%)
```

`trainable` %1'in üstündeyse LoRA yanlış katmanlara takılmıştır; durdur, koşuyu
harcama.

800 örnek × 3 epoch, effective batch 16 → **150 optimizer adımı**, bu kartta
**1.5–3 saat**. OOM gelirse sırayla: `--max-seq-len 2048`, sonra `--grad-accum
32`. Çıktı `../models/persona-v1/`, ~13 MB.

---

## 5. Merge et

Varsayılan `out/adapter`'ı gösterir; persona yolunu vermezsen yanlış (ya da
olmayan) adapter'ı gömer:

```bash
python3 merge_adapter.py --adapter ../models/persona-v1
```

Base'i fp16 olarak **CPU'ya** yükler (kart boşta kalsın diye), LoRA'yı gömer,
`../models/merged-fp16/` altına ~5 GB yazar.

---

## 6. Yayınla — iki yol

Değerlendirme için hot-swap yeter; üretim için MLC derlemesi gerekir. İkisinin
sınırı [`README.md`](README.md)'nin "Hangi motor ne zaman" bölümünde.

```bash
cd ~/mf-capstone/mf-inference
docker compose up -d mlc                    # container lazım, model yüklemesi değil
peft/build_mlc.sh --name persona-v1
```

Çıktı `models/persona-v1-q4f16_1-MLC/`, model id **`persona-v1-q4f16_1-MLC`**.
Script conversation template'i cache'teki base modelden okur, tahmin etmez —
yanlış template, çıktısında turn işaretleri görünen ve kötü fine-tune gibi
duran bir model üretir.

Servis et:

```bash
MLC_MODEL=/models/persona-v1-q4f16_1-MLC docker compose up -d --force-recreate mlc
docker compose logs -f mlc                  # ilk açılışta kernel derlemesi ~1 dk
```

---

## 7. Ölç — `compare.py` değil, `persona_eval.py`

`compare.py` rubriği `/analysis/trial`'dan geçirir; persona öyle ölçülemez,
çünkü çalışma anında **canlı** araştırır ve her koşuda kanıt değişir.
`persona_eval.py` kanıtı sabitler: held-out seti doğrudan inference host'un
OpenAI ucuna, agent'ın göndereceği mesajlarla atar ve `_meta`'daki yer
gerçeğine karşı puanlar.

```bash
cd peft && source .venv/bin/activate
export LLM_BASE_URL=https://mlc.visevent.com     # /v1 olmadan
export LLM_API_KEY=<gateway-secret>
python3 persona_eval.py --before gemma-2-2b-it-q4f16_1-MLC \
                        --after persona-v1-q4f16_1-MLC --limit 40
```

Bu adım GPU kutusunda oturmayı gerektirmez; tünel açıkken Mac'ten de koşar.

Dört sayı, önem sırasıyla:

| metrik | ne diyor |
|---|---|
| `citation_valid` | uydurma `[n]` atıfları gitti mi — **kapı bekçisi** |
| `grounded_format` | karar KARAR/SKOR biçiminde mi |
| `asked_when_thin` | kanıt inceyken tahmin yerine soruyor mu |
| `decision_match` | verdict bandı kanıtla uyuşuyor mu |

`citation_valid` düşerse script zaten "**do not ship this adapter**" basar.
Biçimi düzeltip hâlâ olmayan kanıta atıf yapan bir build, hiç build
olmamasından kötüdür: savunulamaz kararı kendinden emin biçimde üretir.

---

## 8. Panelde aktive et — sadece sayılar iyiyse

Admin rolü gerekir: `.env`'de `ADMIN_EMAIL` ayarlı olmalı ve o hesapla giriş
yapılmış olmalı.

```bash
# Adapter'ı bir kez kaydet
curl -s -X POST $BASE_URL/admin/adapters \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"persona-v1","base_model":"google/gemma-2-2b-it","lora_rank":16,"notes":"yatırım personası v1"}'

ADAPTER_ID=<yukarıdaki id>

curl -s -X PATCH "$BASE_URL/admin/adapters/$ADAPTER_ID/status" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"ready","mlc_model_id":"persona-v1-q4f16_1-MLC"}'

curl -s -X POST "$BASE_URL/admin/adapters/$ADAPTER_ID/activate" \
  -H "Authorization: Bearer $TOKEN"
```

`build_mlc.sh --adapter-id <uuid>` verirsen script statüyü kendi bildirir ve
panelin ilerleme sütunu başka makinede koşan build'i takip eder.

---

## Sık takılınan yerler

| belirti | sebep |
|---|---|
| `bind: address already in use` (8090) | backend zaten açık; eskisini kapat ya da portu değiştir |
| `GET /decision/prompt` 401 | `TOKEN` boş veya süresi dolmuş — login'i tekrarla |
| `GET /decision/prompt` 404 | eski commit'teki backend koşuyor; `git pull` ve yeniden başlat |
| `no CUDA device visible` | mlc container kartı tutuyor ya da torch CPU-only |
| eval/meta uzunlukları farklı | ikisini birlikte yeniden üret, tek dosyayı elle taşıma |
| çıktıda turn işaretleri | MLC derlemesinde yanlış conversation template |
