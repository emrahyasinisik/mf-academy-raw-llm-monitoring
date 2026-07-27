# Flutter ekran üreteci — `qwen3-4b-flutter`

Frontend'in Kod Üretimi ekranını çalıştıran adapter'ın künyesi. Diğer iki
hattan ([rubrik](../README.md), [persona](../PERSONA_RUNBOOK.md)) bir farkı var:
**eğitimi bu repoda koşmadı**, Kaggle'da bir notebook'ta koştu. Bu dizin o
koşuyu geriye dönük belgeliyor ve en azından sözleşmesini doğrulanabilir hale
getiriyor.

## Ne biliniyor

| | |
|---|---|
| Model | `qwen3-4b-flutter-q4f16_1-MLC` — [katalogda](../../../mf-backend/internal/llm/handler.go) sunucu-only |
| Base | Qwen3 4B |
| Donanım | Kaggle **NVIDIA Tesla T4** (`kernel-metadata.json`) |
| Veri | `emrahik/flutter-dataset` → `flutter_screens_train_v7.jsonl`, **139 satır** |
| Notebook | `emrahik/notebook066c67ec08`, private, GPU + internet açık |

T4 de 1660 Ti gibi **sm_75**. Aynı compute capability olduğu için Kaggle'da
alınan bir MLC derlemesi GPU kutusunda geçerli — eğitimi taşımak için tek
gerekçe kartın 16 GB olması değil, kutunun eğitim boyunca inference'sız
kalmaması.

## Ne bilinmiyor

`kaggle kernels pull` ile inen notebook'un **kayıtlı sürümü tek hücre**:
eğitilmiş modeli deneyen bir çıkarım hücresi. Veri setini üreten kod, LoRA
hiperparametreleri ve merge/derleme adımları Kaggle'ın sürüm geçmişinde kaldı,
kod dosyasında değil. `kaggle kernels output` boş döndü — koşu çıktıları da
saklanmamış.

Yani şu an elimizde **v7'nin kendisi var, onu üreten kural yok**. Yeniden
üretmek için notebook'un Kaggle'daki eski sürümlerinden kurtarılması gerekiyor.

## Neden `train_qlora.py` yeniden kullanılamaz

Üstteki [`train_qlora.py`](../train_qlora.py) sadece varsayılanı Gemma değil,
davranışı Gemma-2'ye bağlı: `attn_implementation="eager"` (logit soft-capping
fused kernel'lerde yok), system rolü olmayan bir chat template varsayımı,
256k vocab gerekçesiyle embedding'e LoRA takmama. Qwen3'te bunların hiçbiri
geçerli değil ve `--base-model` vermek bunları düzeltmez — **sessizce yanlış
yapar**, eğitim tamamlanır, loss makul görünür.

Aynen taşınan tek şey fp16 + loss scaling: T4 de sm_75, bf16 yok.

## Dosyalar

| dosya | ne |
|---|---|
| `system_prompt.txt` | Sistem promptunun tek kaynağı, veri setinden bayt bayt çıkarıldı |
| `build_flutter_dataset_v8.py` | v7 → v8: kanıt bloğu, kanonik brief, held-out ayrımı |
| `train_qlora_qwen.py` | Qwen3 QLoRA eğitimi. `../train_qlora.py`'nin Gemma'ya özgü kararları buraya taşınamaz — sebepleri dosyanın başında |
| `flutter_eval.py` | Kanıtın takip edilip edilmediğini ölçer; `hf` ve `openai` arka uçları |
| `verify_contract.py` | Servis edilen sözleşme ile eğitilen sözleşmeyi karşılaştırır |
| `fetch_dataset.sh` | Veri setini Kaggle'dan `data/` altına indirir (data/ gitignore'da) |
| `kaggle-notebook.ipynb` | v7 notebook'unun kayıtlı sürümü — eksik olduğu bilinerek, kanıt olarak |
| `kernel-metadata.json` | v7 koşusunun GPU tipi ve veri kaynakları |

