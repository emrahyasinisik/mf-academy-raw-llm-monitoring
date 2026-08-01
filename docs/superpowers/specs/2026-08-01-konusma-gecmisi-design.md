# Konuşma geçmişi — tasarım

**Tarih:** 1 Ağustos 2026
**Durum:** onaylandı, plana hazır

## Ne yapıyoruz ve neden

Kullanıcı ne yazdığını ve ne cevap aldığını göremiyor. Persona ekranında
konuşma yalnızca istemcinin belleğinde; sekme kapanınca gidiyor. Üreteç
ekranında ise veri **zaten var** — `llm_runs` her istemi ve yanıtı saklıyor,
`GET /llm/runs` ve `GET /llm/runs/{id}` çalışıyor — ama frontend'de
`api.listRuns` ve `api.getRun`'ın **hiçbir çağrı yeri yok.** Veri var, ekran
yok.

Bunun çoğu zaten yazılmış. `feat/persona-history` dalı (commit `a52aaf0`,
1627 satır) `HistoryPanel.tsx`'i, iki ekranın entegrasyonunu, `conversations`
+ `conversation_messages` tablolarını ve `GET/PATCH/DELETE
/decision/conversations/{id}` uçlarını getiriyor. `git merge-tree` çakışma
bildirmiyor.

Sıfırdan yazmıyoruz. Yapılacak iş, o dalı **bugünkü main'e uygun hale
getirmek** — ki main o daldan sonra sakladığı her şeye 30 günlük bir ömür ve
bir gizlilik beyanı kazandı.

## Verilen kararlar

1. **Dal bitirilir, yeniden yazılmaz.**
2. **Konuşmalar redakte edilmez, silinir.** 30 günde satır gider.
3. Ölçüt `created_at` değil **`last_turn_at`**.

### Neden konuşmalarda silme, raporlarda redaksiyon

Raporlarda redaksiyonu seçtik çünkü satırın kendisi bir ölçüm taşıyordu:
`schema_valid`, `coverage`, ve bir trial grubunun ürettiği tutarlılık figürü.
Satırı silmek o toplu ölçüleri geriye dönük değiştirirdi.

Konuşmalar hiçbir toplu ölçümü beslemiyor. Korunacak bir sayı olmadığı için
redaksiyon yalnızca boş satır biriktirmek olurdu. `conversations`'tan `DELETE`,
`conversation_messages`'ı `ON DELETE CASCADE` ile birlikte götürüyor — tek
ifade, ve redaksiyondan daha koruyucu.

### Neden `last_turn_at`

`created_at` ölçüt olsaydı, hâlâ konuştuğun bir thread otuzuncu günde ortasından
silinirdi. `last_turn_at` "bu konuşma otuz gündür dokunulmadı" diyor, ki
saklama süresinin kastettiği şey bu. Sütun zaten
`idx_conversations_user_active` içinde indeksli.

## Yapılacak dört şey

### 1. Konuşma süpürgesi

```go
// decision.Store
SweepConversations(ctx context.Context, olderThan time.Time) (int64, error)
```

`DELETE FROM conversations WHERE last_turn_at < $1`. Mesajlar cascade ile
gider.

`retention` paketi üçüncü bir arayüz ve `Sweep`'e üçüncü bir parametre alır;
`Result` üçüncü bir sayaç. Mevcut davranış korunur: **üç süpürge de her zaman
koşar**, hatalar `errors.Join` ile birleşir, başarılı olanların sayısı hata
hâlinde de raporlanır. Bu davranışın testi zaten var ve üçüncü süpürgeyi
kapsayacak şekilde genişler.

### 2. Geçmiş panelleri redaksiyonu tanısın

Üreteç geçmişi `llm_runs`'tan besleniyor ve o tablo 30. günde bizim
süpürgemizle boşalıyor. `HistoryPanel`'de `redacted` geçen sıfır satır var:
bugünkü haliyle, redakte edilmiş bir koşumun boş istemini "kullanıcı bir şey
yazmamış" gibi gösterir. Raporlarda çözdüğümüz ayrımın aynısı.

