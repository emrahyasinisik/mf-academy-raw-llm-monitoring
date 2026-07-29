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
| [`train/rubric-train.ipynb`](train/rubric-train.ipynb) | Ucuz taban kapısı → eğitim. ~5,5 saat |
| [`eval/rubric-eval.ipynb`](eval/rubric-eval.ipynb) | Taban + adapter, tek oturumda, contrast dahil. ~3 saat |
| `*/kernel-metadata.json` | GPU tipi ve girdiler. `machine_shape` **atlanamaz** — atlanırsa Kaggle P100 verir, sm_60, 4-bit NF4 çalışmaz |
| [`DATASHEET.md`](DATASHEET.md) | Veri setinin künyesi — Gebru'nun 7 kategorisi. Bilinen zayıflıklar dahil |
| [`push.sh`](push.sh) | Veri setini + script'leri yayınlar; notebook ayrı push'lanır |
| [`../build_contrast_set.py`](../build_contrast_set.py) | Tek kriteri bozan eş vakalar — ezber ile kural arasını ayırır |
| [`../train_qlora_qwen.py`](../train_qlora_qwen.py) | Qwen3 QLoRA. `../train_qlora.py`'nin Gemma'ya özgü kararları buraya taşınamaz |
| [`../rubric_eval.py`](../rubric_eval.py) | Kaggle'da koşan ölçüm. `compare.py`'nin kardeşi, yerine geçeni değil |
| [`../measure_tokens.py`](../measure_tokens.py) | `--max-seq-len`'i ölçer; tokenizer'lar arası taşınmaz |
| [`../merge_rubric_sets.py`](../merge_rubric_sets.py) | İki rubriğin setlerini deterministik karıştırır |

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
kaggle kernels push -p train             # ~5,5 saat
# bitsin ve Save Version alınsın, sonra:
kaggle kernels push -p eval              # ~3 saat
```

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
  metinden üretiliyor. Uzay artık satır sayısının çok üstünde ve bölme ayrık,
  ama bir adapter'ın bankanın kendisini ezberleyip ezberlemediğini yakalayacak
  ölçüm hâlâ yok — bunun için banka dışından yazılmış vakalar gerekir.
- Bulgular 7200/4800 dengesiz (satırlar 800/800 ama yatırım cevapları 9, pazarlama
  6 bulgu taşıyor), yani gradyanın ~%60'ı yatırıma gidiyor.
- İki rubrik tek adapter'da; birinin diğerinin kriter adlarını sızdırıp
  sızdırmadığı ölçülmüyor. `rubric_eval.py` bulguları anahtara göre eşleştiriyor,
  yani yanlış rubriğin anahtarı gelirse sessizce atlanır.