## v8 nasıl eğitiliyor

Kaggle'da, `emrahik/flutter-v8-qlora` notebook'unda. Veri ve script'ler
`emrahik/flutter-dataset` dataset'inin v8 sürümünden geliyor; notebook üç adım
koşuyor — temel model ölçümü, eğitim, adapter ölçümü — böylece "öncesi/sonrası"
tek çalıştırmada ve aynı promptlarla çıkıyor.

```
base:      Qwen/Qwen3-4B-Instruct-2507   (non-thinking; hybrid sürüm <think> sızdırır)
115 satır × 6 epoch, effective batch 8 → ~86 adım
max-seq-len 2560                        (ölçüldü: en uzun satır ~2130 token)
fp16 + loss scaling                     (T4 = sm_75, bf16 yok — 1660 Ti ile aynı)
LoRA r16/α32, yalnız q,k,v,o_proj       (115 satırda MLP'yi de açmak overfit daveti)
```

Base model kimliği bir **varsayım**: v7'nin hangi Qwen3 sürümüyle eğitildiği
kayıtlı değil, notebook'un kurtarılmış hücresi yalnızca bir chat template
kullanıldığını gösteriyor. `--base-model` ile tek kelimede değişir.

### Ölçüm neye bakıyor

`flutter_eval.py`'nin sonucu tek satır:

| metrik | ne diyor |
|---|---|
| `followed_unseen` | **Sonuç bu.** Eğitimde hiç görmediği bir API göçünü (SegmentedButton, DropdownMenu, NavigationBar, withValues) kanıta bakarak yaptı mı — n=7 |
| `followed_seen` | Eğitimde gördüğü göçler (FilledButton, titleLarge) — n=6 |
| `clean` / `fenced` / `complete` | v7'nin kapısı, korunuyor: kanıt kazancı kod kalitesine mal olduysa görünsün |

İkisi arasındaki **fark** ezber ölçüsüdür. Eşitlerse model kuralı öğrenmiştir;
arası açıksa iki API adı ezberlemiştir ve kanıt hattı henüz maliyetini
çıkarmıyordur — o durumda backend ajanını yazmadan önce migration çeşitliliğini
artırmak gerekir.

## v8 — kanıtlı sürüm

`build_flutter_dataset_v8.py`, v7'nin brief'lerinin önüne numaralı bir **kanıt
bloğu** koyuyor. Spec'in "üretmeden önce araştırır" iddiası ancak model o bloğu
görerek eğitilirse çıkarımda tek aşamada karşılanabilir; yoksa araya bir damıtma
katmanı koymak gerekir ve o katman da promptu adapter'ın görmediği bir şekle
sokar.

Cevaplar v7'den **değiştirilmeden** geliyor. Yeni Dart yazılmadığı için v8, v7'de
olmayan bir hatayı içeremez. Yalnızca kullanıcı mesajı yeniden yazılıyor.

```bash
python3 build_flutter_dataset_v8.py
```

```
data/flutter_screens_train_v8.jsonl: 136 rows
dropped 3 row(s) using setState — the contract forbids it
row kinds: grounded 62, migration 48, thin 26
```

| satır türü | ne öğretiyor |
|---|---|
| `grounded` | Kanıt, cevabın kullandığı API'leri anlatıyor + kullanmadığı **çeldiriciler**. Çeldirici olmasa model "hepsini kullan" öğrenirdi ki bu hiç okumamakla aynı |
| `migration` | Kanıt "X kaldırıldı, yerine Y" diyor ve cevap Y'yi kullanıyor. Ürünün asıl vaadini taşıyan satır — ve bedava, çünkü v7'nin kodu zaten modern |
| `thin` | Kanıt boş. Cevap aynı. Öğrettiği şey: kaynak yoksa çekirdek API'ye düş, widget uydurma — personanın "tahmin etme, sor" davranışının kod karşılığı |

