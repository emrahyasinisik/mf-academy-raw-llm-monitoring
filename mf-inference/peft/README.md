# PEFT pipeline — rubrik şemasına uyum için QLoRA

Bu klasör, `gemma-2-2b-it` modelini analiz rubriğinin çıktı şemasına uyacak
şekilde eğitir. Teorisi için [docs/peft-nedir.md](../../docs/peft-nedir.md).

## Neden bu eğitim yapılıyor

Tahmine dayanmıyor, ölçüme dayanıyor. Base model gerçek bir deck üzerinde
5 kez koşturuldu ([baseline-trial.sh](../../mf-backend/scripts/baseline-trial.sh)):

| Ölçüm | Base model | Anlamı |
|---|---|---|
| `stddev_score` | **0** | Tutarlılık zaten mükemmel — bozmamak lazım |
| `schema_valid_rate` | **0** | Her cevabı ` ```json ` bloğuna sarıyor |
| `absent_rate` | **0** | Asıl sorun ↓ |

Üçüncüsü kritik. Deck'te hiç geçmeyen bir kritere sorulduğunda model
gerekçesine *"Metinde rakipler hakkında bilgi bulunmuyor"* yazıyor ve **yine de
5 üzerinden 3 veriyor.** `evidence_found=false` yolunu hiç kullanmıyor.

Sonucu: coverage her zaman 1.00 raporluyor, her rapor uydurma orta puanlar
içeriyor, ve ürünün merkezî iddiası — bir reddin savunulabilir olması —
pratikte doğru değil.

Eğitim tam olarak bu iki davranışı hedefliyor. Model zaten içeriği bulabiliyor;
öğretilmesi gereken şey biçim disiplini, ki LoRA'nın en iyi olduğu iş bu.

## Dosyalar

| Dosya | İş |
|---|---|
| `build_dataset.py` | Eğitim verisi üretir — yer gerçeği bilinen sentetik vakalar |
| `train_qlora.py` | QLoRA eğitimi (4-bit NF4 donuk base + ~6M eğitilen parametre) |
| `merge_adapter.py` | LoRA'yı base ağırlıklara gömer, fp16 HF modeli yazar |
| `build_mlc.sh` | q4f16_1'e kuantize eder ve MLC serving config'i üretir |
| `compare.py` | Önce/sonra ölçer ve **verdict basar** |

---

## Windows tarafında ne yapacaksın

Her şey **WSL2 içindeki bash'te** koşuyor (PowerShell'de değil). Docker Desktop
açık olmalı.

### 0. Hazırlık — bir kereye mahsus

```bash
cd ~/mf-capstone            # repo nerede duruyorsa
git fetch origin
git checkout feat/rubric-analysis-engine
git pull

# Eğitim ortamı, mlc container'ından AYRI olmalı.
# Aynı ortamı paylaştıklarında setuptools üzerinden çakışıyorlar —
# mlc imajının conda kullanmasının sebebi tam olarak bu.
cd mf-inference/peft
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt

# torch CPU-only geldiyse CUDA'lısını zorla:
#   pip install torch --index-url https://download.pytorch.org/whl/cu121
python3 -c "import torch;print('CUDA:', torch.cuda.is_available(), torch.cuda.get_device_name(0))"
```

Son satır `CUDA: True NVIDIA GeForce GTX 1660 Ti` yazmalı. Yazmıyorsa devam etme.

Gemma kapılı bir model, Hugging Face hesabıyla erişim onayı gerekiyor:

```bash
pip install huggingface_hub
huggingface-cli login          # token'ı huggingface.co/settings/tokens adresinden al
```

### 1. Backend'i ayağa kaldır ve token al

Veri üreteci prompt'u backend'den okuyor, o yüzden açık olmalı.

```bash
cd ~/mf-capstone/mf-backend
go run ./cmd/server &          # migration'lar açılışta koşar

