# Rubrik adapter'ı — `rubric-v1`, Kaggle'da

Ürünün rubrik motorunu çalıştıran adapter'ın künyesi. Üstteki
[`README.md`](../README.md) aynı hattın **Gemma + GPU kutusu** sürümünü
anlatıyor; burada olan **Qwen3-4B + Kaggle** sürümü, ve tek adapter iki rubriği
birden öğreniyor.

## Neden Kaggle, neden tek adapter

| soru | cevap |
|---|---|
| Neden kutuda değil | Eğitim boyunca `docker compose stop mlc` gerekiyor; ürün 3+ saat çıkarımsız kalıyor. T4 de 1660 Ti gibi **sm_75**, yani orada alınan derleme kutuda geçerli. |
| Neden Qwen3-4B | Kutunun servis ettiği base zaten o. `llamacpp` tek base + çok LoRA yüklüyor, yani aynı base'i paylaşan adapter'lar tek process'te yan yana yaşıyor. |
| Neden iki rubrik tek adapter | İkisi farklı kriter soruyor ama aynı **davranışı** istiyor: şemayı doldur, vakadan alıntıla, vaka sessizse söyle. Bu davranış rubriğe özgü değil. |

## Ölçüm — temel modelin hali

Bu hattın var olma sebebi burada, ve **bir kez yanlış yazıldı**. Sayılar
`Qwen/Qwen3-4B-Instruct-2507` üzerinde ölçülmüş hali (`out/base_gate.json`,
20 satır, 2026-07-29):

| ölçüm | base | ne diyor |
|---|---|---|
| `present_score_mae` | **0,77** | Kanıtın orada olduğunu görüyor, sonra 1-5 ölçeğinde neredeyse bir bant kaçırıyor. Raporun okurun karar verdiği kısmı bu. Eğitilecek şey bu. |
| `hallucinated_quotes` | **%1,3** | 151 alıntının 2'si vakada birebir geçmiyor. Küçük, ama "atıflar gerçek" iddiasının kendisi. |
| `absent_rate` | **%89** | Kanıt yoksa zaten söylüyor. Kazanılacak yer değil, **taban** — düşerse build yayına alınmaz. |
| `schema_valid` | **%95** | Biçim disiplini zaten var. Yine taban. |

Önceki hali `absent_rate 0 / schema_valid 0` diyordu. O ölçüm gerçekti ama
başka bir modelin: `gemma-2-2b-it`, ürün rotasından, [`../README.md`](../README.md).
Base Qwen3-4B'ye geçince sayılar taşınmadı, cümleler taşındı — ve hattın tüm
gerekçesi ölçülmemiş bir kusurun üstünde durdu.

Bunu yakalaması gereken kapı vardı ve doğru ölçtü: eğitim notebook'unun 1.
bölümü. Ama sonucu `out/base_gate.json`'a yazıp geçiyordu, hiçbir hücre
okumuyordu, ve ilk koşu tabanın işi zaten yaptığı bilgisinin üstünden eğitime
girdi. Kapı artık `assert`.

Yani Flutter v8 ile fark, sanıldığı kadar büyük değil: orada da base zaten
kanıtı takip ediyordu. Aradaki tek gerçek fark, bunun burada eğitimden **önce**
görülmüş olması.

## Dosyalar

