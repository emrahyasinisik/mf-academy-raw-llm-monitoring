# Kutuda entegrasyon — Qwen3-4B tabanını servis et, sonra adapter'ı

GPU kutusunda (Windows + WSL2 + Docker Desktop) koşulacak adımlar. Her şey
**WSL2 içindeki bash**'te, PowerShell'de değil.

Bu dosya iki işi kapsıyor ve **sırası önemli**:

1. **Şu an gereken:** Qwen3-4B tabanını servis et, ki prompt karşılaştırması
   tünel üzerinden koşabilsin.
2. **Sonra gerekecek:** bugün eğitilen persona adapter'ını yayına alma yolu —
   ama ancak ölçümler onu hak ederse, ve bugünkü ölçüm hak etmiyor (aşağıda).

---

## 0. Önce durum tespiti — bu bir sürpriz ve önemli

Tünel şu anda **`persona-v1-q4f16_1-MLC`** servis ediyor. Bu, bugün eğitilen
Qwen3 adapter'ı **değil**: o adapter Kaggle'dan indi ve hiç merge edilmedi,
hiç derlenmedi. Servis edilen şey `PERSONA_RUNBOOK.md`'nin anlattığı **eski
Gemma-2-2B personası**.

Yani bugünkü bütün ölçümler Qwen3-4B üzerindeydi, ürün ise bir 2B Gemma
servis ediyor. İkisi farklı model.

Bunun doğrudan bir sonucu var: **`persona-v1` adı dolu.** Yeni bir build'i aynı
adla derlemek `CLAUDE.md`'nin ilk sıradaki tuzağını kurar — `mlc_llm` isteğin
`model` alanını doğrulamaz, yüklediği tek modelden cevaplar ve sorduğun id'yi
geri yansıtır. Bir model cevap verirken kayıtlar ve Grafana panelleri başkasını
etiketler. Yeni build **başka bir ad** almalı.

Ne servis edildiğini görmek için:

```bash
curl -s -H "X-API-Key: $LLM_API_KEY" https://mlc.visevent.com/v1/models | jq -r '.data[].id'
```

Ve kutuda hangi derlemeler var:

```bash
cd ~/mf-capstone/mf-inference
ls -d models/*-MLC 2>/dev/null
```

---

## 1. Qwen3-4B tabanını servis et — prompt ölçümü için

Prompt karşılaştırmasını tünelden koşabilmek için **saf** bir Qwen3-4B
derlemesi gerekiyor. Adapter gömülmüş bir build (ör.
`qwen3-4b-flutter-q4f16_1-MLC`) taban değildir ve onunla alınan sayı taban
hakkında bir şey söylemez.

### 1a. Zaten var mı

```bash
ls -d models/qwen3-4b*-MLC
```

`qwen3-4b-instruct-q4f16_1-MLC` benzeri, **flutter/persona/rubric geçmeyen** bir
dizin varsa 1c'ye atla.

### 1b. Yoksa derle

Taban modelin kendisi merge gerektirmiyor — gömülecek bir adapter yok. Doğrudan
derlenir:

```bash
cd ~/mf-capstone/mf-inference

# base'i HF cache'ine indir (yoksa)
python3 -c "from huggingface_hub import snapshot_download; \
  snapshot_download('Qwen/Qwen3-4B-Instruct-2507')"

# merged-fp16 yerine dogrudan cache'teki snapshot'i goster
SNAP=$(python3 -c "from huggingface_hub import snapshot_download; \
  print(snapshot_download('Qwen/Qwen3-4B-Instruct-2507'))")
cp -r "$SNAP" models/qwen3-4b-instruct-fp16

peft/build_mlc.sh --name qwen3-4b-instruct --merged models/qwen3-4b-instruct-fp16
```

Çıktı: `models/qwen3-4b-instruct-q4f16_1-MLC`, ~20 dakika.

`--merged` bayrağı zorunlu: varsayılanı `models/merged-fp16` ve o dizin bir
adapter merge'inin çıktısı. Vermezsen ya olmayan bir dizini arar ya da
başkasının merge'ini derler.

### 1c. Servis et

