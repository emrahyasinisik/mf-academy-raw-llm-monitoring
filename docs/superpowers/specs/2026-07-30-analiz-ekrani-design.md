# Analiz ekranı — tasarım

**Tarih:** 30 Temmuz 2026
**Durum:** onaylandı, plana geçilecek

## Neden

Ürün çalışıyor ama gösterilemiyor.

`internal/analysis` canlı: Render'daki backend `{"status":"ok"}` veriyor, tünel
açık, `/analysis/*` route'ları auth arkasında ayakta, rubrikler ağırlıklarıyla
veritabanında, puanı hesaplayan Go kodu test edilmiş. Bugün bir vaka gönderip
rubrik-puanlı rapor almanın iki yolu var: curl, ya da MCP üzerinden Claude.

Frontend'de karşılığı yok. Dört ekran var — Üreteç, Persona, Metrikler, Yönetim
— ve hiçbiri analiz çalıştırmıyor. `api.analysisRun()` `lib/api.ts:204`'te
duruyor, çağıran bileşen yok.

Bu bir arıza değil, eksik bir yüzey. Ama sonucu şu: alıcıya açıp
gösterebileceğin bir yer yok, ve `docs/urun-ve-pazarlama.md` §7'nin birinci
maddesi — tek kusursuz örnek rapor — bir ekran olmadan üretilemiyor.

## Kapsam

**Bu spec'in ürettiği tek şey:** `AnalizView`, ürünün gerçek analiz ekranı.

**Kapsam dışı, ve sırası belli:**

- Sentetik örnek vaka ve örnek raporun kendisi. Bu ekran ayağa kalktıktan sonra,
  ayrı iş.
- Kutuya sade taban Qwen3-4B yüklenmesi. Rapor için gerekli, **ekran için
  değil** — ekran hangi model yüklüyse onunla çalışır.
- Backend'e auth'suz public rapor yolu. Karar verildi: dışarı giden şey PDF,
  yeni public yüzey yok.
- PDF export endpoint'i. Yazdırma tarayıcıda.
- Pazarlama rubriği için ikinci örnek rapor. §7 "bir tane" diyor, beachhead
  yatırılabilirlik.
- Tutarlılık sayısı (§7 madde 3). Makinesi hazır, ayrı iş.

## Yerleşim

`mf-frontend/src/components/views/AnalizView.tsx`, AppShell'e beşinci master
view.

**Kalıcı mount edilen grupta olmalı** — Üreteç ve Persona gibi
`opened.has(...)` + `<Pane>` ile; Metrikler ve Yönetim gibi `view === "x" &&`
ile değil. Gerekçe `AppShell.tsx:8-13`'te zaten yazılı: analiz tünelin ardındaki
GPU'da onlarca saniye sürüyor, view söküldüğünde isteği tutan bileşen gidiyor,
sonucun ineceği yer kalmıyor. İş durmaz — kayıt yine yazılır — ama koltuktan
bakınca sekmeyi terk etmek işi öldürmüş gibi görünür.

Tek dosya, içinde alt bileşenler. Alt view (`#analysis/...`) yok: bu ev tek
parça büyük view'lara alışkın (AdminView 750, CodegenView 574 satır) ve iki
seviyeli yönlendirme burada kazandıracağı şeyi kazandırmıyor.

## Nav sırası

Önerilen: **Analiz · Üreteç · Persona · Metrikler · Yönetim**.

Şu an Üreteç başta ve gerekçesi `AppShell.tsx:26-38`'de yazılı: "kutunun servis
ettiği şey o". Bu gerekçe kutuya sade taban yüklendiği anda tersine dönüyor —
asıl ürün analiz, ve fine-tune'unu kaybetmiş olan taraf Üreteç. Sıra
değiştiğinde o yorum yeni gerekçesiyle yeniden yazılır; eski cümlenin yanlış
yerde durması, sıranın kendisinden daha pahalı.

## Durum makinesi ve veri akışı

Dört hal: `boş → çalışıyor → rapor`, ve yanda `hata`.

| Olay | Çağrı |
|---|---|
| Mount | `api.analysisDomains()`, `api.analysisList()` |
| Çalıştır | `api.analysisRun({domain, subject_title, subject})` |
| Geçmişten seç | `api.analysisGet(id)` |

Varsayılan rubrik: yatırılabilirlik.