| dosya | ne |
|---|---|
| [`train/rubric-train.ipynb`](train/rubric-train.ipynb) | Ucuz taban kapısı → eğitim. **Ölçülen maliyetle 38-47 saat**, aşağıya bak — bu haliyle bir oturuma sığmıyor |
| [`eval/rubric-eval.ipynb`](eval/rubric-eval.ipynb) | Taban + adapter, tek oturumda, contrast dahil. ~3 saat |
| [`probe/rubric-probe.ipynb`](probe/rubric-probe.ipynb) | Adım maliyetini ölçer, eğitmez. ~15 dakika |
| `*/kernel-metadata.json` | GPU tipi ve girdiler. `machine_shape` **atlanamaz** — atlanırsa Kaggle P100 verir, sm_60, 4-bit NF4 çalışmaz |
| [`DATASHEET.md`](DATASHEET.md) | Veri setinin künyesi — Gebru'nun 7 kategorisi. Bilinen zayıflıklar dahil |
| [`push.sh`](push.sh) | Veri setini + script'leri yayınlar; notebook ayrı push'lanır |
| [`../build_contrast_set.py`](../build_contrast_set.py) | Tek kriteri bozan eş vakalar — ezber ile kural arasını ayırır |
| [`../train_qlora_qwen.py`](../train_qlora_qwen.py) | Qwen3 QLoRA. `../train_qlora.py`'nin Gemma'ya özgü kararları buraya taşınamaz |
| [`../rubric_eval.py`](../rubric_eval.py) | Kaggle'da koşan ölçüm. `compare.py`'nin kardeşi, yerine geçeni değil |
| [`../measure_tokens.py`](../measure_tokens.py) | `--max-seq-len`'i ölçer; tokenizer'lar arası taşınmaz |
| [`../merge_rubric_sets.py`](../merge_rubric_sets.py) | İki rubriğin setlerini deterministik karıştırır |

## Adım maliyeti — bir oturuma sığmıyor

Bu bölümdeki "~5,5 saat" uzun süre bir **tahmindi** ve Gemma-2-2B hattından
geldi. Ölçüldüğünde yanlış çıktı, ve yanlışlığı bir oturuma mal oldu.

`emrahik/rubric-qlora`'nın 29 Temmuz koşusu OOM değildi — temiz eğitiyordu,
**adım başına ~910 saniye**. 11 saat 24 dakikada 150 adımın 45'ine geldi, sonra
Kaggle'ın 12 saatlik oturum duvarı iptal etti. Hiçbir ağırlık yazılmadı, yani
eval notebook'unun `kernel_sources` ile alacağı bir şey de olmadı.

Logdaki ilk ipucu şuydu: `train_qlora_qwen.py` kendi aritmetiğiyle `~300
optimizer adımı` bastı, Trainer'ın bar'ı `150`'ye koştu. Trainer'ın sayısı tam
olarak iki cihaz gördüğünde yarıya iner, ve gerçekten iki cihaz var:
`machine_shape: NvidiaTeslaT4` bir **2×T4** makine veriyor (`device_count: 2`).
Buradan çıkan hipotez, `device_map={"": 0}` ile tek karta sabitlenmiş 4-bit
modelin `DataParallel`'e sarılıp her microbatch'te replike edildiğiydi.

[`probe/rubric-probe.ipynb`](probe/rubric-probe.ipynb) bunu 15 dakikada ölçtü ve
**hipotezi çürüttü**. Aynı oturumda, aynı 8 satır, aynı kütüphane sürümleri:

| rejim | s/satır | `train_runtime` (8 satır) |
|---|---|---|
| pinlemesiz, iptal edilen koşu gibi | 26,0 | 274,9 s |
| `CUDA_VISIBLE_DEVICES=0` | 21,2 | 281,6 s |

Cihaz sayısı adım maliyetini belirlemiyor: pinleme en iyi halde 1,2x, Trainer'ın
kendi runtime'ına göre hiç. Gerçek maliyet **~1880 token'lık satır başına 28-35
saniye**, ve 910 s/adım oradan geliyor (910 / 32 satır = 28,4). Tam koşu 4800
satır geçişi, yani **38-47 saat**.

Yani sığdırmanın iki yolu var ve ikisi de henüz ölçülmedi: verimi düzeltmek —
`torchrun` ile iki T4 üzerinde DDP (~2x, kartlar zaten elimizde),
`gradient_checkpointing` kapalı, `paged_adamw_8bit` yerine paging yapmayan bir
optimizer — ya da koşuyu oturumlara bölüp `resume_from_checkpoint` ile devam
etmek.

Sığdırmanın **yolu olmayan** hali `--epochs` ya da `--max-seq-len` kırpmak:
kırpma soldan olduğu için vakanın başını atar ve modele görmediği kanıta atıf
yapmayı öğretir, üstelik normal görünen bir loss'la. Yukarıdaki
"Skor ölçeğinde 3 neden var" bölümü aynı türden bir hatanın ölçümle nasıl
görünmez olduğunu anlatıyor.