```bash
MLC_MODEL=/models/qwen3-4b-instruct-q4f16_1-MLC \
  docker compose up -d --force-recreate mlc
docker compose logs -f mlc          # ilk acilista kernel derlemesi ~1 dk
```

Doğrula — **id'nin doğru geldiğini görmek yetmez**, `mlc_llm` sorduğun id'yi
yansıtır. Yüklenen şeyi log'dan oku:

```bash
docker compose logs mlc | grep -i "model\|loaded" | head
curl -s -H "X-API-Key: $LLM_API_KEY" https://mlc.visevent.com/v1/models | jq -r '.data[].id'
```

> **Not:** Bu adım eski persona build'ini kapatır. Panel ve Render'daki backend
> bu süre boyunca Gemma personası yerine ham Qwen3 tabanı görür. Kalıcı bir
> değişiklik değil — 1c'yi eski `MLC_MODEL` ile tekrarlayarak geri alınır.

> **`MLC_MODEL`'i komut satırında değil `.env`'de değiştir.** Yukarıdaki satır
> yalnız o `up` çağrısı için geçerlidir. Konteyner `restart: unless-stopped`
> olduğu için bir yeniden başlatmayı atlatır — komut yaratılışta sabitlenir —
> ama bir sonraki `up` / `--force-recreate` / `down` `.env`'i yeniden okur ve
> eski değere döner. `.env` hâlâ `persona-v1-q4f16_1-MLC` diyorsa dönülen şey
> **Gemma personasıdır**, ve dönüş sessizdir: `mlc_llm` sorulan id'yi yansıttığı
> için completion'lar hiçbir şey belli etmez. Ne yüklendiğini `GET /v1/models`
> söyler — orası isteği değil, sürecin başlatıldığı yolu döner.

---

## 2. Persona adapter'ı — ölçüm hak ederse

**Bugünkü hâliyle hak etmiyor.** Kayda geçsin diye, `persona-measure`,
100 satırlık validation seti:

| metrik | taban | ckpt-40 | ckpt-48 |
|---|---:|---:|---:|
| `citation_valid` | 1.00 | 1.00 | 1.00 |
| `grounded_format` | 0.64 | 1.00 | 0.99 |
| `asked_when_thin` | **19/28** | **0/28** | **0/28** |
| `decision_match` | 15/72 | 52/72 | 51/72 |

Adapter hiç soru sormuyor. Üç metrikteki kazancın tamamı tek bir davranıştan
geliyor — *her zaman karar ver* — ve o, ürünün var olma sebebi olan davranışın
kaybı. **Bu build derlenmemeli.** Aşağısı, düzeltilmiş bir koşu geldiğinde
izlenecek yol.

Ama "düzeltilmiş koşu" muhtemelen bir eğitim koşusu değil. `persona-prompt`
kernel'i aynı 100 satırda, aynı tabanda, tek değişken system turn olacak şekilde
`persona_v2.txt`'i ölçtü:

| metrik | taban | **prompt v2** | adapter ckpt-40 |
|---|---:|---:|---:|
| `citation_valid` | 1.00 | 1.00 | 1.00 |
| `grounded_format` | 0.64 | **1.00** | **1.00** |
| `asked_when_thin` | 0.68 | **0.00** | **0.00** |
| `decision_match` | 0.21 | 0.51 | 0.72 |

Bir prompt dosyası QLoRA koşusunun kazancının neredeyse tamamını **ve kaybının
tamamını** yeniden üretti. Kusur eğitim verisinde ya da hiperparametrelerde
değil, talimatta: çıktı sözleşmesini sıkılaştıran her müdahale — ister LoRA
ister prompt — önce soru sorma davranışını harcıyor. `v2` de kendi ölçütüne
göre reddedildi (`peft/prompts/README.md`). İkinci bir T4 koşusu satın almadan
önce cevaplanacak soru bu, ve `--base-only` ile bir prompt denemesinin bedeli
sıfıra yakın.

### 2a. Adapter'ı indir

```bash
cd ~/mf-capstone/mf-inference
kaggle kernels output emrahik/persona-qlora -p /tmp/pq
ls /tmp/pq/out/persona-v1/adapter_model.safetensors
```

