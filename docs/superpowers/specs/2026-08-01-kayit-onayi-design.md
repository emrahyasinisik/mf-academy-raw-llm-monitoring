# Kayıtta KVKK ve kullanım sözleşmesi onayı — tasarım

**Tarih:** 1 Ağustos 2026
**Durum:** onaylandı, plana hazır

## Ne yapıyoruz ve neden

Kaydolan kişi hiçbir şey kabul etmiyor. Aydınlatma metni `#gizlilik`'te
duruyor ve giriş ekranından bağlantısı var, ama okunduğuna dair bir kayıt yok
ve ortada bir **kullanım sözleşmesi hiç yok** — yani hizmetin hangi şartlarla
verildiği hiçbir yerde yazılı değil.

Bunun iki ayrı sonucu var ve karıştırılmamalı:

- **Kullanım sözleşmesinin kabulü sözleşmenin kurulmasıdır.** Kişisel verinin
  işlenmesi buna dayanıyor: kullanıcı kaydolup vaka metnini yapıştırıyor, o
  metni işlemek hizmetin ifası. Sözleşme yoksa dayanak da yazılı değil.
- **Aydınlatma metninin okunduğunun teyidi rıza değildir.** Onu bir rıza
  kutusu gibi kurmak, dayanmadığımız bir hukuki sebebe dayanıyormuş gibi
  görünmek olurdu — ve açık rıza her an geri alınabildiği için bu konumu
  güçlendirmez, zayıflatır.

Kurulacak şey bu ikisi: sözleşmenin kabulü, ve metnin okunduğunun teyidi.

## Verilen kararlar

1. **Sözleşme metnini biz yazıyoruz**, demo seviyesinde ve ürünün gerçekte ne
   yaptığından türetilmiş. Hukukçu onayından geçmediği metinde açıkça yazılı
   olacak.
2. **Yeni kayıtlarda zorunlu onay, mevcut kullanıcılara girişte kapı.**
   Kabul etmemiş bir kullanıcı giriş yaptığında önce kabul ekranını görür.
   Böylece kayıt herkes için oluşur.

## Veri katmanı

**Migration 011** (010 KVKK saklama işinde kullanıldı):

```sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_version     TEXT NOT NULL DEFAULT '';
```

**Sürüm alanı tek sürümde bile gerekli.** Kaydın varlık sebebi "ne zaman kabul
etti" değil, "**neyi** kabul etti" sorusuna cevap verebilmek. Zaman damgası tek
başına o soruyu cevaplayamaz, ve metin değiştiğinde geçmiş kabuller sessizce
yeni metne ait görünmeye başlar.

Sürüm tek bir yerde sabit olarak durur (Go tarafında `auth.TermsVersion`) ve
frontend aynı değeri gösterir. Şu anki değer: `2026-08-01`.

## API

- **`RegisterRequest`** bir `accepted_terms bool` alanı alır. `false` ya da
  eksikse **400**. Sunucunun varsaydığı bir kabul, kabul değildir — bu yüzden
  sessiz varsayılan yok, ve alan `omitempty` ile serileştirilmez.
- **`POST /auth/accept-terms`** — kimlik doğrulamalı, gövdesiz. Çağıran
  kullanıcının `terms_accepted_at`'ini `now()`, `terms_version`'ını geçerli
  sürüme yazar. Idempotent: zaten kabul etmiş kullanıcı için **204** döner ve
  ilk kabul tarihini **değiştirmez** — kabulün tarihi kaydın kendisidir.
- **`User`** struct'ına `TermsAcceptedAt *time.Time` eklenir (json
  `terms_accepted_at`). `GET /auth/me` bunu zaten döndürecek, yani kapının
  gerekip gerekmediğini öğrenmek için yeni bir uç noktaya gerek yok.

## Frontend

### Kayıt formu

`AuthView`'ın kayıt kolunda zorunlu bir onay kutusu:

> Kullanım koşullarını kabul ediyorum ve aydınlatma metnini okudum.

İki belgeye de bağlantı verir. Kutu işaretlenmeden gönderim düğmesi kapalı, ve
istek `accepted_terms: true` taşır. Kutunun kendisi tek bir cümledir; iki ayrı
kutu kurmuyoruz çünkü ikisi de aynı anda ve aynı işlem için gerekli.