Durum çubuğu için Üreteç'in yaptığı gibi `useMachine().begin(label)`
kullanılır. Bilinen pürüz: tamamlama geri çağrısı `Run` bekliyor, `Assessment` o
değil. `null` geçilir — çubuk geçen süreyi sayar, `lastRun`'ı doldurmaz.
Alternatifi store'u genişletmek, ve bu ekran için bedeli faydasından büyük.

## Girdi uzunluğu — ilk karşılaşılacak hata

`analysis` yolunda girdi uzunluğu koruması **yok**. `decision`'da var
(`agent.go:78`), analizde yok. `LLM_MAX_PROMPT_TOKENS` varsayılanı 1200 ve
dönüşüm oranı 2.2 karakter/token (`agent.go:132`, Türkçe'nin kötü
tokenleşmesine göre temkinli seçilmiş), yani pratik sınır **~2640 karakter (kaba üst sınır; gerçek vaka bütçesi bundan çok daha dar — plana bak)**.

Bunu aşan metin doğrudan mlc'ye gidiyor ve ham 400 olarak dönüyor. Ekranın ilk
yaptığı iş deck yapıştırmak olduğu için bu, kullanıcının göreceği ilk hata olur.

Ekranın sorumluluğu: vaka alanının altında **canlı karakter sayacı ve sınır**,
sınır aşıldığında gönderimi engelleyen görünür bir uyarı. Sayaç backend'le aynı
2.2 oranını kullanır — iki yerde iki farklı tahmin, tahmin olmaktan çıkıp çelişki
olur.

Backend'e `decision`'daki gibi bir koruma eklemek doğru olurdu ve bu spec'in
dışında bırakılıyor; ekran tarafındaki uyarı, hatayı okunabilir kılmaya yeter.

## Rapor render

Ürünün tüm iddiası bu bölümde. İki değişmez, ikisi de `lib/types.ts:146-160`'ta
yazılı ve UI'ın bozmaması gereken şeyler:

- **`score: null` sıfır değildir.** "Deck ekipten hiç söz etmiyor" ile "ekip
  zayıf" farklı bulgular. Null "kanıt yok" olarak çizilir, 0 olarak değil.
  Sıfır olarak çizmek, sessizliği başarısızlık diye puanlamak olur.
- **`overall_score` asla `coverage` olmadan gösterilmez.** 0.9 kapsamda 68 ile
  0.3 kapsamda 68 aynı bulgu değil.

İskelet:

**Başlık şeridi** — vaka adı, rubrik adı ve sürümü, toplam puan ve hemen yanında
kapsam, model kimliği, süre, şema geçerliliği rozeti.

**Kriter tablosu** — sıra `criteria_snapshot`'tan gelir, `findings`'ten değil.
Snapshot rubriğin o anki hali; bulgular eksik gelebilir ve eksik bir bulgu
tablodan satır düşürmemeli, "kanıt yok" göstermeli. Her satır: etiket, ağırlık,
`puan / scale_max`, ve o satırın toplama **katkısı**.

**Katkı kolonu pazarlık konusu değil.** "Aritmetik bizim, açık ve denetlenebilir"
iddiası ancak toplamın nasıl çıktığı satır satır görünüyorsa doğru. Bir başvuru
sahibinin itiraz edebileceği, bir operatörün savunabileceği şey bu kolon. Onsuz
rapor yine bir sayı söyler ve kimse tartışamaz — yani rakiplerle aynı yere düşer.

**Formül `scoring.go`'dan birebir alınmalı, yeniden türetilmemeli.** Naif yazılırsa
kolon toplama eşit çıkmaz, ve aritmetiğini gösterip yanlış toplayan bir rapor,
hiç göstermeyenden kötüdür. `Score()`'un yaptığı (`scoring.go:44-89`):

- Ağırlığı ≤ 0 olan kriter tamamen atlanır — rubrikteki bir yazım hatası sonucu
  ters çeviremesin diye.
- Puan `[0, scale_max]` aralığına **kırpılır**, reddedilmez. Model 0-5 skalasında
  bazen 6 döndürüyor; bunu "azami" saymak bulguyu atmaktan az bilgi kaybettiriyor.
- `scoredWeight` = kanıtı olan kriterlerin ağırlık toplamı.
- **Toplam `scoredWeight`'e göre yeniden normalize edilir, `totalWeight`'e göre
  değil:** `100 × Σ(w × s/max) / scoredWeight`. Tam rubrik ağırlığına bölmek
  kapsamı puanın içine katlardı — ele alınmamış bir kriter, kötü ele alınmış biri
  kadar puanı aşağı çekerdi, ki bu tasarımın kaçındığı tam o karıştırma.