## Contrast set — held-out'un cevaplayamadığı soru

Held-out set "bu vakayı daha önce gördü mü" sorusunu cevaplar. **"Kuralı mı
öğrendi yoksa bankayı mı"** sorusunu cevaplamaz, ve üretilmiş bir sette bu ikisi
kolayca ayrışır: her vaka 51 fragmentten kuruluyor, yani model okumadan
**tanıyarak** yüksek `absent_rate` alabilir.

[Gardner ve ark. (2020)](https://arxiv.org/abs/2004.02709) çareyi tarif ediyor:
değerlendirilen örneği **etiketi değiştirecek kadar** küçük bir müdahaleyle boz,
tahminin de değiştiğini kontrol et. On NLP veri setinde, orijinal testte güçlü
görünen modeller bozulmuş sette 25 puana varan kayıp vermiş — o fark hiçbir
zaman yetenek değilmiş.

[`build_contrast_set.py`](../build_contrast_set.py) iki tür çift üretiyor.
İkisinde de şirket, bölüm sırası ve diğer tüm fragmentler **aynı**, yani cevabın
değişmesinin tek bir olası sebebi var:

| tür | müdahale | beklenen |
|---|---|---|
| `quality` | Bir kriterin fragmenti en az 2 bant farklı biriyle değişir | O bulgunun puanı doğru yöne hareket etmeli |
| `removal` | Bir kriterin bölümü tamamen silinir | O bulgu `evidence_found=false` olmalı |

Üç sayı:

- **`direction`** — bozulan kriter doğru yöne hareket etti mi.
- **`stability`** — bozulmayan kriterler yerinde kaldı mı. Tek paragraf değişince
  tüm raporu yeniden yazan model kriter bazında karar vermiyor demektir, ve
  `absent_rate` bunu göremez çünkü hiç iki cevabı birbiriyle karşılaştırmaz.
- **`consistency`** — ikisini birden tutturan çiftler. Held-out puanının,
  fragment tanıyarak geçilemeseydi ne olacağı.

`absent_rate` yükselip `consistency` yükselmiyorsa, kazanç muhtemelen ezber.
Script bunu basıyor ve o build yayına alınmıyor.

## Held-out sayısı ne ölçüyordu — ölçüldü, ve cevap "hatırlamayı"

`rubric-curve-eval` 3 Ağustos 2026'da tamamlandı. Kurtarılan `checkpoint-175`
(1400 satır geçişi, tam koşunun %29'u), 60 held-out satırda:

| ölçüm | taban | adapter |
|---|---:|---:|
| `schema_valid` | 0,867 | **1,000** |
| `absent_rate` | 0,864 | **1,000** |
| `present_score_mae` | 0,944 | **0,003** |
| `hallucinated_quotes` | 0,042 | **0,000** |

0,003 MAE, 357 bulgunun 356'sının tam isabet etmesi demek. Eğitim loss'u 2,543 →
**0,024**. Bunlar bir öğrenme eğrisinin değil, bir ezberin sayıları — ve neden
öyle oldukları ölçülebilir bir şey:

```
distinct evidence quotes  train: 96   eval: 96
eval quotes also in train : 96 (100.0%)
eval pairs (kriter, puan) unseen in train: 0
```

Held-out setin içinde modelin görmediği **tek bir kanıt cümlesi yok**. `split_of`
vakaları imzadan ayırıyor ve o kısım çalışıyor — kombinasyonlar gerçekten ayrık.
Ama banka 51 fragmentten ibaret ve her fragment sabit bir puan taşıyor, yani
1400 satır boyunca her metin etiketiyle yüzlerce kez görülüyor. `present_score_mae`
burada "kanıt kalitesini değerlendirmeyi öğrendi mi"yi değil, **"51 metinden
hangisinin kaç puan olduğunu hatırlıyor mu"yu** ölçüyor.

Bu, üstteki "Ayrık bölme" bölümünün düzelttiği hatanın **ikinci katmanı**. Orada
bölme aynı RNG akışından çekildiği için pazarlama eval'inin %81'i train'de
çıkmıştı; imza üzerinden bölünce %0'a indi. İmza vakayı tanımlıyor, metni değil,
ve düzeltilen tam olarak o kadarıydı.

### Banka dışı vakalar — bunu ayırabilen ölçüm

[`../offbank_cases.py`](../offbank_cases.py) elle yazılmış 10 yatırım vakası
taşıyor: sulama sensöründen marina yazılımına, bankanın hiç girmediği
sektörlerde, ve bankadan **tek bir cümle kullanmadan**.
[`../build_offbank_eval.py`](../build_offbank_eval.py) bunları `rubric_eval.py`'nin
okuduğu formata çeviriyor ve iki şeyi kanıtlıyor, iddia etmiyor: her alıntı vaka
metninde birebir geçiyor, ve hiçbir alıntı `rubric_train.jsonl`'de geçmiyor.
Üretilen set: **10 vaka, 90 bulgu, 146 farklı alıntı, bankayla 0 ortak**, puan
dağılımı 1:11 2:16 3:16 4:18 5:22.

```bash
PORT=8090 go run ./cmd/server &          # mf-backend/ icinde
export BASE_URL=http://localhost:8090 TOKEN=<bir token>
python3 build_offbank_eval.py --out data/offbank_investment.jsonl \
    --train data/rubric_train.jsonl
```

Okuma kuralı: bir adapter bankada 0,003, banka dışında 0,8 veriyorsa öğrendiği
şey rubrik değil banka. İkisi birbirine yakınsa held-out sayısı gerçekten
yeteneği ölçüyordu. Bu ölçüm alınmadan `rubric-v1` yayına alınmamalı, ve
`checkpoint-175`'in yukarıdaki tablosu tek başına bir yayın gerekçesi değil.

İki sınır, sayıya güvenmeden önce: puanlar hâlâ **yazanın değerlendirme
görüşü** — banka için geçerli olan itiraz metin banka dışına çıkınca da geçerli.
Ve 90 bulgunun yalnızca 7'si absent (%8, bankada %27), yani bu sette
`absent_rate` yedi bulguya dayanıyor ve oran değil anekdot olarak okunmalı.

## `rubric_eval.py` neden `compare.py`'nin yerine geçmiyor

`compare.py` daha iyi ölçüm ve **yayına alma kararını o verir**: vakaları
ürünün kendi `/analysis/trial` rotasından geçirir, yani rubrik, prompt, tırnak
nötrleştirme, parser ve puanlama kanıtlanabilir biçimde üretimin kullandıkları
olur. Kaggle'da koşamaz — orada ne backend var ne çıkarım sunucusu.

`rubric_eval.py` **eğitim anının aleti**: ağırlıkları doğrudan yükler, held-out
seti koşar, üreteçin yazdığı yer gerçeğine karşı puanlar. "Bu adapter davranışı
öğrendi mi" sorusunu onu eğiten oturumda cevaplar. "Ürünün raporları iyileşti
mi" sorusunu **cevaplamaz** ve buradaki hiçbir sayı öyleymiş gibi aktarılmamalı.

## Veri

`data/` gitignore'da; üreteç sabit tohumlu, dosya değil komut taşınır.

```bash
# mf-backend/ içinde
PORT=8090 go run ./cmd/server &

# mf-inference/peft/ içinde
export BASE_URL=http://localhost:8090 TOKEN=<herhangi bir hesabın token'ı>
python3 build_dataset.py --domain startup-investability --out-dir data/investment
python3 build_dataset.py --domain digital-marketing     --out-dir data/marketing
python3 merge_rubric_sets.py --input data/investment --input data/marketing
```

Beklenen: `1600 rows — 12000 findings, 3247 absent (27%)`.

Prompt'lar backend'den çekiliyor, elle kopyalanmıyor. Bir adapter tek bir
talimatı sağlamayı öğrenir; yerel bir kopya, iki taraftan biri düzenlendiği anda
kayar ve ortaya hiçbir şeyin göndermediği bir prompt için ayarlanmış bir adapter
çıkar — eğitim normal tamamlandığı ve loss makul göründüğü için görünmeyen bir
hata.

### Fragment bankası

Vaka metinleri `build_dataset.py` içindeki `DOMAIN_BANKS`'ten geliyor ve `score`
**etiket** — metin onu hak etmek zorunda.

| | yatırım | pazarlama |
|---|---|---|
| kriter | 9 | 6 |
| kriter başına fragment | 3 | 4 |
| kombinatorik uzay | 259.524 | 15.360 |

Metinler **sektörden bağımsız**. İlk sürümde her yatırım vakası bir filo takip
şirketiydi (OBD-II, araç, filo) ve her pazarlama vakası bir online market: 1600
satır tek dikeyden gelince model kanıt kalitesi yerine o sektörün kelimelerini
öğrenebilir — "filo rakamı görürsen 4 ver". Artık bir 4'ü 2'den ayıran tek şey
iddianın ölçülmüş ve kaynağa bağlanmış olup olmadığı.

Gerekçe (`rationale`) fragmentin kendisinde duruyor, ortak bir bankada değil.
Banka hali 12.000 bulguya 11 cümle üretiyordu; şimdi 55, ve her biri puanı
metindeki hangi şeyin kazandırdığını söylüyor. Bir bulguyu tartışılabilir kılan
şey bu.

Bankayı okunur biçimde görmek için: `python3 review_fragments.py --out review.md`

Üç değişmez, üretilen veride doğrulanıyor:

- Her `evidence` alıntısı vaka metninde **birebir** geçer (atıflar gerçek olsun diye).
- `evidence_found=false` olan bulgunun `score`'u `null`'dur (öğretilen davranış bu).
- Hiçbir yerde çift tırnak yok — çıkarım yolu onları nötrleştiriyor
  ([`schema.go`](../../../mf-backend/internal/analysis/schema.go) `neutraliseQuotes`),
  yani çift tırnaklı metinle eğitmek modelin hiç karşılaşmayacağı bir dağılımı öğretir.

### Ayrık bölme — neden imza üzerinden

Bir vaka, hangi kriterleri işlediği ve her biri için hangi fragmenti çektiğiyle
tanımlanır; şirket adı ve bölüm sırası ayrıca çekildiği için aynı **içerik**
farklı metin olarak görünebilir. `split_of` bölmeyi bu imzadan hash'liyor, yani
bir vaka ya train'e ya eval'e ait — ikisine birden asla.

Önceki hali iki bölmeyi tek RNG akışından çekiyordu, ki akışın sıralı olması
içeriğinin ayrık olduğunu söylemez. Ölçüldüğünde **pazarlama eval'inin %81'i
train'de çıktı**: adapter'ın yayına alınıp alınmayacağına karar veren sayı,
büyük ölçüde daha önce gösterilmiş vakaların hatırlanmasını ölçüyordu. Yatırım
%4'te kalmıştı, tasarımdan değil aritmetikten — 9 kriter 6 kriterden çok daha
geniş bir uzay demek. Rubrik yeterince büyük olduğunda çalışan bir bölme,
bölme değildir.

Şimdi ikisi de **%0**. Üreteç bunu her koşuda kendi doğruluyor (`assert`), ve
satır sayısına karşı farklı vaka sayısı düşerse uyarı basıyor.

### Skor ölçeğinde 3 neden var

Fragmentler eskiden yalnız 1, 2, 4 ve 5 taşıyordu. Tabanın belgelenmiş hatası
ise tam olarak **"kanıt yokken yine de 3/5 verir"**. Böyle bir sette eğitilen
model `absent_rate`'i yükseltebilir — ama "kanıt yoksa absent de" öğrendiği için
değil, **"asla 3 deme"** öğrendiği için. İkisi farklı davranış ve ölçüm onları
ayıramazdı. Her kriterde gerçekten belirsiz bir orta metin var artık.

Yeni bir dikey eklemek yeni bir adapter değil: `analysis_domains`'e bir rubrik
satırı, `DOMAIN_BANKS`'e bir fragment bankası. Rubrik ile banka birbirinden
koparsa üreteç açılışta durur, döngünün ortasında `KeyError` ile değil.

## Koşu

```bash
source ../.env && ./push.sh              # veri + script'ler
# sürüm işlenmesi bitsin, sonra:
kaggle kernels push -p probe             # ~15 dk, adım maliyetini ölçer
# projeksiyon 12 saatin altına inmiyorsa train'i push ETME:
kaggle kernels push -p train             # ölçülen haliyle 38-47 saat
# bitsin ve Save Version alınsın, sonra:
kaggle kernels push -p eval              # ~3 saat
```

`probe` sıraya sonradan girdi çünkü onun olmadığı hal 12 saat ve haftalık GPU
kotasının üçte birine mal oldu. Bir üstteki "Adım maliyeti" bölümü ne ölçtüğünü
anlatıyor.

### Neden iki notebook

Bir Kaggle oturumu **12 saatle** sınırlı. Tek notebook'ta eğitim (~5 saat) artı
iki taraflı ölçüm (~3 saat) sekiz saate dayanıyordu — payı dar, ve ölçüm
uzarsa kaybedilen şey beş saatlik eğitim oluyordu. Flutter hattı aynı bölmeye
aynı sebeple ulaşmıştı.

Bölmenin ikinci faydası ölçümün kendisinde: taban ve adapter artık **aynı
oturumda**, aynı kütüphane sürümleriyle koşuyor. Eğitim notebook'undaki 20
satırlık taban kontrolü sayı üretmek için değil, temel model işi zaten
yapıyorsa eğitime hiç başlamamak için — Flutter v8'de bu kontrol atlanmıştı ve
tavana çarpıldığı eğitim bittikten sonra anlaşıldı.

### Mount ayrımı

Ölçüm notebook'u iki girdi bağlıyor: veri seti ve eğitim koşusunun çıktısı.
`kernel_sources` ile bağlanan çıktı o koşunun **`/kaggle/working`'inin
tamamıdır** — veri ve script kopyalarını da taşır. Bu yüzden girdi dosya adıyla
aranmıyor, **slug'la** ayırt ediliyor (`find_mount`); dosya adıyla aramak iki
mount arasında kura çekmek olurdu.

Sonra, GPU kutusunda: `merge_adapter.py --adapter <indirilen> --base-model
Qwen/Qwen3-4B-Instruct-2507`, ardından `build_mlc.sh --name rubric-v1`.

## Bilinen sınırlar

- **Fragmentlerin puanlarını bir insan onaylamadı.** "Bu metin 4 alır" kararı
  değerlendirme görüşüdür ve şu an yazanın görüşü — alan sahibinin değil.
  `review_fragments.py` bunu gözden geçirilebilir hale getiriyor ama gözden
  geçirmenin kendisi ayrı bir iş, ve o yapılana kadar adapter kimin ölçütlerini
  öğrendiği belirsiz.
- `rubric_eval.py`'nin `absent_rate`'i **yer gerçeğine** karşı ölçülüyor, canlı
  bir vakaya karşı değil. Üreteç kanıtı kasten sakladığı için bu ölçüm dürüst,
  ama gerçek bir sunumun sessizliği daha bulanıktır.
- Çeşitlilik metinde değil **kombinasyonda**: 1600 satır, kriter başına 3-4
  metinden üretiliyor. Uzay satır sayısının çok üstünde ve bölme ayrık — ama
  aşağıdaki bölümün ölçtüğü gibi, ayrık olan yalnızca kombinasyon.
- Bulgular 7200/4800 dengesiz (satırlar 800/800 ama yatırım cevapları 9, pazarlama
  6 bulgu taşıyor), yani gradyanın ~%60'ı yatırıma gidiyor.
- İki rubrik tek adapter'da; birinin diğerinin kriter adlarını sızdırıp
  sızdırmadığı ölçülmüyor. `rubric_eval.py` bulguları anahtara göre eşleştiriyor,
  yani yanlış rubriğin anahtarı gelirse sessizce atlanır.