### Kapı

`AppShell`, `user.terms_accepted_at` boşsa uygulamayı değil kabul ekranını
render eder. Bu, `AuthView` için zaten var olan dallanmanın aynısı: oturum
yoksa `AuthView`, kabul yoksa kabul ekranı, ikisi de varsa uygulama.

Kabul ekranı iki belgeyi de okunabilir şekilde gösterir, tek düğmesi
`POST /auth/accept-terms` çağırır ve dönünce kullanıcıyı yeniler.

**Çıkış yapabilmeli.** Kabul etmek istemeyen birinin tek seçeneği kabul etmek
olmamalı; ekranda oturumu kapatma yolu bulunur.

### `#kosullar` rotası

`#gizlilik` ile aynı desen: `MasterView` birliğine girer, `NAV`'a **girmez**
(nav çalışma araçları için, bu bir belge), `OFF_NAV` listesiyle tanınır. Alt
bilgide ve kayıt formunda bağlantısı olur.

## Sözleşme metni neyi anlatacak

Ürünün gerçekte yaptığından türetilmiş, kısa ve somut:

1. **Hizmet nedir** — bir vaka metni girilir, rubrik-puanlı bir rapor üretilir.
2. **Rapor ne değildir** — yatırım tavsiyesi değildir, kararı vermez ve
   vermiş sayılmaz. Değerlendirmeyi yapan kişi kararın sahibidir. Bu, ürünün
   konumlandırmasıyla aynı cümledir ve pazarlama metniyle çelişmemelidir.
3. **Garanti verilmez** — doğruluk, kesintisizlik ve hizmet seviyesi
   taahhüdü yok. Demo olduğu açıkça yazılır.
4. **Yapıştırılan içerikten kullanıcı sorumludur** — üçüncü kişilere ait
   veriler dahil. Başkasının şirket belgesini yapıştıran kişi o veriyi
   yapıştırmaya yetkili olduğunu beyan etmiş olur.
5. **Veri işleme** — burada tekrarlanmaz, aydınlatma metnine yönlendirilir.
   İki belge birbiriyle çelişmemeli; saklama süresi yalnızca bir yerde yazılı
   olur.
6. **Bu metin hukukçu onayından geçmemiştir** — demo için yazılmıştır ve
   gerçek müşteri verisiyle kullanılmadan önce gözden geçirilmelidir.

## Test

**Backend**
- `Register`: `accepted_terms` yokken veya `false` iken 400; `true` iken
  kullanıcı oluşur ve `terms_accepted_at` ile `terms_version` dolar.
- `AcceptTerms`: kimliksiz istekte 401; ilk çağrıda kaydı yazar; ikinci
  çağrıda 204 döner ve **ilk tarihi değiştirmez**.
- Store testleri yok — bu repoda veritabanı destekli test yok ve bu iş bir
  tane icat etmiyor. Handler'lar sahte store ile test edilir, mevcut desen bu.

**Frontend**
- `src/lib/*.test.ts`: kapının gerekip gerekmediğine karar veren saf
  fonksiyon — `terms_accepted_at` boşsa kapı, doluysa uygulama.
- Bileşen testi altyapısı yok ve eklenmiyor; `npm run lint` ve `npm run build`.

## Kapsam dışı

- **Sürüm değişince yeniden kabul ettirme akışı.** Alan ileriye dönük duruyor
  ama akış kurulmuyor; şu an tek sürüm var ve olmayan bir sürüm geçişi için
  makine yazmak YAGNI.
- **Başvuru kanalı.** Hâlâ yok ve hâlâ bilerek ertelenmiş durumda; bu iş onu
  değiştirmiyor.
- **Hesap silme akışı.** `users` üzerindeki `ON DELETE CASCADE` her şeyi
  götürüyor ama bir düğmeye bağlı değil.

## Bilinen sınır

Bu metinler hukukçu tarafından yazılmadı. Demo, ürünü denemek isteyen
kişilerle sınırlı kaldığı sürece savunulabilir; gerçek müşteri verisi girdiği
anda hem sözleşme hem aydınlatma metni gözden geçirilmeli, ve başvuru kanalı
açılmalı. Bu sınır metinlerin kendisinde de yazılı olacak — okuyanın bilmediği
bir sınır, sınır değildir.
