#!/usr/bin/env python3
"""Hand-written investability cases from outside the fragment bank.

Why this file exists
--------------------
`build_dataset.py` assembles cases from a bank of 51 fragments, each carrying a
fixed score. `split_of` makes the *combinations* disjoint between train and
eval, and that part works. What it cannot make disjoint is the text: measured on
the published sets, all 96 distinct evidence quotes in the eval split also
appear in the train split, and every (criterion, score) pair with them.

So the held-out number answers "has it seen this combination", never "has it
learned to read". `rubric-curve`'s checkpoint scores `present_score_mae` 0.003
there — 356 of 357 findings exactly right, at a training loss of 0.024. That is
the signature of a 51-entry lookup table, and no measurement built on the bank
can tell it apart from judgement.

These cases are the measurement that can. Every sentence here was written by
hand, in sectors the bank does not cover, with no phrase reused from it —
`assert_offbank()` proves the second part rather than asserting it in prose. An
adapter that memorised the bank has nothing to look up here; one that learned
what makes a claim credible scores about the same as it does on the bank.

What the scores are, and are not
--------------------------------
The `score` on each finding is the label, and it is one person's evaluation
judgement — the same limitation `DATASHEET.md` records for the bank itself, and
it is not fixed by moving the text off-bank. What *is* fixed is the recall
shortcut. Read a gap between bank and off-bank as evidence about memorisation;
read the absolute level as provisional until a domain owner has been over these
scores.

The scale, so the labels below can be argued with:

  5  the claim is measured, sourced, and the measurement is one an outsider
     could check
  4  measured and specific, but the reader has to take the number on trust
  3  genuinely mixed — a real signal and a real hole, in the same breath
  2  an assertion with a number attached that does not support it
  1  an assertion with nothing attached

`evidence_found=false` means the case does not address the criterion at all.
That is not a low score, and the whole product rests on the difference.

Note the limit that comes with it: 7 of the 90 findings here are absences (8%),
against 27% in the bank's sets. `absent_rate` measured on this file therefore
rests on seven findings and should be read as an anecdote, not a rate — one
disagreement moves it 14 points. The number this file is built to carry is
`present_score_mae`, over 83 findings. If the absent behaviour needs measuring
off-bank too, that wants more cases written to be silent on a criterion rather
than a different reading of these.

Usage:
    python3 build_offbank_eval.py --out data/offbank_investment.jsonl
"""

from __future__ import annotations

# ---------------------------------------------------------------------------
# Each case: a title, the deck text, and one finding per criterion the deck
# addresses. Criteria not listed are absent by omission — spelled out in the
# emitted rows rather than left implicit, so the file cannot silently teach
# "unlisted means unscored" to a reader.
#
# Sectors are deliberately scattered. The bank's first version made every
# investment case a fleet-tracking company, and 1600 rows from one vertical
# teach the vertical's vocabulary rather than evidence quality. Repeating that
# mistake in the instrument built to detect it would be its own kind of funny.
# ---------------------------------------------------------------------------

