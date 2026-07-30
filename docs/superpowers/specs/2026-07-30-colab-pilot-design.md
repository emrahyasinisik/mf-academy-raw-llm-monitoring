# Rubrik adapter'ının eğitimini Colab'a taşımak — pilot

**Tarih:** 2026-07-30
**Durum:** tasarım onaylandı, uygulama planı bekliyor
**Kapsam:** `mf-inference/peft/` — Kaggle hattının Colab (ücretsiz tier) karşılığı

## Problem

`rubric-v1` adapter'ının eğitimi Kaggle'da kuruldu ve **koşamıyor**. Ölçülmüş
maliyet satır başına 28-35 saniye (`kaggle/probe/rubric-probe.ipynb`,
2026-07-29), tam koşu 4800 satır-geçişi, yani **38-47 saat**. Bir Kaggle
oturumu 12 saat. 29 Temmuz koşusu 11 saat 24 dakikada 150 adımın 45'ine geldi
ve oturum duvarı iptal etti; hiçbir ağırlık yazılmadı.

Eğitim Colab'a taşınacak, ve **ücretsiz tier** kısıtı sabit: ödeme yok, compute
unit satın alınmıyor.

## Kısıtlar

| kısıt | sonuç |
|---|---|
| Ücretsiz Colab tek **T4** veriyor | Kaggle 2×T4 veriyordu. `torchrun` + DDP (~2x) kolu Colab'da yok. Probe zaten cihaz sayısının adım maliyetini belirlemediğini ölçmüştü (26,0 → 21,2 s/satır), yani kaybedilen şey gerçekleşmiş bir kazanç değil, gerçekleşmemiş bir kol. |
| T4 = sm_75 | Bugünkü hat zaten sm_75 varsayıyor: fp16 + GradScaler, bf16 yok, flash-attention 2 yok. **Hiçbir eğitim kararı değişmiyor.** |
| Dinamik kota, günde ~3-4 GPU-saati | Oturum duvarının yeri bilinmiyor. Epoch bazlı checkpoint (~100 adım ≈ 13-16 saat) bu dilimde işe yaramaz. |
| `colab exec --timeout` varsayılanı 30 s | Eğitim `exec` içinde bloklayarak koşamaz. VM'de arka plana atılıp yoklanacak. |
| `colab run` bitince VM'i yok ediyor | Eğitim için yanlış komut. `new` + `exec` + `download` + `stop` kullanılacak. |

## Bir saatlik koşunun aritmetiği — ve neden pilot

Talep edilen sınır: **bir saatte biten bir koşu.**

```
1 saat                          = 3600 s
model indirme + tokenizasyon + eval + checkpoint payı ≈ 900 s
saf eğitime kalan               ≈ 2700 s
ölçülmüş maliyet                = 28-35 s/satır-geçişi
                                → 77-96 satır-geçişi
```

Effective batch 16'da bu **5-6 optimizer adımı**. LoRA'nın davranış değiştirmesi
için 100+ adım mertebesi gerekiyor. Ölçülmemiş hızlanma kolu (4-bit yerine düz
fp16) 3x tutsa bile ~15-18 adım.

**Sonuç: bir saatlik koşu yayına girecek bir adapter üretemez, ve bu tasarım
öyleymiş gibi davranmayacak.** Üretebileceği şey pilot:

1. Colab yolunun uçtan uca çalıştığının kanıtı — auth → VM → bağımlılıklar →
   veri → eğitim → checkpoint → indirme → eval.
2. **Colab'ın kendi ölçülmüş s/satır sayısı.** Elimizdeki 28-35 s Kaggle'ın
   kartından; tam koşunun kaç oturum sürdüğü ancak Colab'da ölçülen sayıdan
   çıkar.

Bu iki çıktı, tam koşunun planlanabilmesi için gereken ve şu an olmayan şey.

### Sınır veriden değil `--max-steps`'ten gelir

Veri boyutu süreyi *tahmin* eder, `--max-steps` *garanti* eder. Pilotun tek işi
süreyi tutturmak olduğuna göre sınır oraya konur, ve adım sayısı probe'un
ölçtüğü sayıdan hesaplanır:

```
max_steps = floor(2700 / (ölçülen_s_per_satır × batch_size × grad_accum))
```