export TOKEN=$(curl -s -X POST http://localhost:8090/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"baseline@example.com","password":"baseline-pass-12345"}' | jq -r .access_token)
echo "${TOKEN:0:20}..."        # boş çıkarsa önce /auth/register ile hesap aç
```

### 2. Eğitim verisini üret

```bash
cd ~/mf-capstone/mf-inference/peft
source .venv/bin/activate
python3 build_dataset.py --token "$TOKEN" --n 800 --n-eval 100
```

Beklenen çıktı:

```
  data/train.jsonl: 800 examples
  data/eval.jsonl: 100 examples
rubric: startup-investability v1, 9 criteria
findings: 8100 total, 2279 absent (28%)
```

`absent` oranı %15'in altına düşerse script uyarır — o oran düşükse öğretilmesi
gereken davranış yeterince örneklenmemiş demektir.

### 3. Eğit

```bash
python3 train_qlora.py --epochs 3
```

**GPU kutusunda MLC container'ını durdur** — 6 GB kartı ikisi paylaşamaz:

```bash
cd ~/mf-capstone/mf-inference && docker compose stop mlc
```

Başlangıçta şunu doğrula:

```
device: NVIDIA GeForce GTX 1660 Ti  sm_75  6.0 GB
bf16 unsupported on this card; training in fp16 with loss scaling
trainable: 5,914,624 of 2,620,342,528 (0.226%)
```

`trainable` yüzdesi %1'in üstündeyse LoRA yanlış katmanlara takılmış demektir.

800 örnek × 3 epoch, effective batch 16 → **150 optimizer adımı**. Bu kartta
tahminen 1.5–3 saat. OOM alırsan sırayla: `--max-seq-len 2048`, sonra
`--grad-accum 32 --batch-size 1`.

Çıktı: `out/adapter/` — yaklaşık **13 MB**. Adapter dediğimiz şey bu.

### 4. Merge et

```bash
python3 merge_adapter.py
```

Base'i fp16 olarak CPU'ya yükler (kart boşta kalsın diye), LoRA'yı gömer,
`../models/merged-fp16/` altına ~5 GB yazar. `models/` klasörü container'a
bind-mount edilmiş durumda, derleme oradan okuyacak.

### 5. MLC'ye derle

```bash
cd ~/mf-capstone/mf-inference
docker compose up -d mlc            # container'a ihtiyaç var, model yüklemesi değil
peft/build_mlc.sh --name tuned-v1
```

Script conversation template'i **cache'teki base modelden okuyor**, tahmin
etmiyor — yanlış template, çıktısında turn işaretleri görünen bir model üretir
ve bu kötü bir fine-tune gibi görünür.

Çıktı: `models/tuned-v1-q4f16_1-MLC/`, model id `tuned-v1-q4f16_1-MLC`.

### 6. Yeni modeli servis et

```bash
MLC_MODEL=/models/tuned-v1-q4f16_1-MLC docker compose up -d --force-recreate mlc
docker compose logs -f mlc          # ilk açılışta kernel derlemesi için ~1 dk bekler
```

### 7. **Ölç** — bu adımı atlama

```bash
cd peft && source .venv/bin/activate
python3 compare.py --after tuned-v1-q4f16_1-MLC --token "$TOKEN"
```

Aynı vakayı iki modelle koşturur ve şunu basar:

```
metric                 before      after       change
absent_rate              0.00       0.??       ▲ +0.??
schema_valid_rate        0.00       0.??       ▲ +0.??
completed                0.80       0.??
stddev_score             0.00       0.??
```

Sonunda verdict var ve **`absent_rate` kapı bekçisi**. Biçimi düzeltip hâlâ
olmayan kanıta puan veren bir build, hiç build olmamasından kötüdür: savunulamaz
raporu kendinden emin şekilde üretir.

### 8. Panelde aktive et — sadece verdict "candidate" ise

```bash
# Adapter'ı kaydet (bir kez)
curl -s -X POST http://localhost:8090/admin/adapters \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"tuned-v1","base_model":"google/gemma-2-2b-it","lora_rank":16,"notes":"ilk build"}'

# Hazır olarak işaretle ve aktive et
ADAPTER_ID=<yukarıdaki id>
curl -s -X PATCH "http://localhost:8090/admin/adapters/$ADAPTER_ID/status" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"status":"ready","mlc_model_id":"tuned-v1-q4f16_1-MLC"}'

curl -s -X POST "http://localhost:8090/admin/adapters/$ADAPTER_ID/activate" \
  -H "Authorization: Bearer $TOKEN"
