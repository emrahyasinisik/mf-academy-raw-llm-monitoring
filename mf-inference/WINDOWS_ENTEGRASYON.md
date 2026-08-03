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

Bu bittiğinde haber ver — prompt karşılaştırmasını tünelden ben koşarım
(~20 dk, iki prompt × 100 satır).

> **Not:** Bu adım eski persona build'ini kapatır. Panel ve Render'daki backend
> bu süre boyunca Gemma personası yerine ham Qwen3 tabanı görür. Kalıcı bir
> değişiklik değil — 1c'yi eski `MLC_MODEL` ile tekrarlayarak geri alınır.

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

Ölçüm **tünel üzerinden**, Mac'ten de koşar:

```bash
cd peft && source .venv/bin/activate
export LLM_BASE_URL=https://mlc.visevent.com LLM_API_KEY=<gateway-secret>
python3 persona_eval.py \
    --before qwen3-4b-instruct-q4f16_1-MLC \
    --after  persona-qwen-v1-q4f16_1-MLC \
    --limit 100
```

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
| 1 | Qwen3-4B tabanını derle ve servis et | **şimdi** — prompt ölçümü buna bağlı |
| — | prompt karşılaştırması (Kaggle'da koşuyor, tünelde tekrarlanabilir) | — |
| 2 | persona adapter'ı merge + derle + ölç | **ancak** düzeltilmiş bir eğitim koşusu geldiğinde |
| 3 | hot-swap yolu | adapter karşılaştırması gerektiğinde |

Bugünkü adapter için 2'yi koşma. Ölçüm onu reddetti ve sebebi kayıtlı.