Elle yazılan bir adım sayısı tahmindir, ve bu repoda bir tahmin zaten 12 saate
ve haftalık GPU kotasının üçte birine mal olmuştur.

## Kapsam dışı

- **`--max-seq-len` kırpmak.** Kırpma soldan olduğu için vakanın başını atar ve
  modele görmediği kanıta atıf yapmayı öğretir, normal görünen bir loss'la.
  2560 ölçülerek seçildi (`measure_tokens.py`), aynen kalıyor.
- **Vaka başına kriter sayısını azaltmak** (satırları kısaltarak ucuzlatmak).
  Üretim 9 kriterlik vaka servis ediyor; 4 kriterlikle eğitmek dağılım
  uyumsuzluğudur, soldan kırpmayla aynı aile.
- **Kaggle hattını silmek.** Duruyor. Notebook'lar, `push.sh` ve
  `kernel-metadata.json`'lar olduğu gibi kalıyor.
- **Tam koşunun kendisi.** Bu tasarım pilotu ve onun ölçtüğü sayıyı teslim eder;
  tam koşunun oturum zinciri o sayı elde edildikten sonra planlanır.

## Mimari — neyin yerine ne geçiyor

| Kaggle | Colab |
|---|---|
| `push.sh` → jsonl + script'ler dataset'in içinde | Script'ler VM'de `git clone` (repo public: `emrahyasinisik/mf-academy-raw-llm-monitoring`). Veri `colab upload` ile Mac'ten. |
| `find_mount(slug, marker)` | Ortadan kalkıyor. Mount yok, yol sabit. |
| `kernel_sources` (train çıktısı → eval) | `colab download` ile adapter Mac'e, sonraki oturuma `colab upload` ile. |
| `machine_shape: NvidiaTeslaT4` | `colab new --gpu T4`. **sm_75 assert'i kalıyor** — Colab başka kart verebilir ve hata yarım saat sonra başka maske takarak gelir. |
| 12 saatlik oturum duvarı | Dinamik kota. Adım bazlı checkpoint. |
| Notebook hücreleri | Mac'ten sürülen düz `.py` script'leri. |

### Veri neden HF'ten değil `colab upload` ile gidiyor

`Emrahisik/rubric-dataset` HF'te public ve `load_dataset` ile çekilebilir. Pilot
yine de yüklemeyi seçiyor, iki sebeple:

1. Pilot seti **tam set değil** (400/40). HF'e ikinci bir konfigürasyon koymak,
   `hf/UPLOAD.md`'nin belgelediği "HF kopyası sessizce ayrışır, hiçbir şey
   yakalamaz" sorununu bir de pilot için açmak olur.
2. Dosya birkaç MB. Yükleme, `load_dataset`'in viewer'ın hazır olmasını bekleyen
   iki "not-yet" durumundan ucuz.

Tam koşu HF'ten çekmeli — orada set gerçekten tam set olur, ve resume aynı veri
sırasını gerektirdiği için sürüm sabitlenmesi ayrıca gerekir. Bu, tam koşunun
tasarımına ait.

## Bileşenler

Yeni dizin: `mf-inference/peft/colab/`

| dosya | ne yapar | neye bağlı |
|---|---|---|
| `README.md` | Hattın künyesi: neden ücretsiz tier, ölçülen sayılar, tuzaklar. `kaggle/README.md`'nin kardeşi | — |
| `requirements-colab.txt` | `transformers>=4.51`, `peft>=0.11`, `bitsandbytes>=0.43`, `accelerate>=0.30`, `datasets` | — |
| `probe_step_cost.py` | VM'de koşar. **İki rejimi** ölçer: bugünkü 4-bit NF4 ve düz fp16 LoRA. Her biri için s/satır ve tepe bellek basar. Eğitmez. | `train_qlora_qwen.py` |
| `run_pilot.sh` | Mac'ten sürücü. Oturum aç → bağımlılık → clone → veri yükle → probe → `--max-steps` hesapla → eğitimi arka plana at → yokla → indir → kapat | `colab` CLI |

`train_qlora_qwen.py`'ye eklenen bayraklar — **hepsinin varsayılanı bugünkü
davranış**, yani Kaggle notebook'ları değişmeden çalışmaya devam eder:

| bayrak | varsayılan | ne yapar |
|---|---|---|
| `--max-steps` | `0` | `>0` ise `TrainingArguments.max_steps`; epoch'ların yerine geçer |
| `--save-steps` | `0` | `>0` ise `save_strategy="steps"`, ve **`load_best_model_at_end=False`** — adım bazlı save ile epoch bazlı eval birlikte tutulamaz |
| `--resume` | kapalı | `trainer.train(resume_from_checkpoint=...)` |
| `--no-4bit` | kapalı | `BitsAndBytesConfig`'i atlar, modeli fp16 yükler. Probe'un ölçtüğü kol |

### `--no-4bit` neden ölçülmeye değer

28-35 s/satır, T4'ün FLOP bütçesine göre kabaca 6-8 kat yavaş. Birinci şüpheli
her matmul'de bitsandbytes'ın NF4 dequant'ı. 4B fp16 ≈ 8 GB; 16 GB T4'te
gradient checkpointing + batch 1 + seq 2560 + yalnız attention'a LoRA ile
sığması makul. Sığmazsa merdivenin bir alt basamağı bugünkü 4-bit — bilgi yine
de kazanılmış olur.

Bu, DataParallel hipotezini 15 dakikada çürüten probe'un aynı disiplini:
hipotez ölçülür, koşuya sokulmaz.

## Pilot konfigürasyonu

| ayar | değer | gerekçe |
|---|---|---|
| set boyutu | 400 / 40 (birleştirilmiş) | Alan başına `--n 200 --n-eval 20`. Tokenizasyon ve eval ucuz kalsın; dağılım bozulmuyor, yalnız daha az vaka |
| `--grad-accum` | 4 (16 yerine) | Aynı satır-geçişi için 4 kat fazla optimizer adımı — loss eğrisinde görülecek bir şey olur |
| `--max-seq-len` | 2560 | Değişmiyor. Ölçülmüş sayı |
| `--max-steps` | probe'tan hesaplanır | Bir saatlik sınırın taşıyıcısı |
| `--save-steps` | 5 | Ölçülen hızda ~75 dk'lık koruma; pilotun kendisi de kotaya kesilebilir |
| `--out-dir` | `out/colab-pilot` | `out/rubric-v1` ile karışmasın — bu çıktı yayına girmez |
| çıktı adı | `colab-pilot` | Adı, ne olmadığını söylüyor |

