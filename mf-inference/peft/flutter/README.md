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
| `kaggle-v8-train.ipynb` | v8 eğitim koşusu (`emrahik/flutter-v8-qlora`) — v7'de düştüğümüz "notebook yalnız Kaggle'da" durumu tekrarlanmasın diye |
| `kaggle-v8-eval.ipynb` | Yalnız ölçüm (`emrahik/flutter-v8-eval`); adapter'ı eğitim koşusunun çıktısından alır, eğitimi tekrarlamaz |
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

Üç adımın ilk ikisi geçti, üçüncüsü düştü: adapter takarken peft bir dispatcher
zinciri yürüyor ve zincirdeki torchao yoklaması eski bir torchao'da `False`
dönmek yerine hata fırlatıyor (Kaggle 0.10.0, peft 0.16.0 üstünü istiyor).
Eğitim buna hiç değmemişti — 4-bit model zincirin iki adım öncesindeki
bitsandbytes dispatcher'ında eşleşiyor. Ölçüm servisin koşacağı şeyi puanlamak
için fp16 yüklüyor, zinciri sonuna kadar yürüyor ve tökezliyor. `flutter_eval.py`
artık yoklamayı **yalnızca gerçekten hata fırlattığında** susturuyor; torchao'yu
kullanmadığımız için dürüst cevap zaten `False`.

Eğitilmiş adapter sağlamdı, eksik olan yalnızca sayısıydı. `kaggle-v8-eval.ipynb`
o son adımı 3.5 saatlik eğitimi tekrarlamadan koşuyor: adapter'ı `kernel_sources`
ile eğitim koşusunun çıktısından alıyor.

Base model kimliği bir **varsayım**: v7'nin hangi Qwen3 sürümüyle eğitildiği
kayıtlı değil, notebook'un kurtarılmış hücresi yalnızca bir chat template
kullanıldığını gösteriyor. `--base-model` ile tek kelimede değişir.

### Ölçüm neye bakıyor

| metrik | ne diyor |
|---|---|
| `followed_unseen` | Eğitimde hiç görmediği bir API göçünü (SegmentedButton, DropdownMenu, NavigationBar, withValues) kanıta bakarak yaptı mı — n=7 |
| `followed_seen` | Eğitimde gördüğü göçler (FilledButton, titleLarge) — n=6 |
| `clean` / `fenced` / `complete` | v7'nin kapısı |
| `migrations` | Göç satırının üç sonucu: `replacement` / `deprecated` / `neither` |

`followed_unseen` **sonuç olarak tasarlanmıştı ama o işi yapamıyor** — neden
[aşağıda](#v8-sonucu). Bir göç satırının başarısızlığı iki farklı şey olabildiği
için tek boolean yetmiyor: `deprecated`, modele "bu API kaldırıldı" denmişken
eskisine uzanmasıdır ve kanıt bloğunun önlemek için var olduğu hata tam olarak
budur. `neither` ise widget'ı hiç kullanmayan bir ekrandır — modelin brief'i
daralttığı yerdir, hiçbir kanıt onu düzeltmezdi. `followed_*` ikisini aynı
kutuya koyar, `migrations` ayırır.

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

## v8 sonucu

27 Tem 2026, Kaggle T4. 115 satır × 6 epoch, eval loss **0.6179** (5. epoch'ta
0.6191 — son epoch neredeyse hiçbir şey katmadı, 4 epoch yeterli). Aynı 21
prompt, aynı greedy çözme, aynı `transformers 5.14.1 / peft 0.19.1`:

| | temel | adapter | |
|---|---|---|---|
| `clean` | 81.0% | **95.2%** | 4 linter hatası → 1 |
| `complete` | 95.2% | **100%** | kırpılma bitti |
| `fenced` | 100% | 100% | |
| `followed_seen` | 100% | 100% | n=6 |
| `followed_unseen` | 71.4% | 57.1% | n=7 |

Göç satırlarının kırılımı — `followed_unseen`'in gizlediği yer:

| | temel | adapter |
|---|---|---|
| `replacement` | 11 | 10 |
| `deprecated` | 2 | **1** |
| `neither` | 0 | **2** |

**Kanıt hattının iddiası doğrulanmadı.** `followed_unseen`'in sonuç olmasının
şartı temel modelin düşük çıkmasıydı; temel model **71.4%** yaptı. Kanıt takibi
modern bir 4B instruct modelinin zaten yaptığı bir şey, dolayısıyla metriğin
adapter'ın katkısını gösterecek yeri kalmıyor — üstelik n=7'de tek satır 14
puan oynatıyor. Fine-tune'un kanıt okumayı öğrettiğine dair elimizde kanıt yok.

**Adapter'ın gerçek kazancı ev stili:** `clean` 81 → 95.2. v7'nin işi buydu ve
v8 onu kaybetmemiş, iyileştirmiş.

Düşen `followed_unseen`'in tamamı iki `neither` satırından geliyor: adapter o
ekranlarda dropdown'ı hiç yazmamış. Kaldırılmış API'ye uzanma (`deprecated`)
2'den 1'e inmiş — doğru yön ama 13 göç satırında iddia edilecek bir fark değil.
Kalan tek `deprecated` satırı `withValues`: model `withOpacity` yazıyor ve
`clean`'de kalan tek hata da o.

Bu, `complete` 95.2 → 100 ile aynı madalyonun iki yüzü: adapter daha kısa ve hep
kapanan cevaplar yazıyor, karşılığında iki satırda kapsam kaybediyor.

**v8 yine de servis edilecek sürüm**, ama commit mesajındaki gerekçeyle değil.
Değeri kanıt okumayı öğretmesi değil — o bedava geliyor — **adapter'ın çıkarımda
görecegi prompt şekliyle eğitilmiş olması**. Kanıt bloğunu backend ajanı
gönderdiğinde v7 onu hiç görmemiş bir model olarak karşılardı.

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

1. **Merge + MLC derlemesi.** `clean` 81 → 95.2 servis edilmeye değer; adapter `emrahik/flutter-v8-qlora` çıktısında duruyor. T4 de sm_75 olduğu için derleme GPU kutusunda geçerli.
2. **Ölçüm setini büyütmeden kanıt hattına daha fazla yatırım yapma.** n=7'de tek satır 14 puan oynatıyor ve temel model tavana yakın; bu ölçekte hiçbir koşu "kanıt okumayı öğrendi" diyemez. Ya held-out göç çeşitliliği artmalı (özellikle `withValues` — kalan tek `deprecated` satırı o), ya da iddia bırakılmalı.
3. **`neither` satırlarını kovala.** Adapter iki ekranda widget'ı hiç yazmadı. `complete` 95.2 → 100 ile birlikte okununca bu bir kısalma eğilimi; kanıtla ilgisi yok, brief kapsamıyla ilgili.
4. `internal/codegen` — çıkarımda kanıt bloğunu üreten backend ajanı. `EVIDENCE_HEADER` ve `TURN_INSTRUCTION` şu an v8 üretecinde sabit; ajan gelince `GET /codegen/prompt`'tan çekilmeli, `build_persona_dataset.py`'nin yaptığı gibi. Kanıt bloğu ölçülebilir bir kazanç getirmese de gerekli: adapter o şekille eğitildi.
5. v7 üretecini (`build_flutter_dataset.py`) Kaggle geçmişinden kurtar — v8 notebook'ları artık repoda, eksik kalan tek şey o.