Kanıt korpusu (`FACTS`) uydurma değil: anahtarları frontend linterinin
[`PATTERNS`](../../../mf-frontend/src/lib/flutterContract.ts) listesinden geliyor.
Bir örneği eğitimden eleyen kural ile kanıtın modele öğrettiği kural aynı
kaynaktan türüyor, yani hattın iki ucu inşa gereği anlaşıyor.

**Düşen 3 satır:** v7'de `setState(` kullanan üç satır var; sözleşme bunu
yasaklıyor ve linter `error` sayıyor. Üzerinde eğitmek, modele denetleyicinin
reddedeceği şeyi üretmeyi öğretmek olurdu. Mekanik olarak yeniden yazmak yerine
düşürüldüler — yanlış bir onarım, 136 satırdan kötüdür.

## Sözleşme doğrulaması

```bash
source ../.env && ./fetch_dataset.sh
python3 verify_contract.py
```

Sistem promptu üç yerde yazılı — veri seti, `system_prompt.txt` ve
[`flutterContract.ts`](../../../mf-frontend/src/lib/flutterContract.ts)'deki
`SYSTEM_PROMPT_SHA256`. Üçü de aynı: `0c1d64de…`. Bu pin daha önce
karşılaştıracak bir şey bulamıyordu, çünkü veri seti yalnızca Kaggle'daydı.

**Kullanıcı mesajının şekli ise hiçbir zaman korunmuyordu ve kaymıştı.** İlk
koşuşta script v7'ye karşı dört uyuşmazlıkla düştü:

| bulgu | detay |
|---|---|
| `Alanlar/İçerik:` | UI bu etiketi gönderiyor; v7'deki etiket `Alanlar:` (ve yalnızca 2 satırda) |
| `Bileşen:` | v7'nin **32/139 satırı** bu etiketle; UI yalnızca `Ekran:` üretebiliyor |
| State değerleri | UI'ın kapalı kümesindeki üç değerin **hiçbiri** v7'de geçmiyordu |
| State kümesi | v7'de ~95 farklı State ifadesi (`flutter_bloc (TimerCubit — Timer.periodic, close'da iptal)` gibi); UI üçe indirmişti |

En sık geçen değer `yok (StatelessWidget).` (45 kez) iken UI
`yok, StatelessWidget.` gönderiyordu — parantez yerine virgül. Bu,
`SYSTEM_PROMPT_SHA256`'nın önlemek için yazıldığı hatanın aynısı; sadece promptun
diğer yarısındaydı ve orada bir hash yoktu.

**Kapatıldı.** v8 State'i UI'ın gönderebileceği üç kanonik değere indiriyor,
`Alanlar/İçerik:` etiketini kullanıyor, ve `STATE_CHOICES` v7'nin kendi
söyleyişine hizalandı. Doğrulayıcı artık v8'e karşı `OK`, v7'ye karşı `FAIL`
veriyor — ikincisi doğru davranış, kayma gerçekten oradaydı.

`Bileşen:` de kapandı: `SUBJECT_KINDS` formda `Ekran`/`Bileşen` seçimi olarak
duruyor, yani 32 satırlık eğitilmiş yetenek artık çağrılabiliyor. Doğrulayıcı
v8'in her etiketine `ok` veriyor.

## Sırada

1. Notebook'un dolu sürümünü Kaggle geçmişinden kurtar → `build_flutter_dataset.py` (v7 üreteci) + `train_qlora_qwen.py` buraya.
2. `internal/codegen` — çıkarımda kanıt bloğunu üreten backend ajanı. `EVIDENCE_HEADER` ve `TURN_INSTRUCTION` şu an v8 üretecinde sabit; ajan gelince `GET /codegen/prompt`'tan çekilmeli, `build_persona_dataset.py`'nin yaptığı gibi.
3. `flutter_eval.py` — kalite kapısı şu an yalnızca frontend linteri. Ölçüm asıl olarak `migration` satırlarına bakmalı: kanıt "X kaldırıldı, yerine Y" derken model X'i mi Y'yi mi yazıyor. Bu, kanıtın okunup okunmadığını gösteren tek doğrudan sayı.
