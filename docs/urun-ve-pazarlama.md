# Ürün tanımı ve pazara çıkış planı

Bu doküman "bu repo neyi satıyor ve kime" sorusunun cevabı. Teknik mimari için
[nasil-calisiyor.md](./nasil-calisiyor.md), fine-tuning için
[peft-nedir.md](./peft-nedir.md).

---

## 1. Ürün tek cümlede

> Vakayı verirsin, tanımlı bir rubriğe göre kriter kriter puanlanmış, kanıtı
> gösterilen bir analiz raporu alırsın — hepsi kendi donanımında.

İki alan:

| Alan | Girdi | Çıktı |
|---|---|---|
| **Startup yatırım yapılabilirliği** | Pitch deck, finansallar, ekip bilgisi | Kriter kırılımlı yatırılabilirlik puanı + gerekçe + eksikler |
| **Dijital pazarlama analizi** | Marka/ürün bilgisi, hedef kitle, bütçe | Kanal önerisi + platform seçimi + gerekçe |

Psikolojik durum analizi **kapsam dışı**. Sağlık bitişiği bir alanda tanı
üretmek düzenlemeye tabi ve bu donanımdaki bir modelin kapasitesinin dışında.

---

## 2. Asıl fikir: model puan vermez, rubriği doldurur

Bu ürünün rakiplerinden ayrıldığı yer burası, ve teknik bir detay değil — satış
argümanının kendisi.

```
Vaka  →  rubrik kriterleri getirilir        (RAG)
      →  model her kritere KANIT + değerlendirme yazar   (LoRA şemaya zorlar)
      →  şema doğrulanır
      →  ağırlıklı toplam DETERMİNİSTİK hesaplanır       (scoring.go)
      →  rapor + kaynak alıntıları
```

"LLM'in verdiği yatırım puanına neden güveneyim?" sorusunun cevabı: **güvenmiyorsun.**
Rubriğe güveniyorsun, modelin topladığı kanıtı denetliyorsun, aritmetik açık ve
bizim. Model bir yargı organı değil, bir kanıt toplayıcı.

Bunun ticari sonucu: **reddi savunabilirsin.** "AI hayır dedi" savunulamaz;
"kriter 4'ten 5 üzerinden 2 aldı, gerekçesi deck'in 12. sayfasındaki şu veri"
savunulabilir. Hızlanma programları ve melek ağları için bu bir özellik değil,
zorunluluk.

---

## 3. Kime satılıyor (ICP)

Üç aday var, ikisi tuzak.

### Beachhead: hızlandırma programları, kuluçkalar, melek ağları

**Neden bunlar:**

- **Hacim + tutarlılık acısı gerçek.** Bir çağrıya 300 başvuru gelir, 5 kişi
  değerlendirir, 60'ıncı deck 3'üncü deck'le aynı ölçütle okunmaz. Yorgunluk
  kaynaklı tutarsızlık bilinen ve rahatsız edici bir problem.
- **Savunulabilirlik zorunlu.** Reddedilen başvuru sahibi gerekçe ister; bazen
  kamu fonu söz konusudur ve denetim izi aranır.
- **Veri egemenliği bir itiraz değil, bir gereklilik.** Başvuranın finansalları
  üçüncü tarafa gönderilemez. Self-hosted olmamız burada rakipleri eliyor.
- **Sayılabilir bir liste.** Türkiye'deki hızlandırma programları, teknopark
  kuluçkaları ve melek ağları isim isim listelenebilir — 40-60 kurum. Bu bir
  pazarlama kampanyası değil, bir tablo ve 60 e-posta.

**Tuzak 1 — girişimciler.** Kendi kendini değerlendirmek isteyen kurucular
büyük bir kitle ama ödeme istekleri düşük, kullanım tek seferlik, churn %100'e
yakın. Huninin tepesi olarak iyi, gelir kaynağı olarak kötü.

**Tuzak 2 — büyük VC'ler.** Kendi süreçleri, kendi araçları ve satın alma
komiteleri var. Tek geliştiricili bir üründen almazlar. Sonraki aşama.

### İkinci alan: dijital pazarlama

Ajanslar ve KOBİ pazarlama sorumluları. Kanal karması kararı için.

Bu alan **beachhead değil, çoklu-alan kanıtı.** Rekabet çok daha kalabalık ve
veri egemenliği argümanı burada zayıf (pazarlama brief'i finansal veri kadar
hassas değil). Değeri: "bu bir yatırım aracı değil, alan takılabilir bir analiz
motoru" iddiasını ispatlaması.

---

## 4. Konumlandırma

**Söylenecek:**

> Deal flow'unuzdaki her başvuruyu aynı ölçütle, savunulabilir şekilde puanlayın.
> Veriler sunucunuzdan çıkmaz.

**Söylenmeyecek:**

- ❌ "Yapay zeka yatırım kararı verir" — kimse karar veren bir AI satın almaz,
  sorumluluğu devretmek istemez. Satılan şey **ilk eleme tutarlılığı**.
- ❌ "En iyi model" — 6 GB kartta 2B'lik bir model bu kavgayı kaybeder.
  Rekabet model kalitesinde değil, **rubrik şeffaflığı + egemenlik + tutarlılık**
  ekseninde.
- ❌ "Otomatik" — insan-döngüde olduğu vurgulanmalı. Alıcı işini kaybetmek
  istemiyor, işini hızlandırmak istiyor.