Veri, backend ayakta üretilir (prompt'lar backend'den çekilir, elle
kopyalanmaz — yerel bir kopya iki taraftan biri düzenlendiği an kayar ve
hiçbir şeyin göndermediği bir prompt'a ayarlanmış adapter çıkar):

```bash
# mf-backend/ içinde
PORT=8090 go run ./cmd/server &

# mf-inference/peft/ içinde
export BASE_URL=http://localhost:8090 TOKEN=<bir hesabın token'ı>
python3 build_dataset.py --domain startup-investability --out-dir data/pilot-investment --n 200 --n-eval 20
python3 build_dataset.py --domain digital-marketing     --out-dir data/pilot-marketing  --n 200 --n-eval 20
python3 merge_rubric_sets.py --input data/pilot-investment --input data/pilot-marketing \
    --out-dir data/pilot
```

**`--out-dir data/pilot` atlanamaz.** `merge_rubric_sets.py`'nin varsayılanı
`--out-dir data --prefix rubric`, yani atlanırsa çıktı `data/rubric_train.jsonl`
olur — tam setin tam olarak kendisi. Pilot seti onu sessizce ezer, ve `data/`
gitignore'da olduğu için geri alınacak bir sürüm yoktur; tam set yeniden
üretilene kadar HF'teki kopya ile yereldeki ayrışmış olur.

Üretecin ayrık bölme assert'i (train/eval örtüşmesi %0) küçük sette de koşar ve
koşmalıdır — bölmenin ayrık olması satır sayısından bağımsız bir doğruluk
koşulu.

## Eval

`rubric-eval` notebook'u da Colab'a taşınıyor. Pilot ölçümü:

- 40 satırlık held-out, **taban + adapter aynı oturumda** — bölmenin asıl
  faydası buydu, aynı kütüphane sürümleri, karşılaştırılabilir sayılar.
- Contrast set pilotta **yok**. 120 çift × 2 model, pilotun bütçesinin çok
  üstünde, ve contrast'ın cevapladığı soru ("kuralı mı öğrendi yoksa bankayı
  mı") 20 adım eğitilmiş bir adapter'da anlamsız.
- Türetilmiş bütçe: Kaggle eval'i ~880 üretim için ~3 saat, yani ~12 s/üretim.
  80 üretim ≈ 16 dakika. **Bu sayı türetilmiş, ölçülmemiş** — koşuda
  doğrulanacak.

Adapter Mac'ten `colab upload` ile gider; `kernel_sources` zinciri yok.

## Akış

```
Mac                                   Colab VM (T4, ücretsiz)
---                                   -----------------------
colab new -s pilot --gpu T4      →    VM ayağa kalkar, keep-alive daemon başlar
colab exec (sm_75 assert)        →    kart doğrulanır, yanlışsa saniyeler içinde düşer
colab install -r requirements    →    transformers>=4.51 vb.
colab exec (git clone)           →    script'ler VM'de
colab upload data/*.jsonl        →    pilot seti VM'de
colab exec -f probe_step_cost.py →    iki rejim ölçülür, s/satır basılır
[Mac: max_steps hesaplanır]
colab exec (nohup train ... &)   →    eğitim arka planda, exec hemen döner
colab exec (log/checkpoint yokla)←    döngü — 30 s'lik exec timeout'una uyar
colab download out/colab-pilot   →    adapter Mac'e
colab exec -f rubric_eval.py     →    taban + adapter, 40 satır
colab download out/pilot_eval.json
colab stop
```

## Kabul kriterleri

Pilot şu dördü sağladığında bitmiştir:

1. `probe_step_cost.py` **iki rejim için de** ölçülmüş s/satır ve tepe bellek
   basar (fp16 kolu OOM olursa o da bir sonuçtur ve öyle kaydedilir).
2. `out/colab-pilot/adapter_model.safetensors` yazılmış ve Mac'e inmiş olur.
   Çıkış kodu 0 yetmez — dosyanın varlığı ayrıca kontrol edilir; Kaggle'da
   `!`'in çıkış kodu hiçbir yere gitmediği için boş bir çıktı dizini
   COMPLETE olarak kaydedilmişti.
3. Eval, taban ve adapter için aynı oturumda üretilmiş sayılar basar.
4. `colab/README.md` **ölçülen** s/satır'ı ve ondan çıkan tam koşu
   projeksiyonunu (saat ve ücretsiz-tier oturum sayısı) kaydeder.

Kriter 4 pilotun asıl teslimatı. Diğer üçü onun koşabilmiş olmasının kanıtı.

## Tuzaklar — kaydedilecek

- `colab exec --timeout` varsayılanı **30 saniye**. Eğitim detached koşar.
- `colab run` bitince VM'i yok eder; eğitim için `new`/`exec`/`stop` kullanılır.
- Colab'ın önyüklü `transformers`'ı Qwen3 için eski olabilir — Kaggle'da bu
  sessizce `unknown architecture` ile düşüyordu. Sürüm assert'le doğrulanır.
- `colab` CLI auth'u tarayıcı OAuth'u açar; ilk komut etkileşimli koşmalıdır.
- Ücretsiz tier pilotun kendisini de kesebilir. `--save-steps 5` bu yüzden var.
- `mlc_llm` isteğin `model` alanını doğrulamıyor — pilot çıktısı serving'e
  hiç sokulmuyor, ama `colab-pilot` adının `rubric-v1` ile karışmaması bu
  yüzden önemli.

## Bilinen sınırlar

- Pilot adapter'ı **hiçbir sayıyı iddia etmez.** ~20 optimizer adımı davranış
  değiştirmez; `present_score_mae`'de görülen herhangi bir hareket gürültüdür ve
  öyle raporlanmalıdır.
- Türetilmiş eval bütçesi (16 dk) Kaggle'ın toplamından oranlanmıştır. Üretim
  hızı kart ve kütüphane sürümüne bağlı; koşuda doğrulanacak.
- Colab'ın ücretsiz tier'da hangi kartı vereceği garanti değil. Tasarım T4
  varsayıyor; L4 gelirse bütün maliyet aritmetiği değişir ve bu iyi haberdir,
  ama planlanamaz.
- Tam koşunun oturum zinciri bu tasarımda **yok**. `--save-steps` ve `--resume`
  onun için gereken alt yapıyı bırakıyor; sürücünün kendisi ayrı bir iş.