### 2b. Merge

```bash
cd peft && source .venv/bin/activate
python3 merge_adapter.py --adapter /tmp/pq/out/persona-v1 \
                         --out ../models/persona-qwen-fp16
```

`--base-model` **yazma**. Script base'i adapter'ın kendi `adapter_config.json`'ından
okuyor; elle verirsen uyuşmazlık kontrolü ona karşı koşar. (Eskiden varsayılan
`google/gemma-2-2b-it`'ti ve bu komut reddedilirdi.)

Base fp16 olarak **CPU'ya** yükleniyor, kart boşta kalsın diye. ~5 GB yazar.

### 2c. Derle — ve adı çakıştırma

```bash
cd ~/mf-capstone/mf-inference
peft/build_mlc.sh --name persona-qwen-v1 --merged models/persona-qwen-fp16
```

**`--name persona-v1` KULLANMA.** O ad Gemma build'inde. Yukarıdaki 0. bölüm
neden önemli olduğunu anlatıyor: aynı adı taşıyan iki farklı model, kayıtlarda
ayırt edilemez.

### 2d. Servis et ve ölç

```bash
MLC_MODEL=/models/persona-qwen-v1-q4f16_1-MLC \
  docker compose up -d --force-recreate mlc
```

Ölçüm **kutuda** koşar, tünelden değil — sebebi aşağıda, ve tahmin değil ölçüm:

```bash
cd ~/mf-capstone/mf-inference/peft && source .venv/bin/activate
source ../.env                              # LLM_API_KEY
export LLM_BASE_URL=http://localhost:8080   # gateway, 127.0.0.1'e publish edilmiş
python3 -u persona_eval.py \
    --before /models/qwen3-4b-instruct-q4f16_1-MLC \
    --after  /models/persona-qwen-v1-q4f16_1-MLC \
    --limit 100
```

Tek taraf ölçmek için `--base-only` (tabanın kendi sayısını almak, ya da bir
prompt denemesi): `--after` düşer, çıktı `--out`'a yazılır.

### Neden tünelden değil

Mac'ten tünel üzerinden denendi (3 Ağu 2026) ve koşu ilk satırlarda öldü:

```
inference call failed (524):
error code: 524
```

Altı satırlık zamanlama: **çalışanlar 9-20 sn, çökenler 125-130 sn'de 524.**
524 Cloudflare'ın origin zaman aşımı: proxy ~100 sn'de bağlantıyı kesiyor,
origin hâlâ üretirken. `persona_eval.py` ilk HTTP hatasında çıktığı için **tek
bir uzun satır 100 satırlık koşunun tamamını çöpe atıyor**.

Hangi satırın çökeceği kestirilemiyor, ve bu önemli: prompt boyutları arasında
anlamlı fark yok (2109-2345 karakter) *ve* aynı satır bir denemede 20 sn'de
dönüp iki deneme sonra 524 aldı. Yani sebep girdi değil, satır da değil —
zamana bağlı bir şey. Üretim hızı ~35 tok/sn ve tavan 1024 token olduğuna göre
en kötü ~30 sn beklenirdi; 100 sn'nin aşılması açıklanmış değil. Kuyruk mu,
prefill mi, kartın o anki durumu mu — kutuda, CDN gürültüsü olmadan koşan ilk
tam pass gösterir.

Akış (`stream: true`) 524'ü muhtemelen aşardı, ama yanlış işi düzeltmek olurdu:
ölçüm kutunun konteynerine karşı koşuyor, araya CDN girmesinin tek sebebi
Mac'ten koşulması. Kutuda tünel yok, sorun da yok.

### Bunun ürün tarafındaki karşılığı — ve o bizim sorunumuz

Aynı ~100 sn duvarı Render'daki backend için de var, ve orada gizleniyor:

| yer | değer |
|---|---|
| `mf-backend/.env` → `LLM_TIMEOUT` | 120s |
| Cloudflare proxy | ~100s |
| `internal/llm/provider.go` | `Stream: false` |
| aynı dosya, tanınmayan kod | `"inference host returned %d"` |

Backend'in bütçesinin son ~20 saniyesi erişilemez — Cloudflare önce keser — ve
kullanıcı **"inference host returned 524"** görür: kutuyu suçlayan, aslında bir
CDN'in kestiği hata. Uzun bir persona cevabı bunu demo sırasında basabilir.
Ölçüm yolu tünelden çıkınca kaybolmayan tek Cloudflare işi budur.

`--before`'un **varsayılanı yok** ve olmamalı: yanlış bir before tarafı,
adapter'ı başka bir base modele karşı kıyaslar ve bütün farkı adapter'a yazar.
Her iki id de aynı base'i paylaşmalı — yani 1b'de derlediğin taban.

Bu ölçüm `--local`'in yerine geçer, tersi değil: burada ölçülen şey ürünün
gerçekten servis ettiği derleme, nicemlemesiyle birlikte.

### 2e. Yayına alma kapısı

`persona_eval.py` şunlardan biri olursa **"Do not ship this adapter"** basar:

- `citation_valid` düştüyse
- `asked_when_thin` düştüyse

İkincisi bugünkü koşuyu yakalayan kural. Üç metrikteki kazanç, soru sorma
davranışının yerine geçmez.

---

## 3. Hot-swap alternatifi — merge ve derleme olmadan

Adapter'ları karşılaştırmak için 20 dakikalık bir MLC derlemesi gerekmiyor.
`llamacpp` adapter'ı ayrı tensör tutuyor:

```bash
cd ~/mf-capstone/mf-inference

# bir kez: base GGUF
peft/build_gguf.sh --base --hf-base Qwen/Qwen3-4B-Instruct-2507

# her adapter icin: ~30 MB, saniyeler
peft/build_gguf.sh --adapter /tmp/pq/out/persona-v1 --name persona-qwen-v1
```

`--hf-base`'i **yalnız `--base` modunda** vermek gerekiyor; adapter modunda
script base'i adapter'dan okuyor.

Sonra `LLAMA_ALIAS`'ın gerçekten yüklenen base'i adlandırdığından emin ol.
`docker-compose.yml` varsayılanı artık `qwen3-4b-instruct-gguf` — Gemma
döneminden kalan `gemma-2-2b-it-gguf` düzeltildi. `models/gguf/base.gguf` hâlâ
Gemma ise **önce onu yeniden üret**, yoksa alias yalan söyler.

> **Bellek uyarısı.** `docker-compose.yml`'nin "iki motor 6 GB'a sığar"
> aritmetiği **2B modeller için** yazılmış. Qwen3-4B Q4 GGUF ~2,5-3 GB, MLC
> tarafı da 4B — ikisini aynı anda 4B ile ayakta tutmak muhtemelen sığmaz.
> Pratikte: `llamacpp` ile ölçerken `docker compose stop mlc`, MLC'ye geçerken
> `docker compose stop llamacpp`.

---

## Sırası

| # | ne | ne zaman |
|---|---|---|
| 1 | Qwen3-4B tabanını derle ve servis et | **yapıldı** (3 Ağu 2026) |
| — | prompt karşılaştırması (`persona-prompt`, Kaggle) | **yapıldı** — `v2` reddedildi |
| 1b | `MLC_MODEL`'i `.env`'de kalıcılaştır | recreate hâlâ Gemma'ya döndürüyorsa |
| 2 | persona adapter'ı merge + derle + ölç | **ancak** düzeltilmiş bir koşu geldiğinde — ve o muhtemelen bir prompt, bir eğitim değil |
| 3 | hot-swap yolu | adapter karşılaştırması gerektiğinde |

Bugünkü adapter için 2'yi koşma. Ölçüm onu reddetti ve sebebi kayıtlı.

Tabanın nicemlenmiş sayısı (`--base-only`, kutuda) henüz alınmadı. Aciliyeti
yok: o sayı yalnızca bir adapter'ın `--before` tarafı olarak lazım ve
karşılaştırılacak adapter yok. Alınacağı zaman kutuda iki şey gerekiyor —
`data/persona_eval.jsonl` (repo'da yok, `peft/.gitignore` `data/`'yı dışlıyor;
`emrahik/persona-dataset`'ten iner) ve `.venv`.