**Rakiplere karşı konum:**

| Rakip tipi | Onların zayıflığı |
|---|---|
| ChatGPT'ye deck yapıştırmak | Tutarsız, denetim izi yok, veri dışarı çıkıyor |
| Genel AI değerlendirme SaaS'ları | Rubrik kapalı kutu, bulut zorunlu, TR diline zayıf |
| Elle Excel skorlama | Tutarlı ama yavaş ve ölçeklenmiyor |

Bizim yerimiz: **Excel'in savunulabilirliği + LLM'in hızı.**

---

## 5. Fiyatlama

Self-hosted olmak kullanım başına ücretlendirmeyi hem teknik olarak zor hem de
ticari olarak yanlış kılıyor — müşteri tam da sizin sayacınızı istemediği için
self-hosted alıyor.

**Açık çekirdek (open core):**

| Katman | İçerik | Fiyat |
|---|---|---|
| **Core (açık kaynak)** | Tek kullanıcı, tek alan, hazır rubrik, kendi kurulumun | Ücretsiz |
| **Team** | Çok kullanıcı, rol yönetimi, özel rubrik editörü, rapor dışa aktarma | Yıllık kurum lisansı |
| **Domain** | Kendi alanınıza özel adapter eğitimi + rubrik danışmanlığı | Proje bazlı |

Açık çekirdek burada bir ideoloji değil bir dağıtım stratejisi: **rubrik
şeffaflığı zaten ürünün iddiası.** Kapalı kaynak bir "şeffaf skorlama" ürünü
kendi argümanıyla çelişir. Kodu açmak iddiayı ispatlıyor.

---

## 6. Kanallar — tek kişi, bütçe sıfır

Sıraya göre, ilk üçü asıl iş:

**1. Sayılabilir listeye doğrudan temas.**
Pazarlama değil, satış. 40-60 kurum listelenir, her birine tek bir şey gönderilir:
*onların kendi geçmiş çağrısından anonimleştirilmiş bir başvurunun tam raporu.*
Demo istemek yerine demoyu göndermek. Dönüşüm oranı bir kampanyanın kat kat üstünde.

**2. MCP dizini.**
Analiz motorunu MCP sunucusu olarak yayınlamak, Claude/Cursor kullanan kitleye
bedava dağıtım demek. Şu an bu dizinlerde rekabet düşük ve niyet yüksek.
Teknik alıcıya ulaşmanın en ucuz yolu.

**3. Ölçüm içeriği.**
Repoda gerçek ölçüm var: tarayıcı/GPU gecikme karşılaştırması, load balancer
denemesi (least_conn'un neden başarısız olduğu), LoRA öncesi/sonrası şema uyum
oranı. *"6 GB'lık bir GTX 1660 Ti'da 2B modeli fine-tune ettik, işte olanlar"*
bu nişte paylaşılan ve aranan içeriğin tam kendisi. Uydurma blog yazısı değil,
elinizdeki ölçüm.

**4. Kod tabanının kendisi.**
Bu repodaki yorumlar alışılmadık derecede iyi — neden yapıldığını anlatan,
başarısız denemeyi de yazan yorumlar. Bu bir pazarlama varlığı. Mühendislik
dokümanını içeriğe çevirmek sıfır ek iş.

**5. MasterFabric Academy demo günü.**
Capstone zaten oraya çıkıyor. İlk referans ve ilk geri bildirim kaynağı.

---

## 7. Satmadan önce hazır olması gerekenler

Sıra önemli — bunlar olmadan yapılan temas yakılmış temastır.

- [ ] **Bir tane kusursuz örnek rapor.** Anonimleştirilmiş gerçek bir deck →
      tam analiz. Satış materyalinin tamamı bu.
- [ ] **Rubriğin kendisi yayında.** Şeffaflık iddiası ancak rubrik okunabilirse
      doğru. Kapalı rubrik = kapalı kutu = rakiplerle aynı yer.
- [ ] **Tutarlılık ölçümü.** Aynı vaka N kez → puan dağılımı. "Tutarlı" demek
      yetmez, sayısı olmalı.
- [ ] **Kurulum 15 dakikanın altında.** Self-hosted ürünün en büyük kaybı
      kurulum aşamasında olur. Tek `docker compose up` hedefi.
- [ ] **İlk 60 saniye.** Kayıt sonrası boş ekran yerine örnek korpus + örnek
      vaka. Kullanıcı kendi verisini girmeden önce değeri görmeli.

---

## 8. Bu planın zayıf noktaları

Dürüst olmak gerekirse:

- **Rubrik kalitesi ürünün tavanı.** Kötü bir yatırım rubriği, mükemmel bir
  yazılımla da kötü sonuç verir. Alan uzmanıyla çalışılmadan bu ürün olmaz —
  bu bir yazılım işi değil, yazılım + metodoloji işi.
- **Self-hosted satış döngüsü uzundur.** Kurulum, güvenlik onayı, donanım.
  Bulut SaaS'a göre kat kat yavaş.
- **2B model yeterli mi bilmiyoruz.** Rubrik doldurma göreve LoRA ile
  oturtulabilir ama bu henüz ölçülmedi. Ölçülene kadar tüm plan bir varsayımın
  üstünde duruyor — ilk yapılacak iş bu ölçüm.
- **Tek geliştirici tek kurumluk destek verebilir.** İlk müşteri sayısı bilinçli
  olarak düşük tutulmalı.