CASES: list[dict] = [
    {
        "title": "Sulama sensörü — tohum turu sunumu",
        "text": """Toprakita

PROBLEM
Kayısı üreticisi sulamayı takvimle yapıyor, toprağın o anki nemine göre değil.
Malatya'da 40 bahçede ölçtük: aynı hafta içinde komşu iki parselin nem farkı
yüzde 18'e çıkabiliyor, ve takvim ikisine de aynı suyu veriyor. Sonuç bir tarafta
kök çürümesi, diğer tarafta verim kaybı.

ÇÖZÜM
Parsele gömülen tek bir sonda ve GSM ile günde iki ölçüm. Rakiplerin çoğu
meteoroloji verisinden tahmin yürütüyor; biz toprağın kendisini okuyoruz. Sondanın
kalibrasyonu killi toprakta hâlâ elle yapılıyor, bunu otomatikleştiremedik.

PAZAR
Türkiye'de 640 bin dekar kayısı alanı var, TÜİK 2025 verisi. Bunun yüzde 12'si
100 dekar üstü işletmelerde ve ilk hedefimiz orası — 76 bin dekar, dekar başına
yıllık 180 lira abonelikle 13,7 milyon lira. Meyve dışına çıkma planımız var ama
onun rakamını çıkarmadık.

ÇEKİŞ
Mart 2026'da 12 bahçede ücretsiz pilot başladı. Ağustos itibarıyla 9'u ödemeye
geçti, 3'ü bıraktı; bırakanların ikisi sondanın kurulumunu kendileri yapamadığını
söyledi. Ödeyen 9 bahçede toplam yıllık sözleşme 1,4 milyon lira.

EKİP
Kurucu 11 yıl ziraat mühendisi olarak sahada çalıştı, ikinci kurucu gömülü
sistemler tarafından geliyor ve daha önce bir sayaç şirketinde 200 bin cihaz
sevk etmiş. İkisi de tam zamanlı. Üçüncü ortak yarı zamanlı ve satıştan sorumlu.

İŞ MODELİ
Dekar başına yıllık abonelik, donanım bedelsiz ve mülkiyeti bizde kalıyor.
Sondanın maliyeti 890 lira, iki sezonda kendini amorti ediyor; bu hesabı 9 sahadaki
gerçek maliyetle yaptık, tedarikçi listesiyle değil.

RİSK
Donanım tarlada duruyor ve iki sezondur dolu vurmadı — vurursa yenileme maliyeti
bizde. Karşılığını ayırmadık, çünkü sıklığı hakkında elimizde veri yok. İkinci
risk tek tedarikçiye bağlı olmamız.
""",
        "findings": [
            {"key": "problem_clarity", "score": 5,
             "evidence": ["Malatya'da 40 bahçede ölçtük: aynı hafta içinde komşu iki parselin nem farkı",
                          "takvim ikisine de aynı suyu veriyor"],
             "rationale": "Problem bir gözlemle değil ölçümle konmuş, ve ölçüm nerede kaç örnekte yapıldığını söylüyor; okuyan aynı ölçümü tekrarlayabilir."},
            {"key": "market_size", "score": 4,
             "evidence": ["Türkiye'de 640 bin dekar kayısı alanı var, TÜİK 2025 verisi",
                          "76 bin dekar, dekar başına yıllık 180 lira abonelikle 13,7 milyon lira"],
             "rationale": "Kaynaklı bir taban rakamdan aşağı doğru hesaplanmış ve hedef dilim ayrıca daraltılmış; 5 değil çünkü 180 liralık fiyatın nereden geldiği gösterilmiyor."},
            {"key": "solution_differentiation", "score": 4,
             "evidence": ["Rakiplerin çoğu meteoroloji verisinden tahmin yürütüyor; biz toprağın kendisini okuyoruz",
                          "Sondanın kalibrasyonu killi toprakta hâlâ elle yapılıyor"],
             "rationale": "Fark tek cümlede ve teknik olarak somut; üstelik açık kalan tarafı kendisi söylüyor, ki bu farkın abartılmadığının işareti."},
            {"key": "traction", "score": 3,
             "evidence": ["Ağustos itibarıyla 9'u ödemeye geçti, 3'ü bıraktı",
                          "Ödeyen 9 bahçede toplam yıllık sözleşme 1,4 milyon lira"],
             "rationale": "Gerçek dönüşüm ve gerçek gelir var ama taban 12; bu büyüklükte bir örneklem eğilim göstermez, ve bırakanların sebebi ürünün kendisinde."},
            {"key": "business_model", "score": 5,
             "evidence": ["Sondanın maliyeti 890 lira, iki sezonda kendini amorti ediyor",
                          "bu hesabı 9 sahadaki gerçek maliyetle yaptık, tedarikçi listesiyle değil"],
             "rationale": "Birim ekonomi tek cümlede kapanıyor ve rakamın kaynağının gerçekleşen maliyet olduğu ayrıca belirtilmiş."},
            {"key": "team", "score": 4,
             "evidence": ["Kurucu 11 yıl ziraat mühendisi olarak sahada çalıştı",
                          "daha önce bir sayaç şirketinde 200 bin cihaz sevk etmiş"],
             "rationale": "İki kurucu da probleme dokunan deneyime sahip ve biri ölçekli sevkiyatı yapmış; satışın yarı zamanlı olması 5'i engelliyor."},
            {"key": "competition", "score": 2,
             "evidence": ["Rakiplerin çoğu meteoroloji verisinden tahmin yürütüyor"],
             "rationale": "Rakipler tek bir yöntem farkıyla topluca geçiştirilmiş; kim oldukları, kaç oldukları, ne fiyatladıkları yok."},
            {"key": "risk", "score": 4,
             "evidence": ["iki sezondur dolu vurmadı — vurursa yenileme maliyeti bizde",
                          "Karşılığını ayırmadık, çünkü sıklığı hakkında elimizde veri yok"],
             "rationale": "Riski adlandırmakla kalmayıp karşılık ayırmadığını ve neden ayıramadığını söylüyor; farkındalık ölçüsü budur."},
            {"key": "financials_ask", "score": None, "evidence": [],
             "rationale": "Sunum ne kadar istendiğine, hangi kullanım planına ya da mevcut nakit süresine hiç değinmiyor."},
        ],
    },
    {
        "title": "Diş kliniği hatırlatma yazılımı — yatırımcı notu",
        "text": """Randevum

PROBLEM
Diş kliniklerinde gelmeyen hasta oranı yüksek. Bu herkesin bildiği bir sorun.

PAZAR
Dünyada dijital sağlık pazarı 2030'da 500 milyar dolar olacak. Biz de bu pazarın
içindeyiz ve küçük bir payı bile bizim için fazlasıyla yeterli.

ÇÖZÜM
Hastaya randevudan önce mesaj atıyoruz. Mesajın metnini kliniğe göre
değiştirebiliyoruz ve gönderim saatini seçebiliyorlar.

ÇEKİŞ
Şu ana kadar 60'tan fazla klinik sistemi denedi ve geri bildirimler çok olumlu.
Kullanıcılarımız ürünü çok seviyor.

EKİP
Üç kurucu da yazılım geliştirici. Ekipte sağlık sektöründen gelen kimse yok ama
öğreniyoruz.

İŞ MODELİ
Klinik başına aylık sabit ücret alıyoruz. Fiyatı henüz netleştirmedik, pilotlarda
ücretsiz ilerliyoruz.

REKABET
Bizim yaptığımızı yapan başka bir ürün görmedik.
""",
        "findings": [
            {"key": "problem_clarity", "score": 1,
             "evidence": ["Diş kliniklerinde gelmeyen hasta oranı yüksek",
                          "Bu herkesin bildiği bir sorun"],
             "rationale": "Sorun bir oranla değil bir sıfatla tarif edilmiş, ve gerekçesi olarak yaygın kabul gösterilmiş; ölçülmüş hiçbir şey yok."},
            {"key": "market_size", "score": 1,
             "evidence": ["Dünyada dijital sağlık pazarı 2030'da 500 milyar dolar olacak",
                          "küçük bir payı bile bizim için fazlasıyla yeterli"],
             "rationale": "Tepeden inen bir rakam ve payın nasıl alınacağına dair hiçbir yol; metnin kendisi hesabı bir temenniye bağlıyor."},
            {"key": "solution_differentiation", "score": 2,
             "evidence": ["Hastaya randevudan önce mesaj atıyoruz",
                          "gönderim saatini seçebiliyorlar"],
             "rationale": "Ürün anlatılmış ama anlatılan şey herhangi bir mesaj aracının yaptığı; farkı taşıyacak tek bir özellik gösterilmiyor."},
            {"key": "traction", "score": 2,
             "evidence": ["60'tan fazla klinik sistemi denedi ve geri bildirimler çok olumlu",
                          "Kullanıcılarımız ürünü çok seviyor"],
             "rationale": "Sayı var ama ölçtüğü şey deneme; ödeyen, kalan ya da kullanmaya devam eden kaç klinik olduğu yok, memnuniyet de anekdot olarak veriliyor."},
            {"key": "team", "score": 2,
             "evidence": ["Üç kurucu da yazılım geliştirici",
                          "Ekipte sağlık sektöründen gelen kimse yok ama öğreniyoruz"],
             "rationale": "Yürütme tarafı örtülü ama alan bilgisi hem yok hem de yerine bir plan değil bir niyet konmuş."},
            {"key": "business_model", "score": 2,
             "evidence": ["Klinik başına aylık sabit ücret alıyoruz",
                          "Fiyatı henüz netleştirmedik, pilotlarda ücretsiz ilerliyoruz"],
             "rationale": "Modelin şekli belli, ama fiyat yokken model bir gelir iddiası değil bir niyet; maliyet tarafı da hiç girmiyor."},
            {"key": "competition", "score": 1,
             "evidence": ["Bizim yaptığımızı yapan başka bir ürün görmedik"],
             "rationale": "Rekabet yokluğu iddiası aramanın sonucu olarak değil, görmemenin sonucu olarak sunulmuş."},
            {"key": "financials_ask", "score": None, "evidence": [],
             "rationale": "Talep edilen tutar, kullanım planı ve mevcut nakit süresi metinde yok."},
            {"key": "risk", "score": None, "evidence": [],
             "rationale": "Sunum hiçbir risk başlığına girmiyor; risk bölümü yok."},
        ],
    },
    {
        "title": "İkinci el saat doğrulama pazaryeri — A serisi",
        "text": """Kadran

PROBLEM
İkinci el lüks saat alıcısının en büyük engeli sahtecilik. 2025'te İstanbul'da
340 satıcıyla anket yaptık: alıcıların yüzde 61'i son bir yılda en az bir alımdan
sahtelik şüphesiyle vazgeçtiğini söyledi. Vazgeçilen işlemlerin ortalama tutarı
4.200 dolar.

ÇÖZÜM
Saat bize geliyor, üç aşamalı kontrolden geçiyor, sonra alıcıya gidiyor. Kontrolün
üçüncü aşaması hareket mekanizmasının röntgeni ve bu ekipmanı Türkiye'de tutan
başka bir platform yok — cihaz 140 bin euro ve iki yıllık bir tedarik süresi var.
İlk iki aşama rakiplerde de var.

PAZAR
Rakamı üstten değil aşağıdan kurduk. Türkiye'de yıllık ikinci el lüks saat hacmi
konusunda yayınlanmış veri yok, o yüzden üç büyük forumun 2025 ilan kayıtlarını
saydık: 18.400 ilan, medyan 3.100 dolar, yani gördüğümüz hacim 57 milyon dolar.
Bu bir alt sınır, çünkü forum dışı satışları göremiyoruz.

ÇEKİŞ
Ocak 2026'da açıldık. Temmuz sonu itibarıyla 610 tamamlanmış işlem, 1,9 milyon
dolar hacim, aylık büyüme ortalama yüzde 24. Tekrar alım yapan alıcı oranı yüzde
31 ve bu oran her ay artıyor.

İŞ MODELİ
İşlem başına yüzde 7 komisyon, satıcıdan. Doğrulama maliyetimiz saat başına 340
lira; medyan işlemde komisyon 6.900 lira, yani brüt marj yüzde 95. Lojistik ve
sigorta dahil edildiğinde katkı marjı yüzde 71'e iniyor.

EKİP
Kurucu daha önce iki pazaryeri kurdu, ikincisi 2023'te satıldı. Doğrulamadan
sorumlu ortak 14 yıl saat ustası olarak çalıştı ve marka yetkili servisinde
eğitmenlik yaptı.

REKABET
İki uluslararası platform Türkiye'ye kargo kabul ediyor ama doğrulamayı yurt
dışında yapıyor, bu da 3 haftalık bir süre demek. Yerelde üç butik satıcı var,
hiçbiri işlem garantisi vermiyor. Bizim süremiz 4 gün.

FİNANSAL
2,5 milyon dolar istiyoruz. Yüzde 40'ı ikinci röntgen cihazı ve Ankara deposu,
yüzde 35'i alıcı tarafı pazarlama, yüzde 25'i 18 aylık ekip. Mevcut nakit 11 ay
yetiyor, bu tur olmadan da kapanmıyoruz ama büyüme aylık yüzde 8'e iniyor.

RİSK
En büyük risk marka hukuku: platform üzerinden geçen sahte bir saat bizim
sorumluluğumuz. Bugüne kadar 610 işlemde 2 sahte yakalandı ve ikisi de satıcıya
iade edildi, ama bir tanesi geçerse hem para hem itibar gider. Sigorta poliçemiz
işlem başına 25 bin dolara kadar karşılıyor.
""",
        "findings": [
            {"key": "problem_clarity", "score": 5,
             "evidence": ["2025'te İstanbul'da 340 satıcıyla anket yaptık",
                          "alıcıların yüzde 61'i son bir yılda en az bir alımdan sahtelik şüphesiyle vazgeçtiğini söyledi"],
             "rationale": "Problem birincil bir araştırmayla ölçülmüş, örneklem büyüklüğü ve tarih verilmiş, ve kayıp işlem tutarıyla parasallaştırılmış."},
            {"key": "market_size", "score": 5,
             "evidence": ["üç büyük forumun 2025 ilan kayıtlarını saydık: 18.400 ilan, medyan 3.100 dolar",
                          "Bu bir alt sınır, çünkü forum dışı satışları göremiyoruz"],
             "rationale": "Yayınlanmış veri olmadığı söylenip yerine sayılabilir bir vekil kurulmuş, ve tahminin hangi yöne yanlı olduğu ayrıca belirtilmiş."},
            {"key": "solution_differentiation", "score": 5,
             "evidence": ["üçüncü aşaması hareket mekanizmasının röntgeni ve bu ekipmanı Türkiye'de tutan başka bir platform yok",
                          "cihaz 140 bin euro ve iki yıllık bir tedarik süresi var"],
             "rationale": "Fark tek bir yetenekte toplanmış ve o yeteneğin kopyalanmasının maliyeti ile süresi sayıyla verilmiş; ayrıca farkın nerede bitttiği de söylenmiş."},
            {"key": "traction", "score": 5,
             "evidence": ["610 tamamlanmış işlem, 1,9 milyon dolar hacim, aylık büyüme ortalama yüzde 24",
                          "Tekrar alım yapan alıcı oranı yüzde 31"],
             "rationale": "Hacim, büyüme ve tekrar alım birlikte veriliyor; tekrar oranı çekişin talepten mi kampanyadan mı geldiğini ayıran sayıdır."},
            {"key": "business_model", "score": 5,
             "evidence": ["Doğrulama maliyetimiz saat başına 340 lira; medyan işlemde komisyon 6.900 lira",
                          "Lojistik ve sigorta dahil edildiğinde katkı marjı yüzde 71'e iniyor"],
             "rationale": "Brüt marjda durulmamış, gerçek katkı marjına kadar inilmiş; iki rakam arasındaki farkın kaynağı da adlandırılmış."},
            {"key": "team", "score": 5,
             "evidence": ["Kurucu daha önce iki pazaryeri kurdu, ikincisi 2023'te satıldı",
                          "Doğrulamadan sorumlu ortak 14 yıl saat ustası olarak çalıştı"],
             "rationale": "Bir taraf çıkışa kadar götürmüş bir pazaryeri kurucusu, diğer taraf ürünün kritik yeteneğinin kendisi; ikisi de doğrulanabilir."},
            {"key": "competition", "score": 4,
             "evidence": ["İki uluslararası platform Türkiye'ye kargo kabul ediyor ama doğrulamayı yurt dışında yapıyor, bu da 3 haftalık bir süre demek",
                          "Yerelde üç butik satıcı var, hiçbiri işlem garantisi vermiyor"],
             "rationale": "Rakipler sayılmış, ayrıştıkları eksen zaman ve garanti olarak somutlanmış; isimler verilmediği için 5 değil."},
            {"key": "financials_ask", "score": 5,
             "evidence": ["2,5 milyon dolar istiyoruz", "Mevcut nakit 11 ay yetiyor, bu tur olmadan da kapanmıyoruz ama büyüme aylık yüzde 8'e iniyor"],
             "rationale": "Tutar, üç kalemli kullanım dağılımı ve mevcut nakit süresi var; turun alınmaması hâlinde ne olacağı da ayrıca söylenmiş."},
            {"key": "risk", "score": 5,
             "evidence": ["610 işlemde 2 sahte yakalandı ve ikisi de satıcıya iade edildi",
                          "Sigorta poliçemiz işlem başına 25 bin dolara kadar karşılıyor"],
             "rationale": "Risk adlandırılmış, bugüne kadarki gerçekleşme sayısıyla ölçülmüş, ve karşısına konan azaltıcı bir limitle birlikte verilmiş."},
        ],
    },
    {
        "title": "Atık toplama rota optimizasyonu — belediye satışı",
        "text": """Rotayel

PROBLEM
Belediye çöp kamyonları sabit rotalarla dolaşıyor. Bazı konteynerler dolmadan
boşaltılıyor, bazıları taşıyor. Bunun ne kadar yakıt israfına denk geldiğini
ölçmedik ama sahada gözle görülüyor.

ÇÖZÜM
Konteynere doluluk sensörü koyup rotayı her sabah yeniden hesaplıyoruz.

PAZAR
Türkiye'de 1.390 belediye var. Hepsine satabilsek çok büyük bir iş olur.

ÇEKİŞ
İki ilçe belediyesiyle protokol imzaladık. Biri 6 aydır kullanıyor ve yakıt
tüketiminde yüzde 19 düşüş raporladı; ikincisi geçen ay başladı, henüz veri yok.
Üçüncü bir belediyeyle görüşme sürüyor.

EKİP
Kurucu 8 yıl lojistik planlama yazılımı yazdı. Belediyelerle iş yapma tecrübesi
olan bir danışmanla çalışıyoruz, kadroda değil.

İŞ MODELİ
Konteyner başına yıllık lisans, artı kurulum. Belediye ihalesiyle satıldığı için
fiyatı biz belirlemiyoruz, ihale şartnamesi belirliyor.

REKABET
Aynı işi yapan iki yerli firma var. İkisi de sensörü kendi üretmiyor, ithal
ediyor; biz de ithal ediyoruz.

FİNANSAL
1,2 milyon dolar arıyoruz.

RİSK
Belediye alımları seçim döngüsüne bağlı. Mart 2029'a kadar yeni bir yerel seçim
yok, bu bize üç yıllık bir pencere veriyor; pencerenin sonunda karar vericilerin
tamamı değişebilir ve sözleşmelerin yenilenmesi garanti değil.
""",
        "findings": [
            {"key": "problem_clarity", "score": 2,
             "evidence": ["Bunun ne kadar yakıt israfına denk geldiğini ölçmedik ama sahada gözle görülüyor"],
             "rationale": "Mekanizma doğru tarif edilmiş ama büyüklüğü ölçülmediği kendi ağzıyla söyleniyor; gözlem bir kanıt değil."},
            {"key": "market_size", "score": 1,
             "evidence": ["Türkiye'de 1.390 belediye var", "Hepsine satabilsek çok büyük bir iş olur"],
             "rationale": "Sayım doğru ama pazar değil; hedeflenebilir dilim, birim fiyat ve dönüşüm varsayımı yok."},
            {"key": "solution_differentiation", "score": 2,
             "evidence": ["Konteynere doluluk sensörü koyup rotayı her sabah yeniden hesaplıyoruz"],
             "rationale": "Çözüm net ama rakiplerin yapmadığı bir şey iddia edilmiyor; metnin başka yerinde sensörün ithal olduğu da söyleniyor."},
            {"key": "traction", "score": 3,
             "evidence": ["Biri 6 aydır kullanıyor ve yakıt tüketiminde yüzde 19 düşüş raporladı",
                          "ikincisi geçen ay başladı, henüz veri yok"],
             "rationale": "Tek gerçek referans ölçülmüş bir sonuç taşıyor, ki bu kayda değer; ama n=1 ve rakam müşterinin kendi raporu."},
            {"key": "team", "score": 3,
             "evidence": ["Kurucu 8 yıl lojistik planlama yazılımı yazdı",
                          "Belediyelerle iş yapma tecrübesi olan bir danışmanla çalışıyoruz, kadroda değil"],
             "rationale": "Teknik taraf yerinde, satış kanalının gerektirdiği ilişki ise kadro dışında — bu iş modelinde kritik olan taraf o."},
            {"key": "business_model", "score": 2,
             "evidence": ["Belediye ihalesiyle satıldığı için fiyatı biz belirlemiyoruz, ihale şartnamesi belirliyor"],
             "rationale": "Modelin şekli var ama fiyatlama gücünün karşı tarafta olduğu itiraf ediliyor ve marj üzerine hiçbir şey söylenmiyor."},
            {"key": "competition", "score": 3,
             "evidence": ["Aynı işi yapan iki yerli firma var", "İkisi de sensörü kendi üretmiyor, ithal ediyor; biz de ithal ediyoruz"],
             "rationale": "Rakipler dürüstçe sayılmış ve aynı zeminde durulduğu kabul edilmiş; dürüst ama lehte bir fark üretmiyor."},
            {"key": "financials_ask", "score": 1,
             "evidence": ["1,2 milyon dolar arıyoruz"],
             "rationale": "Tutar var, kullanım planı ve nakit süresi yok; tek başına bir rakam finansal plan değil."},
            {"key": "risk", "score": 4,
             "evidence": ["Belediye alımları seçim döngüsüne bağlı",
                          "pencerenin sonunda karar vericilerin tamamı değişebilir ve sözleşmelerin yenilenmesi garanti değil"],
             "rationale": "Kanalın yapısal riski doğru teşhis edilmiş ve takvime bağlanmış; azaltıcı bir adım önerilmediği için 5 değil."},
        ],
    },
    {
        "title": "İhracat evrak otomasyonu — köprü turu",
        "text": """Gümrükçe

PROBLEM
Küçük ihracatçı her sevkiyat için ortalama 14 belge hazırlıyor ve bunların 5'i
elle dolduruluyor. 60 firmanın 2025 kayıtlarını inceledik: sevkiyat başına
ortalama 3,4 saat evrak işi ve gecikmelerin yüzde 22'si evrak hatasından.

ÇÖZÜM
Sipariş verisinden 14 belgeyi de üretiyoruz. Gümrük müşavirinin sisteminden
otomatik onaya gönderiyoruz.

PAZAR
Türkiye'de 2025'te ihracat yapan 96 bin firma var, TİM verisi. Yıllık cirosu 5
milyon doların altında olan 71 bini bizim hedefimiz. Firma başına aylık 2.400
lira ile ulaşılabilir pazar yıllık 2 milyar lira.

ÇEKİŞ
Nisan 2026'da ilk müşteri. Ağustos itibarıyla 34 ödeyen firma, aylık yinelenen
gelir 79 bin lira. Aylık iptal oranı yüzde 4,2 ve bu son üç ayda düşmüyor.

EKİP
Kurucu ortaklardan biri 9 yıl gümrük müşaviri, diğeri backend geliştirici. Ekipte
başka kimse yok.

REKABET
İki büyük ERP sağlayıcısı bu modülü satıyor ama sadece kendi ERP'sini kullanan
firmaya. Hedef segmentimizdeki firmaların çoğu ERP kullanmıyor, Excel kullanıyor.
Doğrudan rakibimiz aslında Excel.

FİNANSAL
600 bin dolar istiyoruz, 18 aylık runway için. Bugünkü nakit 5 ay yetiyor.

RİSK
Gümrük mevzuatı yılda birkaç kez değişiyor ve belge şablonları ona bağlı. Her
değişiklikte 2-3 gün geliştirme gidiyor; bunu bir maliyet kalemi olarak
bütçeledik.
""",
        "findings": [
            {"key": "problem_clarity", "score": 5,
             "evidence": ["60 firmanın 2025 kayıtlarını inceledik: sevkiyat başına ortalama 3,4 saat evrak işi",
                          "gecikmelerin yüzde 22'si evrak hatasından"],
             "rationale": "Problem ikinci elden değil kayıt incelemesinden çıkarılmış, ve iki ayrı boyutta (zaman ve hata) sayısallaştırılmış."},
            {"key": "market_size", "score": 4,
             "evidence": ["Türkiye'de 2025'te ihracat yapan 96 bin firma var, TİM verisi",
                          "Firma başına aylık 2.400 lira ile ulaşılabilir pazar yıllık 2 milyar lira"],
             "rationale": "Kaynaklı taban, daraltılmış hedef ve fiyatla çarpım var; fiyatın gerçekleşen ortalamayla aynı olup olmadığı gösterilmediği için 5 değil."},
            {"key": "solution_differentiation", "score": 3,
             "evidence": ["Sipariş verisinden 14 belgeyi de üretiyoruz",
                          "Gümrük müşavirinin sisteminden otomatik onaya gönderiyoruz"],
             "rationale": "Kapsam iddiası somut ve müşavir entegrasyonu gerçek bir engel; ama bunun kopyalanmasını zorlaştıran bir şey söylenmiyor."},
            {"key": "traction", "score": 3,
             "evidence": ["34 ödeyen firma, aylık yinelenen gelir 79 bin lira",
                          "Aylık iptal oranı yüzde 4,2 ve bu son üç ayda düşmüyor"],
             "rationale": "Ödeyen müşteri ve gelir gerçek, ama iptal oranı yüksek ve düzelmediği kendi ağzıyla söyleniyor — büyüme bu oranla sınırlı."},
            {"key": "team", "score": 4,
             "evidence": ["Kurucu ortaklardan biri 9 yıl gümrük müşaviri, diğeri backend geliştirici"],
             "rationale": "Alan bilgisi ve yürütme tam olarak bu ürünün ihtiyacı olan iki taraf; ekibin iki kişiden ibaret olması 5'i engelliyor."},
            {"key": "competition", "score": 5,
             "evidence": ["İki büyük ERP sağlayıcısı bu modülü satıyor ama sadece kendi ERP'sini kullanan firmaya",
                          "Doğrudan rakibimiz aslında Excel"],
             "rationale": "Hem yerleşik oyuncular hem de asıl rakip olan mevcut alışkanlık adlandırılmış; ikincisini görmek rekabet analizinin zor kısmıdır."},
            {"key": "business_model", "score": None, "evidence": [],
             "rationale": "Aylık fiyat pazar hesabında geçiyor ama sunumda iş modeli, maliyet yapısı veya marj üzerine bir bölüm yok."},
            {"key": "financials_ask", "score": 3,
             "evidence": ["600 bin dolar istiyoruz, 18 aylık runway için", "Bugünkü nakit 5 ay yetiyor"],
             "rationale": "Tutar, süre ve mevcut nakit var; paranın hangi kalemlere gideceği yok."},
            {"key": "risk", "score": 4,
             "evidence": ["Gümrük mevzuatı yılda birkaç kez değişiyor ve belge şablonları ona bağlı",
                          "bunu bir maliyet kalemi olarak bütçeledik"],
             "rationale": "Ürünün yapısına gömülü olan bakım riski görülmüş ve karşılık ayrılmış; sıklığın rakamı olmadığı için 5 değil."},
        ],
    },
    {
        "title": "Fizik tedavi hareket takibi — pre-seed",
        "text": """Hareketim

PROBLEM
Fizik tedavi hastası evde verilen egzersizi yapmıyor ya da yanlış yapıyor.

ÇÖZÜM
Telefonun kamerasıyla hareketi izleyip anında geri bildirim veriyoruz. Model
cihazda çalışıyor, video sunucuya gitmiyor.

PAZAR
Ölçmedik.

ÇEKİŞ
3 klinikte 40 hastayla 8 haftalık bir çalışma yaptık. Egzersiz uyumu kontrol
grubunda yüzde 41, bizim grupta yüzde 78. Çalışma bir üniversite hastanesinin
etik kurulundan geçti ve sonuçlar hakemli bir dergiye gönderildi, henüz kabul
edilmedi.

EKİP
Kurucu bilgisayarlı görü alanında doktoralı ve bu konuda 6 yayını var. Klinik
tarafta danışman olarak bir fizyoterapi profesörü var.

REKABET
Yurt dışında benzer üç uygulama var. Üçü de videoyu buluta gönderiyor; KVKK ve
GDPR tarafında bu bir yük, biz cihazda çalıştığımız için o yükü taşımıyoruz.

RİSK
Tıbbi cihaz sınıflandırması alırsak süreç uzar ve maliyet artar. Hukuki görüş
aldık: mevcut haliyle egzersiz takibi olarak sınıf dışı kalıyoruz, ama geri
bildirim metinleri tanı ima ederse sınıf IIa'ya girebiliriz. Metinleri buna göre
yazıyoruz.
""",
        "findings": [
            {"key": "problem_clarity", "score": 2,
             "evidence": ["Fizik tedavi hastası evde verilen egzersizi yapmıyor ya da yanlış yapıyor"],
             "rationale": "Doğru bir sorun ama tek cümlelik bir iddia; ne sıklıkta, kimde ve ne sonuç doğurduğu bu bölümde yok."},
            {"key": "market_size", "score": None, "evidence": [],
             "rationale": "Sunum pazar büyüklüğüne dair bir rakam vermiyor ve ölçmediğini açıkça yazıyor; bilgi yokluğu düşük puan değildir."},
            {"key": "solution_differentiation", "score": 4,
             "evidence": ["Model cihazda çalışıyor, video sunucuya gitmiyor"],
             "rationale": "Tek bir mimari karar hem gizlilik hem maliyet tarafında ayrışma üretiyor ve rekabet bölümünde karşılığı gösteriliyor."},
            {"key": "traction", "score": 4,
             "evidence": ["Egzersiz uyumu kontrol grubunda yüzde 41, bizim grupta yüzde 78",
                          "Çalışma bir üniversite hastanesinin etik kurulundan geçti"],
             "rationale": "Kontrol gruplu bir çalışma anekdottan çok daha güçlü bir kanıt; ticari çekiş (ödeyen müşteri) hâlâ yok, o yüzden 5 değil."},
            {"key": "team", "score": 4,
             "evidence": ["Kurucu bilgisayarlı görü alanında doktoralı ve bu konuda 6 yayını var",
                          "Klinik tarafta danışman olarak bir fizyoterapi profesörü var"],
             "rationale": "Teknik derinlik doğrulanabilir biçimde gösterilmiş; klinik taraf danışman seviyesinde kaldığı ve ticari taraf hiç olmadığı için 5 değil."},
            {"key": "competition", "score": 4,
             "evidence": ["Yurt dışında benzer üç uygulama var", "Üçü de videoyu buluta gönderiyor"],
             "rationale": "Rakipler sayılmış ve ayrışma ekseni tek bir teknik farkta toplanmış; isim ve konum verilmediği için 5 değil."},
            {"key": "risk", "score": 5,
             "evidence": ["Hukuki görüş aldık: mevcut haliyle egzersiz takibi olarak sınıf dışı kalıyoruz",
                          "geri bildirim metinleri tanı ima ederse sınıf IIa'ya girebiliriz"],
             "rationale": "Düzenleyici risk dışarıdan görüşle sınırı çizilerek ele alınmış, ve sınırı aşmamak ürün kararına bağlanmış."},
            {"key": "business_model", "score": None, "evidence": [],
             "rationale": "Sunumda fiyat, gelir modeli veya maliyet yapısı üzerine hiçbir ifade yok."},
            {"key": "financials_ask", "score": None, "evidence": [],
             "rationale": "Talep edilen tutar ve kullanım planı metinde geçmiyor."},
        ],
    },
    {
        "title": "Eczane stok paylaşım ağı — tohum",
        "text": """Reçetem

PROBLEM
Küçük eczane elinde olmayan ilacı hastaya veremiyor, hasta başka eczaneye
gidiyor. Bu hem satış kaybı hem hasta kaybı.

ÇÖZÜM
Eczaneler arası stok görünürlüğü ve aynı gün transfer. Kurye ağını kendimiz
işletmiyoruz, mevcut motokurye firmalarıyla anlaşıyoruz.

PAZAR
Türkiye'de 27 bin eczane var. Yüzde 10'una ulaşırsak 2.700 eczane eder.

ÇEKİŞ
Ankara'da 3 ilçede 210 eczane ağda. Son ay 4.100 transfer gerçekleşti, transfer
başına ortalama 180 lira ilaç bedeli. Ağdaki eczanelerin yüzde 84'ü ayda en az
bir kez kullanıyor.

EKİP
İki kurucu da eczacı. Yazılımı dışarıya yaptırdık, içeride geliştirici yok.

İŞ MODELİ
Transfer başına 12 lira komisyon alıyoruz. Kurye maliyeti transfer başına 9 lira,
yani net 3 lira. Ölçek arttıkça kurye pazarlığında iyileşme bekliyoruz ama bunu
henüz test etmedik.

REKABET
Ecza depolarının kendi uygulamaları var ama sadece depo-eczane arasında çalışıyor,
eczane-eczane değil.

FİNANSAL
Bu turda 400 bin dolar istiyoruz. Yüzde 60'ı üç yeni şehir açılışı, yüzde 40'ı
yazılımı içeri almak için ekip. Nakit 7 ay yetiyor.

RİSK
İlaç transferi mevzuata tabi ve şu an gri alanda ilerliyoruz. Ecza kurumundan
görüş talep ettik, cevap gelmedi.
""",
        "findings": [
            {"key": "problem_clarity", "score": 3,
             "evidence": ["Küçük eczane elinde olmayan ilacı hastaya veremiyor, hasta başka eczaneye gidiyor",
                          "Bu hem satış kaybı hem hasta kaybı"],
             "rationale": "Mekanizma net ve sonucu iki başlıkta adlandırılmış, ama ne sıklıkla olduğuna dair tek bir sayı yok."},
            {"key": "market_size", "score": 1,
             "evidence": ["Türkiye'de 27 bin eczane var", "Yüzde 10'una ulaşırsak 2.700 eczane eder"],
             "rationale": "Yüzde 10 varsayımının hiçbir gerekçesi yok ve sonuç paraya bile çevrilmemiş; aritmetik pazar hesabı değildir."},
            {"key": "solution_differentiation", "score": 3,
             "evidence": ["Kurye ağını kendimiz işletmiyoruz, mevcut motokurye firmalarıyla anlaşıyoruz"],
             "rationale": "Sermaye kullanmayan bir işletme kararı ve makul; ama aynı kararı bir rakibin de alabileceği ortada."},
            {"key": "traction", "score": 5,
             "evidence": ["Son ay 4.100 transfer gerçekleşti, transfer başına ortalama 180 lira ilaç bedeli",
                          "Ağdaki eczanelerin yüzde 84'ü ayda en az bir kez kullanıyor"],
             "rationale": "Hacim ve aktif kullanım oranı birlikte veriliyor; yüzde 84 kayıt olmayı değil alışkanlığı ölçen bir sayı."},
            {"key": "team", "score": 2,
             "evidence": ["İki kurucu da eczacı", "Yazılımı dışarıya yaptırdık, içeride geliştirici yok"],
             "rationale": "Alan bilgisi tam ama ürün bir yazılım ve onu yapacak yetenek şirketin dışında; kritik bağımlılık kadro dışında."},
            {"key": "business_model", "score": 3,
             "evidence": ["Transfer başına 12 lira komisyon alıyoruz. Kurye maliyeti transfer başına 9 lira, yani net 3 lira",
                          "Ölçek arttıkça kurye pazarlığında iyileşme bekliyoruz ama bunu henüz test etmedik"],
             "rationale": "Birim ekonomi açıkça verilmiş ve marjın ince olduğu saklanmamış; iyileşme beklentisinin test edilmediği de söyleniyor."},
            {"key": "competition", "score": 3,
             "evidence": ["Ecza depolarının kendi uygulamaları var ama sadece depo-eczane arasında çalışıyor, eczane-eczane değil"],
             "rationale": "Yerleşik oyuncunun kapsamı doğru sınırlanmış; ama depoların bu tarafa geçmesini engelleyen bir şey söylenmiyor."},
            {"key": "financials_ask", "score": 4,
             "evidence": ["Bu turda 400 bin dolar istiyoruz. Yüzde 60'ı üç yeni şehir açılışı, yüzde 40'ı yazılımı içeri almak için ekip",
                          "Nakit 7 ay yetiyor"],
             "rationale": "Tutar, iki kalemli kullanım ve nakit süresi var; harcamanın hangi sonuca bağlandığı belirtilmediği için 5 değil."},
            {"key": "risk", "score": 3,
             "evidence": ["İlaç transferi mevzuata tabi ve şu an gri alanda ilerliyoruz",
                          "Ecza kurumundan görüş talep ettik, cevap gelmedi"],
             "rationale": "En büyük risk doğru teşhis edilmiş ve bir adım atılmış, ama cevapsız kalması karşısında hiçbir plan B yok."},
        ],
    },
    {
        "title": "Tekstil kalite kontrol kamerası — A serisi denemesi",
        "text": """Dokugöz

PROBLEM
Kumaş hatası atölyede gözle ayıklanıyor. Vardiya sonuna doğru kaçırma artıyor.

ÇÖZÜM
Tezgâhın üstüne kamera koyup hatayı anında işaretliyoruz. Yapay zekâ kullanıyoruz.

PAZAR
Bursa'da 4.000 tekstil atölyesi var. Bunun büyük kısmı bizim müşterimiz olabilir.

ÇEKİŞ
7 atölyede kurulu. Müşteriler memnun.

EKİP
Kurucu makine mühendisi, iki yıldır bu ürün üzerinde çalışıyor.

İŞ MODELİ
Cihaz satıyoruz, tanesi 240 bin lira. Üretim maliyeti hakkında bilgi paylaşmak
istemiyoruz.

REKABET
Alman ve İtalyan üreticiler var, onların cihazı çok daha pahalı.

FİNANSAL
Yatırım arıyoruz.

RİSK
Riskler yönetilebilir durumda.
""",
        "findings": [
            {"key": "problem_clarity", "score": 3,
             "evidence": ["Kumaş hatası atölyede gözle ayıklanıyor", "Vardiya sonuna doğru kaçırma artıyor"],
             "rationale": "Mevcut yöntem ve bozulduğu an doğru tarif edilmiş; ne kadar kaçırıldığı ve maliyeti ölçülmemiş."},
            {"key": "market_size", "score": 1,
             "evidence": ["Bursa'da 4.000 tekstil atölyesi var", "Bunun büyük kısmı bizim müşterimiz olabilir"],
             "rationale": "Sayım var, daraltma yok ve para hiç girmiyor; hedef dilim bir ölçüm değil bir umut."},
            {"key": "solution_differentiation", "score": 1,
             "evidence": ["Tezgâhın üstüne kamera koyup hatayı anında işaretliyoruz", "Yapay zekâ kullanıyoruz"],
             "rationale": "Ayrıştırıcı olarak öne sürülen tek şey bir teknoloji adı; rakiplerin yapmadığı hiçbir şey iddia edilmiyor."},
            {"key": "traction", "score": 2,
             "evidence": ["7 atölyede kurulu", "Müşteriler memnun"],
             "rationale": "Kurulum sayısı var ama ödeme, süreklilik ve sonuç yok; memnuniyet ölçülmemiş bir sıfat."},
            {"key": "team", "score": 2,
             "evidence": ["Kurucu makine mühendisi, iki yıldır bu ürün üzerinde çalışıyor"],
             "rationale": "Tek kurucu ve ürünün merkezindeki görü tarafında bir yetkinlik gösterilmiyor; süre bir yetenek kanıtı değil."},
            {"key": "business_model", "score": 2,
             "evidence": ["Cihaz satıyoruz, tanesi 240 bin lira",
                          "Üretim maliyeti hakkında bilgi paylaşmak istemiyoruz"],
             "rationale": "Fiyat var ama marj kasten verilmiyor; maliyet olmadan cihaz satışının iş modeli olarak değerlendirilmesi mümkün değil."},
            {"key": "competition", "score": 2,
             "evidence": ["Alman ve İtalyan üreticiler var, onların cihazı çok daha pahalı"],
             "rationale": "Rakip kategorisi ve tek eksen var, ama isim, fiyat farkı ve neden ucuz kalınabildiği yok."},
            {"key": "financials_ask", "score": 1,
             "evidence": ["Yatırım arıyoruz"],
             "rationale": "Tutar bile yok; bu bir finansal plan değil bir cümle."},
            {"key": "risk", "score": 1,
             "evidence": ["Riskler yönetilebilir durumda"],
             "rationale": "Tek bir risk adlandırılmamış; başlık var, içerik yok."},
        ],
    },
    {
        "title": "Kurumsal mutfak israf ölçümü — köprü",
        "text": """Tartım

PROBLEM
Kurumsal yemekhanede ne kadar yemek çöpe gittiği bilinmiyor. 6 tesiste iki hafta
boyunca elle tarttık: tabak artığı kişi başı günde 210 gram, tencere artığı ise
toplamın yüzde 31'i. İkincisi tamamen üretim planlamasıyla ilgili ve kimse
ölçmüyor.

ÇÖZÜM
Çöp kovasının altına terazi, üstüne kamera. Ne atıldığını ve ne kadar atıldığını
ayrı ayrı kaydediyoruz.

PAZAR
Türkiye'de 1.000 kişi üstü yemek çıkaran 3.400 tesis olduğunu sektör derneğinin
2025 raporundan aldık. Tesis başına yıllık 96 bin lira abonelikle ulaşılabilir
pazar 326 milyon lira. Derneğin sayımı üyeleriyle sınırlı, gerçek sayı daha
yüksek olabilir.

ÇEKİŞ
11 tesis ödüyor, aylık yinelenen gelir 88 bin lira. En eski müşteri 14 aydır
kullanıyor ve bu süre içinde tencere artığını yüzde 31'den yüzde 12'ye indirdi.
Son 6 ayda hiç müşteri kaybetmedik.

EKİP
Kurucu 12 yıl toplu yemek sektöründe operasyon müdürlüğü yaptı. Teknik ortak
gömülü sistem ve görüntü işleme tarafında 8 yıllık.

İŞ MODELİ
Tesis başına yıllık abonelik, donanım dahil. Donanım maliyeti tesis başına 21 bin
lira, ilk yıl marjı yüzde 78, sonraki yıllar yüzde 96.

REKABET
Dünyada iki büyük oyuncu var ve ikisi de Türkiye'de satmıyor. Yerelde doğrudan
rakip yok. Asıl rakip hiç ölçmemek.

FİNANSAL
900 bin dolar istiyoruz: yüzde 50 satış ekibi, yüzde 30 donanım stoku, yüzde 20
ürün. Bu turla 30 aya, tur olmadan 9 aya kadar nakdimiz var.

RİSK
Donanım tesise kurulu ve müşteri ayrılırsa geri sökülmesi gerekiyor; 14 ayda bir
kez oldu ve söküm maliyeti 4 bin lira çıktı. İkinci risk yemek şirketlerinin
konsolidasyonu: üç büyük grup pazarın yarısını tutuyor ve biri bizi almazsa
büyüme yavaşlar.
""",
        "findings": [
            {"key": "problem_clarity", "score": 5,
             "evidence": ["6 tesiste iki hafta boyunca elle tarttık: tabak artığı kişi başı günde 210 gram",
                          "tencere artığı ise toplamın yüzde 31'i"],
             "rationale": "Problem birincil ölçümle konmuş ve iki bileşene ayrılmış; ikinci bileşenin kimse tarafından ölçülmediği tespiti fırsatın kendisi."},
            {"key": "market_size", "score": 5,
             "evidence": ["sektör derneğinin 2025 raporundan aldık", "Derneğin sayımı üyeleriyle sınırlı, gerçek sayı daha yüksek olabilir"],
             "rationale": "Kaynaklı taban, fiyatla çarpım ve kaynağın kendi sınırının belirtilmesi; tahminin hangi yöne yanlı olduğunu söylemek onu güçlendirir."},
            {"key": "solution_differentiation", "score": 3,
             "evidence": ["Ne atıldığını ve ne kadar atıldığını ayrı ayrı kaydediyoruz"],
             "rationale": "Ölçümü iki boyuta ayırmak gerçek bir fark ve problem bölümüyle tutarlı; ama kopyalanmasını zorlaştıran bir şey yok."},
            {"key": "traction", "score": 5,
             "evidence": ["11 tesis ödüyor, aylık yinelenen gelir 88 bin lira",
                          "bu süre içinde tencere artığını yüzde 31'den yüzde 12'ye indirdi"],
             "rationale": "Gelir, süre ve müşteride ölçülmüş sonuç birlikte; en eski müşterinin sonucu ürünün iddiasını doğrudan kanıtlıyor."},
            {"key": "business_model", "score": 5,
             "evidence": ["Donanım maliyeti tesis başına 21 bin lira, ilk yıl marjı yüzde 78, sonraki yıllar yüzde 96"],
             "rationale": "Donanımlı aboneliğin iki farklı yıl marjı ayrı ayrı verilmiş; modelin nasıl para kazandığı tek satırda görünüyor."},
            {"key": "team", "score": 5,
             "evidence": ["Kurucu 12 yıl toplu yemek sektöründe operasyon müdürlüğü yaptı",
                          "Teknik ortak gömülü sistem ve görüntü işleme tarafında 8 yıllık"],
             "rationale": "Alan ve teknoloji tarafları ayrı ayrı ve ürünün gerektirdiği tam disiplinlerde karşılanmış."},
            {"key": "competition", "score": 4,
             "evidence": ["Dünyada iki büyük oyuncu var ve ikisi de Türkiye'de satmıyor", "Asıl rakip hiç ölçmemek"],
             "rationale": "Hem uluslararası oyuncular hem de mevcut alışkanlık adlandırılmış; oyuncuların neden girmediği açıklanmadığı için 5 değil."},
            {"key": "financials_ask", "score": 5,
             "evidence": ["900 bin dolar istiyoruz: yüzde 50 satış ekibi, yüzde 30 donanım stoku, yüzde 20 ürün",
                          "Bu turla 30 aya, tur olmadan 9 aya kadar nakdimiz var"],
             "rationale": "Tutar, üç kalemli dağılım ve iki senaryolu nakit süresi; turun alınmadığı hâl ayrıca hesaplanmış."},
            {"key": "risk", "score": 5,
             "evidence": ["14 ayda bir kez oldu ve söküm maliyeti 4 bin lira çıktı",
                          "üç büyük grup pazarın yarısını tutuyor ve biri bizi almazsa büyüme yavaşlar"],
             "rationale": "İki farklı türde risk var: biri gerçekleşmiş ve maliyeti ölçülmüş, diğeri kanalın yapısından geliyor ve sonucu adlandırılmış."},
        ],
    },
    {
        "title": "Yat marina yönetimi — tohum öncesi",
        "text": """Marinar

PROBLEM
Marina işletmesi bağlama, bakım ve fatura kayıtlarını üç ayrı yerde tutuyor.
Muğla'daki 9 marinanın 7'sinde bu üç kaydın en az biri hâlâ kâğıtta.

ÇÖZÜM
Üç kaydı tek yerde topluyoruz.

PAZAR
Türkiye'de 82 marina var ve toplam bağlama kapasitesi 28 bin tekne. Marina başına
yıllık 140 bin lira ile pazar 11,5 milyon lira. Küçük bir pazar olduğunu biliyoruz;
bu yüzden Yunanistan ve Hırvatistan'a açılmayı planlıyoruz ama oradaki rakam
elimizde yok.

ÇEKİŞ
2 marina ödüyor, 1 marina pilotta. Toplam yıllık sözleşme 280 bin lira.

EKİP
Kurucu 6 yıl marina işletme müdürlüğü yaptı. Yazılım tarafında bir ortak var,
daha önce iki SaaS ürünü çıkarmış.

İŞ MODELİ
Marina başına yıllık lisans. Bağlama sayısına göre kademeli fiyat.

REKABET
İki İtalyan ve bir Hırvat yazılımı Türkiye'de kullanılıyor. Üçü de Türkçe
desteklemiyor ve e-fatura entegrasyonu yok; bu iki eksik satışlarımızın ikisinde
de belirleyici oldu.

FİNANSAL
350 bin dolar istiyoruz. Nakit 4 ay yetiyor.

RİSK
Pazarın küçüklüğü en büyük risk ve bunu yurt dışı açılımıyla çözmeyi planlıyoruz.
Açılım için gereken sertifikasyon ve yerelleştirme maliyetini henüz çıkarmadık.
""",
        "findings": [
            {"key": "problem_clarity", "score": 4,
             "evidence": ["Muğla'daki 9 marinanın 7'sinde bu üç kaydın en az biri hâlâ kâğıtta"],
             "rationale": "İddia sayılmış bir gözlemle desteklenmiş; kâğıt kaydın ne maliyete yol açtığı ölçülmediği için 5 değil."},
            {"key": "market_size", "score": 4,
             "evidence": ["Türkiye'de 82 marina var ve toplam bağlama kapasitesi 28 bin tekne",
                          "Küçük bir pazar olduğunu biliyoruz"],
             "rationale": "Hesap dürüstçe yapılmış ve sonucun küçüklüğü saklanmamış; genişleme planının rakamı olmadığı kendi ağzıyla söyleniyor."},
            {"key": "solution_differentiation", "score": 1,
             "evidence": ["Üç kaydı tek yerde topluyoruz"],
             "rationale": "Tek cümlelik bir kapsam ifadesi; hiçbir rakibin yapmadığı bir şey ya da yapmasını zorlaştıran bir engel yok."},
            {"key": "traction", "score": 2,
             "evidence": ["2 marina ödüyor, 1 marina pilotta", "Toplam yıllık sözleşme 280 bin lira"],
             "rationale": "Ödeme gerçek ve rakam verilmiş, ama iki müşteri bir eğilim göstermez ve süre bilgisi hiç yok."},
            {"key": "team", "score": 4,
             "evidence": ["Kurucu 6 yıl marina işletme müdürlüğü yaptı",
                          "Yazılım tarafında bir ortak var, daha önce iki SaaS ürünü çıkarmış"],
             "rationale": "Alan bilgisi ve ürün çıkarma tecrübesi ikisi birden var ve ikisi de doğrulanabilir biçimde ifade edilmiş."},
            {"key": "business_model", "score": 3,
             "evidence": ["Marina başına yıllık lisans. Bağlama sayısına göre kademeli fiyat"],
             "rationale": "Model ve fiyatlama ekseni net; kademelerin rakamı ve maliyet tarafı yok."},
            {"key": "competition", "score": 5,
             "evidence": ["Üçü de Türkçe desteklemiyor ve e-fatura entegrasyonu yok",
                          "bu iki eksik satışlarımızın ikisinde de belirleyici oldu"],
             "rationale": "Rakipler sayılmış, ayrışma iki somut eksikte toplanmış, ve bu eksiklerin gerçekten satış kazandırdığı kendi müşteri kayıtlarıyla gösterilmiş."},
            {"key": "financials_ask", "score": 2,
             "evidence": ["350 bin dolar istiyoruz", "Nakit 4 ay yetiyor"],
             "rationale": "Tutar ve nakit süresi var, ama kullanım planı yok ve 4 ay bir turu kapatmak için dar bir süre."},
            {"key": "risk", "score": 3,
             "evidence": ["Pazarın küçüklüğü en büyük risk ve bunu yurt dışı açılımıyla çözmeyi planlıyoruz",
                          "Açılım için gereken sertifikasyon ve yerelleştirme maliyetini henüz çıkarmadık"],
             "rationale": "Doğru riski işaret etmiş ve bir çözüm önermiş, ama çözümün maliyeti bilinmiyor — yani risk aslında ertelenmiş."},
        ],
    },
]