```

Bu isteklerin **admin rolü** gerektirdiğini unutma — `.env`'de `ADMIN_EMAIL`
ayarlı olmalı ve o hesapla giriş yapılmış olmalı.

`build_mlc.sh --adapter-id <uuid>` verirsen script statüyü kendisi bildirir,
panelin ilerleme sütunu başka makinede koşan build'i takip eder.

---

## Bilinen sınırlar

- **Çift kuantizasyon kaybı.** Adapter 4-bit NF4 base üzerinde eğitiliyor, merge
  fp16 gerektiriyor, sonuç tekrar q4f16_1'e sıkışıyor. İki kayıplı adım ve LoRA
  birincisinin hata profiline göre oturmuş. Loss eğrisinden görünmez —
  `compare.py` tam da bu yüzden var.
- **Eğitim ve servis aynı kartı paylaşamaz.** Eğitim sırasında `mlc` durmalı,
  yani o süre boyunca ürün kapalı.
- **Eğitim verisi sentetik.** Gerçek deck'lerin dil çeşitliliğini tam
  yansıtmıyor. `compare.py` bilerek üretecin hiç görmediği gerçek deck üzerinde
  ölçüyor; asıl doğrulama o.
- **MLC'de hot-swap yok.** MLC modeli önceden derliyor
  ([#2625](https://github.com/mlc-ai/mlc-llm/issues/2625) 2024'ten beri açık,
  [#3281](https://github.com/mlc-ai/mlc-llm/pull/3281) merge edilmeden kapandı).
  Bu motorda "adapter yükle" demek yeni model derlemek demek — dakikalar,
  milisaniye değil. Çözüm motoru değiştirmek oldu; aşağıya bak.

---

## Hot-swap yolu — llama.cpp

MLC'nin yapamadığı şeyi yapan ikinci bir motor duruyor. MLC'nin *yerine* değil,
*yanında*: MLC derlenmiş olduğu için hızlı, llama.cpp adapter'ı ayrı tensörler
olarak tuttuğu için değiştirilebilir. İkisi de aynı kartta, ikisi de aynı
gateway'in arkasında.

Fark, tek bir cümlede: **merge yok, derleme yok, yeniden başlatma yok.**

| | `build_mlc.sh` | `build_gguf.sh` |
|---|---|---|
| ne üretir | tam model (~1.5 GB) | sadece adapter (~30 MB) |
| süre | ~20 dakika | saniyeler |
| aktive etmek | başka model istemek | çalışan sunucuda bir ölçeği 0→1 |

### 0. Taban modeli bir kereye mahsus GGUF'a çevir

```bash
cd ~/mf-capstone/mf-inference
peft/build_gguf.sh --base
```

Bu, eğitimin zaten indirdiği `google/gemma-2-2b-it`'i HF önbelleğinden bulur,
GGUF'a çevirir ve Q4_K_M'e sıkıştırır (~1.7 GB). İkinci bir kopya indirmez.

Sonra motoru kaldır:

```bash
docker compose up -d llamacpp
docker compose logs llamacpp | tail -5      # "base=... adapters=0" görmelisin
```

### 1. Eğittiğin adapter'ı yayınla

Merge etmeden, doğrudan adapter dizininden:

```bash
peft/build_gguf.sh --adapter ../models/adapter-v1 --name tuned-v1
docker compose restart llamacpp
```

Yükleneni doğrula:

```bash
curl -s -H "X-API-Key: $LLM_API_KEY" http://localhost:8080/rt/lora-adapters | jq
# [{"id":0,"path":"/models/gguf/adapters/tuned-v1.gguf","scale":0.0}]
```

`scale: 0.0` doğru. Konteyner adapter'ı **yüklü ama uygulanmamış** olarak
açılıyor — aksi hâlde her yayınladığın adapter üst üste binerdi.

### 2. Panelden aktive et

Panelde build'in yanında `hot-swap` rozeti belirir. **aktive et**'e bas; panel
ölçülen süreyi yazar:

> Canlı geçiş yapıldı — 8 ms. Yeniden başlatma yok, yeniden derleme yok.

Geçiş başarısız olursa panel bunu **söyler**, başarılı gibi göstermez. En olası
sebep: dosyayı yayınladın ama `docker compose restart llamacpp` yapmadın.

### Sınırın tam yeri

llama-server adapter'ları yalnızca **başlangıç bayrağı** olarak alıyor. Yani:

- yüklü adapter'lar arasında geçiş → anlık, yeniden başlatma yok
- **yeni** bir adapter eklemek → o konteynerin yeniden başlatılması gerekir

Yeniden başlatma birkaç saniye (taban model mmap'li), ama yeniden başlatmadır ve
panel bunu gizlemiyor.

### Hangi motor ne zaman

`llm_settings.runtime` bunu seçiyor:

- `mlc` — üretim. Token başına daha hızlı, adapter değişikliği yeniden derleme ister.
- `hotswap` — değerlendirme. Daha yavaş, adapter değişikliği anlık.

İki adapter'ı karşılaştırırken `hotswap`, karar verdikten sonra `mlc`'ye derleyip
oraya geç.