Dolayısıyla bir satırın gösterilecek katkısı `100 × w × (s/max) / scoredWeight`
ve bu katkılar **tam olarak** toplam puana eşitlenir. Kanıtsız kriterler katkı
göstermez ve `scoredWeight`'e girmez — onların hesabı kapsam sayısında verilir.

**Kanıt** — her kriterin altında birebir alıntılar ve gerekçe, açılır. Alıntı,
parafraz değil: parafraz kaynağa karşı doğrulanamaz, ve doğrulanamayan bir atıf
hiç atıf olmamasından kötüdür çünkü sağlam görünür.

**Kanıtsız kriterler** toplamdan görünür biçimde düşer, sebebiyle birlikte.
Sessizce sıfır sayılmaz.

## Yazdırma

`@media print`: nav ve chrome gider, tüm kanıt blokları açılır, beyaz zemin
siyah metin.

Örnek raporun PDF'i buradan çıkacak. Ekran görüntüsü yerine yazdırma seçildi
çünkü kural bir kez yazılıyor ve çıktısı seçilebilir metin oluyor — ekran
görüntüsü okunamayan, aranamayan bir görsel.

## Hata halleri

Hepsi okunabilir olmalı; hiçbiri ekranı çökertmemeli.

| Hal | Ne görünür |
|---|---|
| `LLM_BASE_URL` boş → 503 | Çıkarım makinesine ulaşılamıyor. Persona'nın yaptığı gibi makineyi bildirir, çökmez. |
| Girdi sınırı aşıldı | Gönderimden **önce**, sayaçla birlikte. |
| mlc 400 (yine de olursa) | Ham gövde değil, çevrilmiş bir cümle. |
| Zaman aşımı | Analiz route'unun kendi bütçesi var; ekran süreyi sayar ve bunu söyler. |
| `schema_valid=false` | Rapor yine çizilir, rozetle işaretlenir. Gizlenmez — bu bilgi operatörün işine yarar. |

## Reddedilen alternatifler

- **Alt view'lu master view (`#analysis/rapor-<id>`).** Deep-link örnek raporu
  ekran görüntüsüne hazırlarken işe yarardı. Tek parça view tercih edildi; evin
  deseni bu, ve bir tık fazlaya değmez.
- **Önce sadece render, fixture ile.** Fixture'la çizilen raporun gerçek çıktıyla
  uyuşmadığı ancak sonunda görülür.
- **Auth'suz public rapor linki.** Prospect'e ham link gitmesi daha iyi olurdu
  ama backend'e yeni bir public yüzey açmak gerekiyordu. PDF seçildi.
- **Rapor render'ının ayrı `components/ui/` bileşeni olması.** İleride PDF
  route'u ya da public sayfa için yeniden kullanılabilirdi. Bugün ikisi de
  kapsam dışı, yani YAGNI.

## Bitmiş sayılma koşulu

- Analiz nav'da, tıklanınca açılıyor, rubrikleri yüklüyor.
- Bir vaka yapıştırılıp çalıştırılabiliyor; sınır aşılırsa gönderimden önce
  uyarıyor.
- Çalışırken başka bir view'a geçip dönmek sonucu kaybettirmiyor.
- Dönen rapor, seçili rubriğin `criteria_snapshot`'ındaki **her kriter** için ya
  alıntılı bir puan ya da "kanıt yok" gösteriyor; hiçbir null 0 olarak
  çizilmiyor.
- Toplam puan yanında kapsamla birlikte, ve katkı kolonundaki sayıların toplamı
  gösterilen toplam puana eşit — yuvarlama farkı dışında.
- Geçmiş listesinden eski bir rapor açılabiliyor.
- `Ctrl/Cmd+P` okunabilir bir PDF üretiyor.
- `npm run build` ve `npm run lint` temiz.

## Bunun açtığı sonraki adımlar

Sırayla, ve hiçbiri bu spec'te değil:

1. Sentetik yatırım vakası — ~2640 karakter (kaba üst sınır; gerçek vaka bütçesi bundan çok daha dar — plana bak) sınırına sığacak şekilde tasarlanmış,
   sentetik olduğu dosyada ve raporda etiketli.
2. Kutuya sade taban Qwen3-4B, ve backend'deki dört `defaultModel` sabitinin
   düzeltilmesi (`analysis`, `wiki`, `decision`, `admin/mcp`).
3. Örnek raporun üretilip PDF'e alınması. §7 madde 1 burada kapanır.
4. Tutarlılık sayısı — §7 madde 3, makinesi hazır.
