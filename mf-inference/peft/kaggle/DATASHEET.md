# Datasheet — `rubric-dataset`

[Gebru ve ark., *Datasheets for Datasets*](https://arxiv.org/abs/1803.09010)
(CACM 2021) biçiminde. Orijinali 7 kategoride 57 soru; burada bu veri setine
uygulanan sorular yanıtlanıyor.

Neden tutulduğu, kendi tarihimizden: bu veri setinin ilk sürümünde üç kusur
vardı ve **üçünü de bu listedeki sorular yakalardı** — sorulmadıkları için
sonradan, ölçerek bulundular. Aşağıda hangi sorunun neyi yakalayacağı yerinde
işaretli.

Kaynak: [`build_dataset.py`](../build_dataset.py) ·
[`merge_rubric_sets.py`](../merge_rubric_sets.py) ·
[`build_contrast_set.py`](../build_contrast_set.py)

---

## 1. Motivasyon

**Veri seti hangi amaçla oluşturuldu? Hangi boşluğu dolduruyor?**

Ürünün rubrik motorunu çalıştıran modelde ölçülmüş eksikler
(`Qwen/Qwen3-4B-Instruct-2507`, 20 satır, 2026-07-29):

| ölçüm | temel model | anlamı |
|---|---|---|
| `present_score_mae` | 0,77 | Kanıtı görüyor, sonra 1-5 ölçeğinde neredeyse bir bant kaçırıyor |
| `hallucinated_quotes` | %1,3 | 151 alıntının 2'si vakada birebir geçmiyor |
| `absent_rate` | %89 | Kanıt yoksa **zaten** söylüyor — öğretilecek değil, korunacak |
| `schema_valid` | %95 | Biçim disiplini **zaten** var — yine korunacak |

Veri setinin ilk sürümü ilk iki satırı değil, son iki satırı öğretmek için
tasarlandı: o zamanki gerekçe `absent_rate 0 / schema_valid 0` idi, ve o ölçüm
başka bir modele — `gemma-2-2b-it` — aitti. Base değişti, sayılar yeniden
ölçülmedi. Set olduğu gibi hâlâ geçerli (kanıt kasten saklanıyor, `score`
etiket, alıntılar birebir), ama **hangi davranışın kazanç olduğu** değişti:
öğretilen şey artık kanıt yokluğunu ilan etmek değil, kanıt varken doğru bandı
vermek. Bkz. [`README.md`](README.md).

**Kim oluşturdu, kimin adına?**

MasterFabric capstone projesi kapsamında, tek geliştirici.

**Bu veri seti bir modeli neye ikna etmeye çalışmıyor?**

Puanlamayı. Genel puan Go tarafında aritmetikle hesaplanıyor
([`scoring.go`](../../../mf-backend/internal/analysis/scoring.go)); model
yalnızca kanıt bulup kriter başına derece veriyor. Veri seti modele "iyi
yatırım nedir" öğretmiyor, "kanıtı olan ve olmayanı ayır" öğretiyor.

---

## 2. Kompozisyon

**Örnekler neyi temsil ediyor?** ⚠️ *Bu soru sektör sızıntısını yakalardı.*

Her örnek bir **vaka metni** (yatırım sunumu ya da pazarlama brief'i) ve ona
karşılık gelen rubrik doldurması. Metinler **sektörden bağımsız**: ilk sürümde
her yatırım vakası bir filo takip şirketiydi (OBD-II, araç, filo) ve her
pazarlama vakası bir online marketti. 1600 satır tek dikeyden gelince model
kanıt kalitesi yerine o sektörün kelimelerini öğrenebilir. Artık bir 4'ü 2'den
ayıran tek şey iddianın ölçülmüş ve kaynağa bağlanmış olması.

**Kaç örnek var?**

| dosya | satır | içerik |
|---|---|---|
| `rubric_train.jsonl` | 1600 | 800 yatırım + 800 pazarlama, karıştırılmış |
| `rubric_eval.jsonl` | 200 | 100 + 100, held-out |
| `contrast_investment.jsonl` | 60 çift | 30 kalite + 30 silme |
| `contrast_marketing.jsonl` | 60 çift | 30 kalite + 30 silme |

**Her örnek hangi ham veriden oluşuyor?**

`DOMAIN_BANKS` içindeki fragment bankasından. Kriter başına 3 (yatırım) ya da 4
(pazarlama) metin parçası, her biri sabit bir puan etiketiyle. Vaka, bu
parçaların rastgele bir altkümesinin karıştırılmasıyla kuruluyor.

| | yatırım | pazarlama |
|---|---|---|
| kriter | 9 | 6 |
| kriter başına fragment | 3 | 4 |
| kombinatorik uzay | 259.524 | 15.360 |

**Etiketler var mı, neyi temsil ediyor?**

Evet, ve **etiket girdidir**: üreteç önce "bu kriter 4 alacak" diye karar verip
sonra 4 almayı hak eden metni koyuyor. Yer gerçeği çıkarım değil.

**Örnekler arasında ilişki var mı?**

Contrast set'te evet ve kasten: her çift, **tek kriterde** farklı iki vaka.
Diğer her şey aynı — şirket, bölüm sırası, tüm diğer fragmentler.

**Veri gürültülü ya da fazlalıklı mı?** ⚠️ *Bu soru nicelik/bilgi farkını yakalardı.*

Fazlalık var. 1600 satır 51 fragmentten üretiliyor, yani fragment başına ~31
üretim. [Sentetik veri çeşitliliği literatürü](https://arxiv.org/pdf/2410.15226)
performansı belirleyenin *konu sayısı* olduğunu, konu başına üretimi artırmanın
bir yerden sonra sadece fazlalık eklediğini söylüyor. **1600 satır, 1600 örnek
kadar bilgi taşımıyor.** Kazanç satır eklemekten değil fragment eklemekten
gelir.

**Önerilen train/test ayrımı var mı?** ⚠️ *Bu soru %81 sızıntıyı yakalardı.*

Var ve **ayrık olduğu kanıtlanıyor**. Her vaka, içeriğinden (hangi kriterler +
hangi fragment) hash'lenip ya train'e ya eval'e atanıyor (`split_of`), yani bir
vaka ikisine birden ait olamaz.

İlk sürümde iki bölme tek RNG akışından çekiliyordu; ölçüldüğünde **pazarlama
eval'inin %81'i train'de** çıktı. Yatırım %4'te kalmıştı — tasarımdan değil
aritmetikten, çünkü 9 kriterin uzayı 6 kriterinkinden çok daha geniş. Rubrik
yeterince büyük olduğunda çalışan bir bölme, bölme değildir. Şimdi ikisi de %0
ve üreteç her koşuda `assert` ile doğruluyor.

**Veri seti kendi kendine yeterli mi?**

Hayır. Sistem prompt'u ve kriterler çalışan backend'den
(`/analysis/domains/{domain}/prompt`) çekiliyor. Bilinçli: adapter tek bir
talimatı sağlamayı öğrenir, yerel bir kopya iki taraftan biri düzenlendiğinde
kayar ve hiçbir şeyin göndermediği bir prompt için ayarlanmış adapter üretir —
eğitim normal tamamlandığı için görünmeyen bir hata.

**Gizli veya hassas içerik var mı?**

Hayır. Metinlerin tamamı uydurma; gerçek bir şirket, kişi ya da sunum yok.
Şirket adları sektör-nötr uydurma markalar.

---

## 3. Toplama Süreci

**Veri nasıl elde edildi?**

Toplanmadı, **üretildi**. Gerçek vakayı toplayıp etiketlemek haftalar sürer ve
tutarsız çıkar; burada etiket önce seçilip metin ona göre kuruluyor.

**Örnekleme stratejisi neydi?**

Kriter altkümesi ve fragment seçimi tohumlu rastgele (`--seed 20260724`).
Kaç kriterin işleneceğinin tabanı rubrik başına ayarlı: yatırımda 4/9,
pazarlamada 3/6. Pazarlamada 4 kullanılırken absent oranı %16'ya düşüyordu —
yatırımda %28 — yani adapter'ın var olma sebebi olan dal yarı yarıya az
eğitiliyordu.

**Kim topladı ve nasıl ücretlendirildi?**

Fragment metinlerini ve puan etiketlerini **Claude yazdı**, geliştiricinin
gözden geçirmesi için. ⚠️ **Bu, veri setinin en zayıf halkası:** "bu metin 4
alır" bir değerlendirme görüşüdür ve şu an yazanın görüşü — alan sahibinin
değil. [`review_fragments.py`](../review_fragments.py) bankayı gözden
geçirilebilir hale getiriyor; gözden geçirme henüz yapılmadı.

**Etik inceleme yapıldı mı?**

Gerekmedi — insan verisi yok.

---

## 4. Ön İşleme / Temizleme / Etiketleme

**Ne yapıldı?**

- **Çift tırnak nötrleştirme.** Çıkarım yolu vakayı modele göstermeden önce
  tırnakları tekile çeviriyor
  ([`schema.go`](../../../mf-backend/internal/analysis/schema.go)
  `neutraliseQuotes`), o yüzden fragmentlerde hiç çift tırnak yok — olsaydı
  modelin hiç karşılaşmayacağı bir dağılım öğretilirdi.
- **Hedef çıktı çitsiz ham JSON**, boşluksuz ayraçlarla. O çitin *yokluğu*
  öğretilenin yarısı.
- **Karıştırma.** İki rubriğin setleri sabit tohumla karıştırılıyor; ard arda
  eklense eval kaybı grafiğinde rubrik değişimi model kararsızlığı gibi
  okunurdu.

**Ham veri saklandı mı?**

Saklanmıyor ve gerekmiyor: üreteç + sabit tohum tekrar edilebilir eser, kopya
dosya değil. `data/` gitignore'da.

---

## 5. Kullanım

**Bu veri seti hangi görev için kullanıldı?**

Qwen3-4B üzerinde QLoRA ile `rubric-v1` adapter'ı.

**Ölçüm nasıl yapılıyor?**

| ne | nerede | ne söyler |
|---|---|---|
| `absent_rate`, `schema_valid` | [`rubric_eval.py`](../rubric_eval.py) | Eğitim anında, held-out sette |
| `direction`, `stability`, `consistency` | aynı, `--contrast` ile | Kuralı mı öğrendi, bankayı mı |
| `absent_rate`, `stddev`, `completed` | [`compare.py`](../compare.py) | **Yayına alma kararı** — ürünün kendi rotasından |

`rubric_eval.py` "bu adapter davranışı öğrendi mi"yi cevaplar; "ürünün raporları
iyileşti mi"yi **cevaplamaz** ve buradaki hiçbir sayı öyleymiş gibi
aktarılmamalı.

**Bu veri seti nerede yanıltır?** ⚠️ *Sorulması en önemli soru.*

- **Fragment tanımayla iyi sonuç alınabilir.** Her vaka aynı 51 parçadan
  kuruluyor, yani model okumadan tanıyarak yüksek `absent_rate` alabilir.
  Contrast set tam olarak bunu ölçmek için var
  ([Gardner ve ark. 2020](https://arxiv.org/abs/2004.02709)): tek paragrafı
  değişen aynı vaka, ve tanıma bunu atlatamaz.
- **Yer gerçeği sessizliği temiz.** Üreteç kanıtı kasten saklıyor, gerçek bir
  sunumun sessizliği daha bulanık.
- **Puanlar onaylanmadı** (bkz. Toplama Süreci).
- **Bulgu dengesi eşit değil**: 7200 yatırım / 4800 pazarlama, yani gradyanın
  ~%60'ı yatırıma gidiyor.
- **İki rubrik tek adapter'da** ve birinin diğerinin kriter adlarını sızdırıp
  sızdırmadığı ölçülmüyor.

**Hangi kullanımlardan kaçınılmalı?**

Bu veri setiyle eğitilen adapter'ı **bu iki rubriğin dışında** bir değerlendirme
için kullanmak. Kriterler prompt'tan geliyor gibi görünse de adapter bu dokuz ve
altı kriterin dilini gördü.

---

## 6. Dağıtım

Kaggle'da `emrahik/rubric-dataset`, **private**. Eğitim script'leri veri setinin
içinde taşınıyor ([`push.sh`](push.sh)) — Flutter hattında notebook yalnız
Kaggle'ın sürüm geçmişinde kalmış ve kurtarıldığında eğitim kodu yok olmuştu.

---

## 7. Bakım

**Kim bakıyor?** Depo sahibi.

**Nasıl güncellenir?** Üreteci değiştir, veriyi yeniden üret, `push.sh`.
Bölme ataması **imzanın kendisinden** hash'lendiği için `--n` değişse bile her
vaka bulunduğu tarafta kalır — eğitim seti büyürken dünkü eval vakaları sessizce
içine karışmaz.

**Eski sürümler destekleniyor mu?** Hayır. Yeniden üretilebilirlik dosyada
değil, üreteç + tohum + commit'te.

**Bir sonraki sürümde yapılacaklar**

1. Fragment puanlarını alan sahibi gözden geçirsin (`review_fragments.py`).
2. Satır sayısını düşür, fragment sayısını artır — 1600 satır fazlalık taşıyor.
3. Banka dışından yazılmış birkaç vaka ekle; bankanın ezberlenip
   ezberlenmediğini yakalayacak tek ölçüm o.