Bunun için **Task 1'de bilerek ertelenen alan şimdi geliyor.** `RunSummary`'ye
`RedactedAt *time.Time` eklenir ve `ListRuns`'ın SELECT'i `r.redacted_at`
sütununu alır. O sırada gerekçe şuydu: *"alan ve sütun, onu okuyan kodla
birlikte gelir."* Okuyan kod bu.

Frontend'de `src/lib/report.ts`'teki `isRedacted` deseni geçmiş öğelerine
genişler. Üç durum korunur: silinmiş, başlıksız-ama-duruyor, normal.

**Persona konuşmaları redakte edilmediği için** panelde böyle bir duruma
düşmezler — ya vardır ya yoktur. Ayrım yalnızca üreteç koşumları için gerekli.

### 3. Gizlilik metni

`GizlilikView` iki yerde değişir:

- **"Ne saklanıyor"** listesine persona konuşmaları girer: yazdığın mesajlar,
  asistanın yanıtları, ve o yanıtı üretirken toplanan araştırma sonuçları.
- **"Ne kadar süreyle"** iki davranışı ayırır: rapor içeriği boşalır ama
  içeriksiz ölçüm satırı kalır; **konuşma tamamen silinir, geriye hiçbir şey
  kalmaz.**

Metnin hâlihazırdaki Persona uyarısı (canlı araştırma yaptığı ve yazdığın
metnin bir bölümünün arama motoruna gittiği) yerinde kalır — konuşmaların
saklanması o gerçeği değiştirmiyor, üstüne ekliyor.

### 4. Bugünkü main'e karşı doğrulama

Dal 3 gün önce yazıldı ve o zamandan beri `Assessment`/`AssessmentSummary`
modelleri `redacted_at` aldı, `AssessmentStore` arayüzü bir metot kazandı,
`main.go` yeni bir arka plan işçisi kurdu ve `<main>` düzeni iki satırlı bir
flex kolona dönüştü. `git merge-tree` metinsel çakışma bildirmiyor ama
derlendiğini ve testlerin geçtiğini **merge sonrası** görmek gerekiyor;
çakışmasızlık uyumluluk değildir.

`HistoryPanel` AppShell'in yeni düzeninde de görünmeli: `<main>` artık
kaydırılabilir bir satır ve altında `shrink-0` bir alt bilgi taşıyor.

## Kapsam dışı

- **Persona konuşmaları için yeni silme akışı.** Dalda
  `DELETE /decision/conversations/{id}` ve panelde karşılığı zaten var.
- **`llm_runs` için satır içi silme.** `DELETE /llm/runs/{id}` mevcut ve
  satırı gerçekten siliyor; bu iş onu değiştirmiyor.
- **Konuşma dışa aktarma.** Gerçek bir talep gelene kadar yazılmaz.
- **`conversations.verdict` üzerinden herhangi bir toplu ölçüm.** Sütun dalda
  var ama hiçbir şey onu toplamıyor; toplasaydık silme kararı yeniden
  tartışılırdı.

## Test

**Backend**
- `SweepConversations`: yaş sınırının altındaki konuşmaya dokunmaz;
  üstündekini siler; mesajların cascade ile gittiği şemadan doğrulanır (SQL'in
  kendisi bu repoda test edilmiyor — bilinen ve kabul edilen sınır).
- `retention.Sweep`: üç süpürgeyi de aynı cutoff ile çağırır; biri hata
  verdiğinde diğer ikisi yine koşar ve sayıları raporlanır; üç hata da
  `errors.Join` ile korunur. Mevcut iki testin üçlüye genişlemiş hâli.

**Frontend**
- `src/lib/*.test.ts`: geçmiş öğesinin üç durumu — redakte, başlıksız, normal.
- Bileşen testi altyapısı yok ve eklenmiyor; `npm run lint` ve `npm run build`
  ile doğrulanır.

## Bilinen sınır

Persona konuşmaları artık sunucuda duruyor, yani ajanın istemciden transkript
okuması bir tasarım tercihi olarak kalıyor ama veri iki yerde bulunuyor. Bu
dalın kendi notu bunu zaten açıklıyor: yazma hatası 500 değil, yanıt yine
döner ve `conversation_id` boş gelir. Kayıp kayıt, kayıp cevap değildir.
