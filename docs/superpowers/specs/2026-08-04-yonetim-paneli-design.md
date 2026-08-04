# Yönetim paneli — tasarım

**Tarih:** 4 Ağustos 2026
**Durum:** onaylandı, plana hazır
**Dal:** `feat/yonetim-paneli` (main'den)

## Ne yapıyoruz ve neden

Bugün bir yönetim yüzeyi var ama ürünün içine gömülü: [`AdminView.tsx`](../../../mf-frontend/src/components/views/AdminView.tsx)
dört sekmeyle (`Genel`, `Model & Ayarlar`, `MCP`, `Log Monitörü`) ana SPA'nın
`#admin` hash rotasında oturuyor, ürünün header'ını ve alt bilgisini paylaşıyor.
Arkasında [`internal/admin`](../../../mf-backend/internal/admin/) duruyor ve o
taraf sağlam: `RequireAuth` + `RequireRole(admin)` tüm alt ağaçta, `/metrics`
kendi uzak zaman aşımıyla ayrılmış.

Eksik olan üç şey var:

1. **Hesap kavramı yok.** `users` tablosunda `role` var, organizasyon yok.
   Bir şirketi bireysel bir kullanıcıdan ayırt edemiyoruz, bir şirketin
   çalışanlarını bir çatı altında sayamıyoruz.
2. **Sayılar chart değil.** `Overview` `total_users`, `runs_last_24h`, p95 ve
   `schema_valid_rate_24h` döndürüyor; panel bunları düz kutu olarak basıyor.
   Zaman içinde ne olduğu görünmüyor.
3. **Hukuki metinler kodda gömülü.** [`GizlilikView.tsx`](../../../mf-frontend/src/components/views/GizlilikView.tsx)
   ve [`KosullarView.tsx`](../../../mf-frontend/src/components/views/KosullarView.tsx)
   JSX. Bir virgül düzeltmek bir deploy demek, ve kabul kaydının işaret ettiği
   metnin gövdesi hiçbir yerde saklanmıyor — `terms_version` bir Go sabiti
   (`auth.TermsVersion`), gövde ise repo geçmişinde.

Bu iş bu üçünü kapatıyor, ve paneli ürün kabuğundan çıkarıp kendi rotasına
taşıyor.

## Kapsam sınırı: çok kiracılılık bu turda değil

İstenen nihai hâl, şirketin kendi sayfasından üyelerini oluşturup çalışanlarını
takip etmesi. Bu tur onu **kurmuyor**, önünü açıyor.

`user_id` taşıyan dört tablo var: `sessions`, `llm_runs`, `assessments`,
`conversations`. Çok kiracılılık bu dördüne `org_id` ve **her sorguya filtre**
eklemek demek. Filtre unutulan tek sorgu, bir şirketin raporunu başka bir
şirkete gösterir. Bu, panel işinin arasına sıkıştırılacak bir iş değil: ayrı
spec, ayrı plan, sorgu sorgu geçilen ayrı bir test turu.

Bu turda `organizations` tablosu kuruluyor, her kullanıcı bir organizasyona
bağlanıyor, panel şirket hesabı açabiliyor. **Hiçbir sorguya filtre
eklenmiyor** — herkes bugünkü gibi yalnızca kendi verisini görüyor, çünkü
sorgular zaten `user_id` kapsamlı.

**Kaydedilen risk:** filtresiz bir `org_id` sütunu dolu bir silahtır. Şemaya
bakan biri "kapsam org bazlı" varsayıp bir sorguyu ona göre yazabilir. İkinci
turun ilk işi, sıfırdan yazmak değil, bu dört tablonun her sorgusunu tek tek
geçmek olacak. Sütunlara bunu söyleyen bir yorum yazılacak.

## 1. Erişim ve rota

### `/yonetim` — ayrı Next.js rotası

Panel `mf-frontend/src/app/yonetim/` altına kendi `layout.tsx`'i ile taşınıyor.
Ürün kabuğu — header, `StatusRail`, alt bilgi — panelde yok; panel kendi
kabuğunu kuruyor.

Alt rotalar gerçek yollar, hash değil:

```
/yonetim              → Panel (dashboard)
/yonetim/hesaplar     → Hesap listesi ve detayı
/yonetim/belgeler     → Hukuki metin editörü
/yonetim/model        → bugünkü "Model & Ayarlar"
/yonetim/mcp          → bugünkü "MCP Sunucuları"
/yonetim/loglar       → bugünkü "Log Monitörü"
/yonetim/denetim      → denetim kaydı
```

Son üçü taşınıyor, yeniden yazılmıyor: `ModelPanel`, `MCPPanel`, `LogsPanel`
bileşenleri `AdminView.tsx`'ten çıkıp kendi dosyalarına iniyor. `AdminView.tsx`
750 satır ve dört paneli birden taşıyor; bu bölme, işin gerektirdiği bir
temizlik, keyfi bir refactor değil.

### `#admin` ölmüyor, yönleniyor

`MasterView` birliğinden `admin` çıkıyor, ama `#admin` ile gelen bir bağlantı
404 görmüyor: AppShell hash'i tanıyıp `/yonetim`'e `replace` ediyor. Bu reponun
kendi kuralı — `codegen` nav'dan indi, rotası adreslenebilir kaldı
([`AppShell.tsx`](../../../mf-frontend/src/components/AppShell.tsx) `OFF_NAV`
notu). Nav'daki "Yönetim" düğmesi de kalkıyor; yerine, yalnızca admin rolüne
görünen ve `/yonetim`'e giden bir bağlantı geliyor.

`Metrikler` ürün tarafında kalıyor. Prometheus'tan okuyan tek ekran o, ve kutu
kapalıyken susan bir ekranın panelin içinde durması paneli de ölü gösterirdi.

### Oturum: ayrı ekran, ayrı oturum değil

`/yonetim`'e oturumsuz gelen kişi panelin kendi giriş ekranını görüyor. Token
seti **aynı** — `mf_access` / `mf_refresh` ([`api.ts`](../../../mf-frontend/src/lib/api.ts)).
İki ayrı token seti iki ayrı yenileme döngüsü demek, ve 401 sonrası hangisinin
yenileneceği belirsizleşir; bir sekmede yapılan çıkış diğerinde yarım oturum
bırakır.

Ayrı olan, giriş **ekranı** ve kabuk. Kararı rol veriyor:

- Kimlik doğru, `role == admin` → panele girer, deep link korunur.
- Kimlik doğru, `role != admin` → panele giremez, ürüne (`/`) yönlendirilir.
  Ekranda "yönetici değilsin" yazmaz; var olmayan hesabı yanlış parolalıdan
  ayırt etmeyen tek mesaj kuralı burada da geçerli.
- Kimlik yanlış → mevcut auth ne diyorsa o.

**Onay kapısı panelde çalışmıyor.** Ürün tarafında koşulları kabul etmemiş
kullanıcı uygulamayı değil kapıyı görüyor; panelde bu kural geçerli değil.
Sebebi kolaylık değil, kilitlenme: 4. aşamada operatör hukuki metni panelden
düzeltecek, ve düzeltilecek metnin eski hâlini kabul etmeden panele
giremiyorsa metni hiç düzeltemez. Operatör burada hizmeti tüketen taraf değil,
veri sorumlusunun kendisi.

Backend'de rota işi yok. `/admin/*` zaten iki gate'in altında ve yeni uçlar aynı
alt ağaca eklendiği için gate'i miras alıyor — [`routes.go`](../../../mf-backend/internal/admin/routes.go)
bunu bilerek böyle kurmuş.

### Adresi gizlemiyoruz

Tahmin edilemez bir segment (`/yonetim/a7f3c1`) tartışıldı ve **reddedildi**.
Güvenlik eklemiyor: koruma `RequireRole`'da, ve `RequireRole`'un kendi yorumu
zaten "admin yüzeyinin varlığını gizlemek bir şey kazandırmaz, rota adları
zaten her ziyaretçiye gönderiliyor" diyor. Bir kere paylaşılan gizli adresin
gizliliği biter; kalan tek etkisi, panele girmesi gereken kişinin adresi
hatırlayamaması olur.

**TOTP bu turun dışında, bilinçli olarak.** Admin rolü API'den verilmiyor;
sunucunun `ADMIN_EMAIL` değişkeninde adı geçen hesap açılışta yükseltiliyor,
yani ortada tek bir yönetici var. Tek hesap için ikinci faktör doğru bir
yatırım ama acil değil ve bu turu iki katına çıkarır. Yeri, şirket hesapları
gelip yönetici sayısı birden fazla olduğu tur.

## 2. Kabuk ve tasarım dili

Sol sabit sidebar (gruplu, ikonlu), üstte ince bir topbar + breadcrumb, içerik
yoğun ve tablo ağırlıklı. Ürün ekranları "az ve nefes alan"; panel bunun tersi.
SAP/Linear/Stripe hissi bu yoğunluktan gelir, süslemeden değil.

Renk ve tipografi ürünle **aynı** token setinden (`globals.css`), dark-only.
Panel için ayrı tema açılmıyor: iki tema iki bakım yüzeyi, ve panelin ürüne ait
olduğu görünmeli.

Uygulama sırasında `frontend-design` skill'i okunacak; chart'lara başlamadan
önce `dataviz` skill'i okunacak (palet ve eksen kuralları oradan gelir).

## 3. Hesaplar

### Migration 012 — 012 boş, kontrol edildi

Bu depoda migration numarası dallar arasında çakışmış bir kez
([010 tuzağı](2026-08-01-kvkk-ve-veri-silme-design.md)). Tüm yerel dallar
tarandı: en yüksek numara **011** (`011_terms.sql`), hiçbir dalda 012 yok.
Bu iş 012'den başlıyor.

```sql
-- 012_organizations.sql
CREATE TABLE IF NOT EXISTS organizations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'individual',  -- 'individual' | 'company'
    tax_id     TEXT NOT NULL DEFAULT '',
    seat_limit INTEGER NOT NULL DEFAULT 1,
    status     TEXT NOT NULL DEFAULT 'active',      -- 'active' | 'suspended'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- DİKKAT: org_id bu turda hiçbir sorguda filtre olarak KULLANILMIYOR.
-- Kapsam hâlâ user_id. Buraya bakıp "kapsam org bazlı" varsayan bir sorgu
-- yazmak, bir şirketin raporunu başka bir şirkete gösterir.
ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id   UUID REFERENCES organizations(id);
ALTER TABLE users ADD COLUMN IF NOT EXISTS org_role TEXT NOT NULL DEFAULT 'owner';
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT false;

-- Geri doldurma: mevcut her kullanıcı kendi tek kişilik bireysel org'una.
-- org_id NOT NULL yapılmıyor: geri doldurma ile yeni kayıt akışının ikisi de
-- doğru çalıştığı doğrulanana kadar, NULL bir org_id sessiz bir hata değil
-- görünür bir eksiklik olsun.
```

Her kullanıcı tam olarak bir organizasyona ait. Bireysel hesap = tek üyeli
`individual` org. Şirket hesabı = `company` org + `org_role = 'owner'` olan bir
kullanıcı.

### Hesap açma ve geçici parola

E-posta altyapısı yok — site `.vercel.app` üzerinde ve posta alamıyor
([KVKK spec'i](2026-08-01-kvkk-ve-veri-silme-design.md) bunu zaten kaydetmiş).
Yani davet e-postası gönderemiyoruz.

Akış: admin hesabı açar → sunucu `crypto/rand` ile geçici parola üretir →
parola **yanıtta bir kez** döner ve ekranda bir kez gösterilir, bir daha asla →
`users.must_change_password = true`.

**Geçici parolanın sunucu tarafında zorlanması şart, ve sebebi güvenlik
tiyatrosu değil:** admin, açtığı hesabın parolasını biliyor. Zorlama olmadan
admin o hesapla giriş yapıp kullanıcının verisini görebilir — yani aşağıda
gerekçesiyle reddedilen "kullanıcı taklidi" arka kapıdan içeri girer.

Uygulama:

- `accessClaims`'e `pwd_reset bool` alanı, `common.AuthClaims`'e aynısı.
- Yeni middleware `common.RequirePasswordFresh`: `pwd_reset` işaretliyse 403 ve
  `password_change_required` kodu. Ürün alt ağaçlarına takılır; `/auth` alt
  ağacına takılmaz, yoksa parola değiştirmenin kendisi de bloke olur.
- Parola değiştiğinde `must_change_password` düşer; sonraki token temiz çıkar.
- **Yenileme yolu da bayrağı taşır.** `POST /auth/refresh` yeni access token'ı
  kullanıcı satırından üretir; bayrak orada okunmazsa, işaretli bir kullanıcı
  bir kez yenileme yapıp temiz token alır ve kapı tamamen delinir.
- Frontend `password_change_required` kodunu görünce parola değiştirme ekranını
  gösterir — kapı sunucuda, ekran yalnızca ona eşlik ediyor.

### Panel yüzeyi

- **Liste:** arama (ad, e-posta, vergi no), tür ve durum filtresi, sayfalama.
  Sütunlar: hesap adı, tür, üye sayısı, analiz sayısı, son etkinlik, durum.
- **Yeni hesap:** bireysel (ad, e-posta) veya şirket (unvan, vergi no, koltuk
  sayısı, sahip adı + e-postası). Şirket seçilirse org ve sahip kullanıcı tek
  işlemde, tek transaction'da yaratılır.
- **Detay:** üyeler, hesap metadata'sı (kaç analiz, hangi domain, ne zaman),
  açık oturumlar, hesabı askıya alma, hesabı silme.
- **Askıya alma:** `organizations.status = 'suspended'`. Etkisi: üyelerinin
  oturumları iptal edilir ve girişleri reddedilir. Bu tek yer, org durumunun
  bir sorguya girdiği yerdir ve kasıtlıdır.

### Kullanıcı taklidi — yapılmıyor

Destek için cazip ve çoğu SaaS panelinde var. Burada **reddedildi**: satılan
eksen veri egemenliği, ve "operatör istediği hesabın içine girebiliyor" o
iddiayı çürütür. Hesap detayında içerik değil metadata gösterilir — kaç analiz,
ne zaman, hangi domain. Vaka metni ve bulgular panelde hiçbir zaman görünmez.

## 4. Panel (dashboard)

### Tek endpoint

```
GET /admin/stats?window=30d|90d
```

Altı chart için altı istek atmıyoruz. Render'daki bağlantıda her istek ayrı
gecikme, ve altısının tek deadline altında olması gerekiyor — biri zaman aşımına
uğrayıp diğerleri dönerse panel yarısı dolu, yarısı boş bir yalan anlatır.

Rollup tablosu yok; `date_trunc('day', created_at)` ile okuma anında
hesaplanıyor. Bu ölçekte doğru olan bu, ve satırlar milyonu geçtiğinde rollup
eklemek geriye dönük bir iş değil — aynı endpoint, farklı kaynak.

Tüm seriler **Postgres'ten**. Prometheus'a bağlı hiçbir şey bu ekrana
girmiyor: GPU kutusu kapalıyken üye büyümesi grafiğinin boşalması, panelin
tamamını ölü gösterirdi. `Metrikler` ekranının panelden ayrı durmasının sebebi
de bu.

### Gösterilenler

Üstte dört kutu, her birinin yanında önceki pencereye göre değişim yüzdesi:
toplam üye, toplam rapor, son 24 saat, aktif adapter.

| Chart | Ne gösterir | Kaynak |
|---|---|---|
| Üye büyümesi | Gün başına yeni kayıt + kümülatif | `users.created_at` |
| Hesap dağılımı | Bireysel / şirket kırılımı | `organizations.type` |
| Analiz hacmi | Gün başına üretilen rapor | `assessments.created_at` |
| Şema geçerlilik trendi | Günlük oran — adapter değişince düşerse geri alma sinyali | `assessments.schema_valid` |
| Aktivasyon hunisi | Kayıt → onay → ilk analiz | `users` + `assessments` |
| Kohort tutunması | Haftalık kohortun 2. ve 4. haftada analiz üretme oranı | `users` + `assessments` |
| Çalışma hacmi | Gün başına LLM koşumu, `target` kırılımlı | `llm_runs` |

Aktivasyon hunisi ve kohort tutunması, sektörün erken aşama SaaS'ta baktığı iki
sayı: üye sayısı büyürken aktivasyon düşüyorsa büyüme sahtedir. Bu ikisi
panelde durmazsa üye grafiği tek başına yanıltıcıdır.

**MRR, churn, LTV hesaplanamıyor** — faturalama yok. Bunlara panelde yer ayırıp
boş bırakmıyoruz; faturalama geldiğinde eklenir. Boş bir grafik, olmayan bir
ölçümü varmış gibi gösterir.

Chart'lar mevcut [`TimeChart.tsx`](../../../mf-frontend/src/components/ui/TimeChart.tsx)
ailesinden büyütülüyor. Yeni bağımlılık gelmiyor: bu repo chart'ı zaten elle
çiziyor ve `package.json`'da üç üretim bağımlılığı var.

### Tutarlılık kartı

`POST /analysis/trial` ve `PerCriterionStdDev` zaten yazılmış; eksik olan,
[`urun-ve-pazarlama.md`](../../urun-ve-pazarlama.md) §7'nin 3. önceliği olan
**yayınlanmış bir sayı**. Panelde bir kart: örnek vaka üzerindeki son trial
grubunun spread'i ve en oynak kriter.

Bu kart panelde durduğu için her adapter değişiminden sonra yeniden ölçmek
doğal hale gelir — satış materyaline giden sayının bayatlamamasının tek pratik
yolu bu.

**Kapsam kesilirse ilk bu gider.** Diğer her şey panelin kendisi; bu, panelin
üstüne konan bir ürün kararı.

## 5. Belgeler (KVKK / gizlilik / koşullar)

### Migration 013

```sql
CREATE TABLE IF NOT EXISTS legal_documents (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug               TEXT NOT NULL,
    title              TEXT NOT NULL,
    version            TEXT NOT NULL,
    body               TEXT NOT NULL,
    requires_reconsent BOOLEAN NOT NULL DEFAULT false,
    is_draft           BOOLEAN NOT NULL DEFAULT true,
    published_at       TIMESTAMPTZ,
    published_by       UUID REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_legal_slug_published
  ON legal_documents (slug, published_at DESC) WHERE is_draft = false;
```

**Append-only.** Her yayın yeni bir satır; eski satır güncellenmiyor. Sebep
hukuki: "bu kullanıcı tam olarak neyi kabul etti" sorusunun cevabı, altı ay
sonra da metnin gövdesiyle birlikte elde olmalı. Bir tabloda tek satır tutup
üzerine yazmak, geçmiş her kabulü sessizce yeni metnin kabulüne çevirir —
[`011_terms.sql`](../../../mf-backend/migrations/011_terms.sql) tam olarak bu
yüzden versiyonu ayrı sütunda tutuyor.

Taslak da aynı tabloda (`is_draft`), ayrı tabloda değil: taslak ile yayın aynı
şeyin iki hâli, ve iki tablo iki şema bakımı demek.

Başlangıç seed'i, bugünkü `GizlilikView` ve `KosullarView` metinlerini
`gizlilik` ve `kosullar` slug'larıyla, mevcut `auth.TermsVersion` (`2026-08-01`)
versiyonuyla yayınlanmış olarak yazar. **Seed olmadan deploy, gizlilik
sayfasını boşaltır.**

### API

```
GET    /legal/{slug}                    public, auth yok
GET    /admin/legal                     belge listesi (taslaklar dahil)
GET    /admin/legal/{slug}              yayın geçmişi + açık taslak
PUT    /admin/legal/{slug}              taslak kaydet
POST   /admin/legal/{slug}/publish      yayınla
DELETE /admin/legal/{slug}/draft        taslağı at
```

`GET /legal/{slug}` public grubuna monte edilir (`/auth` ile aynı yere): giriş
ekranı, kaydolmamış bir ziyaretçiye gizlilik metnini gösterebilmeli.

`publish` gövdesi tek anlamlı alan taşır: `requires_reconsent`. İşaretliyse yeni
bir `version` üretilir (`YYYY-MM-DD` + gerekirse sıra eki) ve herkes onay
kapısına döner. İşaretli değilse gövde yayınlanır, `version` önceki yayınla
aynı kalır, kimse rahatsız edilmez.

Bu ayrım kodda zaten yazılıydı — [`auth/models.go`](../../../mf-backend/internal/auth/models.go)
`TermsVersion` yorumu *"bir insanın yeniden okumak isteyeceği kadar
değiştiyse bump et, yazım hatası için değil"* diyor. Metni düzenleyen kişi artık
kodu görmediği için o cümlenin panelde bir kutu olarak durması gerekiyor.

### Onay kapısı

`auth.TermsVersion` sabiti **kalkıyor**. Yerine: "yeniden onay gerektiren en son
yayının versiyonu", `kosullar` slug'ından **her sorulduğunda okunur**. Kapı,
kullanıcının `terms_version` değeri buna eşit değilse kapalıdır.

Önbellek yok, bilerek. Bu değer yalnızca girişte ve `/auth/me` çağrısında
okunuyor — sıcak bir yol değil. Süreç ömrü boyunca önbelleklemek, Render
birden fazla instance çalıştırdığı anda yanlış olurdu: bir instance'ta yapılan
yayın diğerinin önbelleğini geçersizleştiremez, ve sonuç, kullanıcının hangi
sunucuya düştüğüne göre onay kapısı görüp görmemesi olur.

Bugünkü kapı `terms_accepted_at IS NULL`'a bakıyor
([`terms.ts`](../../../mf-frontend/src/lib/terms.ts)); yeni kapı versiyon
karşılaştırması. Hiç kabul etmemiş kullanıcı da doğal olarak kapalı kalır.

### Editör

Panelde yan yana editör ve önizleme. Önizleme, ürünün bastığı ile **aynı**
[`RichText.tsx`](../../../mf-frontend/src/components/ui/RichText.tsx) ile
render edilir — yayınlayınca göreceği şeyin aynısı. Bu bileşen ham HTML kabul
etmeyen, el yazımı bir Markdown alt kümesi; hukuki metnin bir enjeksiyon
yüzeyine dönüşmemesi için doğru olan da bu.

`GizlilikView` ve `KosullarView` gövdeyi `GET /legal/{slug}`'dan okuyup
`RichText`'e verir. İçerik repodan çıkar.

## 6. Denetim kaydı

### Migration 014

```sql
CREATE TABLE IF NOT EXISTS audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,     -- 'account.create', 'legal.publish', ...
    target     TEXT NOT NULL DEFAULT '',
    detail     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log (created_at DESC);
```

Kim hangi ayarı değiştirdi, hangi hesabı açtı, hangi belgeyi yayınladı.
Yazanlar: hesap açma/askıya alma/silme, belge yayınlama, ayar değişikliği,
adapter aktivasyonu.

`actor_id` `ON DELETE SET NULL`: aktörün hesabı silinse bile kaydın kendisi
kalmalı — silinen bir hesabın yaptığı iş, kaydın en çok gerektiği durumdur.

`detail` içine **kişisel veri veya vaka metni yazılmaz**. Neyin değiştiği yazılır
(alan adları, eski/yeni değer için yalnızca ayar tipi alanlar), içerik değil.
Aksi hâlde denetim kaydı, redaksiyon süpürgesinin
([`internal/retention`](../../../mf-backend/internal/retention/)) ulaşamadığı
bir kişisel veri deposuna dönüşür.

Şimdi ekleniyor çünkü geriye dönük yazılamaz: kaydedilmemiş bir değişiklik
kaybolmuştur. Şirket hesapları geldiğinde zaten zorunlu hale gelecek.

## 7. Veri saklama ve hesap silme

[`internal/retention`](../../../mf-backend/internal/retention/) zaten var: 30
günlük süpürge, `RETENTION_DAYS` ile ayarlı, `0` kapatıyor. Panelde görünmüyor.

- **Panelde saklama süresi:** ayar `settings` satırına taşınır ve panelden
  değiştirilir. `RETENTION_DAYS` ortam değişkeni yalnızca **ilk açılışta**, satır
  boşken varsayılanı verir; sonrasında doğruluk kaynağı veritabanıdır. İki ayrı
  kaynak bırakmak, süpürgenin beyan edilen süreden farklı bir süreyle koşmasına
  ve KVKK metnindeki sayının yalan olmasına yol açardı. `0` yine kapatır ve
  açılışta uyarı basar.
- **Hesabı ve verisini sil:** hesap detayında, onay adımıyla. `users` üzerindeki
  `ON DELETE CASCADE` zaten her şeyi götürüyor. KVKK spec'i bunu "ayrı bir iş"
  diye kapsam dışı bırakmıştı; burası o ayrı iş.

Silme, KVKK metninde beyan edilen silme hakkını operatör için tıklanabilir
yapar. Veri egemenliği satan bir üründe satışta sorulan ilk üç sorudan biri bu.

## 8. Uygulama sırası

Bu spec tek bir plana sığmayacak kadar geniş — beş ayrı yüzeye dokunuyor. Plan
aşağıdaki sırayla yazılacak ve her aşama kendi başına sevk edilebilir olacak.
Sıra keyfi değil: her aşama bir öncekinin ürettiği yüzeyi kullanıyor.

1. **Kabuk.** `/yonetim` rotası, giriş ekranı, sidebar, `#admin` yönlendirmesi,
   mevcut dört panelin taşınması ve `AdminView.tsx`'in bölünmesi. Yeni özellik
   yok — bittiğinde bugünkü panel yeni evinde çalışıyor olmalı. Bu aşama tek
   başına sevk edilebilir ve geri kalanı bekletmez.
2. **Hesaplar.** Migration 012, org modeli, hesap listesi/detayı, hesap açma,
   geçici parola akışı, askıya alma.
3. **Panel.** `GET /admin/stats`, chart'lar, kutular. Hesap dağılımı grafiği
   012'ye bağlı olduğu için hesaplardan sonra.
4. **Belgeler.** Migration 013, seed, public uç, editör, onay kapısının
   versiyona bağlanması.
5. **Denetim ve saklama.** Migration 014, denetim kaydı yüzeyi, saklama süresi
   ayarı, hesap silme. Denetim kaydı 2 ve 4'teki aksiyonları yazacağı için
   sonda; kod tarafında yazma çağrıları o aşamalarda eklenir, yüzey burada.

Tutarlılık kartı (§4) 3. aşamanın sonuna eklenir ve kapsam kesilirse ilk o
düşer.

## 9. Test

Mevcut kalıplar izlenir.

**Backend** — store arayüzleri tüketici tarafında tanımlı, handler'lar canlı
Postgres olmadan sahte store ile test edilir
([`handler_swap_test.go`](../../../mf-backend/internal/admin/handler_swap_test.go)
deseni):

- Hesap açma: şirket seçilince org + sahip aynı transaction'da; biri başarısız
  olursa ikisi de yazılmaz. Geçici parola yanıtta bir kez döner ve hash'lenmiş
  saklanır.
- `pwd_reset` claim'i: işaretli token ürün uçlarında 403 alır, `/auth` uçlarında
  almaz. Parola değişince bayrak düşer.
- Askıya alınmış org'un üyesi giriş yapamaz; oturumları iptal edilir.
- `publish`: `requires_reconsent` işaretliyse yeni versiyon, değilse aynı
  versiyon. Yayın append-only — eski satır değişmez.
- Onay kapısı: kullanıcının `terms_version`'ı son "reconsent" versiyonuna eşit
  değilse kapalı; eşitse açık; hiç kabul etmemiş kullanıcı kapalı.
- `GET /legal/{slug}` auth'suz 200 döner; taslak satırı **döndürmez**.
- `/admin/stats`: boş veritabanında sıfırlarla döner, çökmez; pencere sınırı
  dışındaki satırı saymaz.
- Denetim kaydı: hesap açma ve belge yayınlama birer satır yazar.
- Migration testi: `migrations_test.go` altında, mevcut kullanıcıların geri
  doldurulmuş `org_id`'si dolu.

**Frontend** — saf mantık `node --test` ile (`src/lib/*.test.ts`):

- Versiyon karşılaştırma: hangi durumda kapı kapalı.
- Chart kova hesabı: günlük kovalar, boş günler sıfırla dolar, kümülatif seri
  monoton.
- Huni oranları: payda sıfırken NaN değil sıfır.
- Rota koruması: admin olmayan kullanıcı için panel yolu ürüne yönlenir.

## 10. Deploy

1. aşama (kabuk) yalnızca frontend'dir ve hiçbir yeni uç okumaz; tek başına
sevk edilir. Sonraki her aşama için:

**Backend önce.** Frontend `GET /legal/{slug}` ve `GET /admin/stats`'ı okuyacak;
o uçlar yokken frontend deploy edilirse gizlilik sayfası ve panel boş açılır.
Ters sırada bir pencere yok: yeni uçlar eski frontend'i bozmuyor.

Migration seed'i (013) backend deploy'unun parçası, yani metinler ilk backend
deploy'unda yerine oturur.

Ve: **push yarım sürümdür.** `render.yaml` `autoDeploy: true` diyor ama GitHub
webhook'u pratikte ateşlemiyor. Deploy elle tetiklenip doğrulanacak, ve
doğrulanmadan hiçbir şey "çıktı" diye raporlanmayacak.

## 11. Bilinçli olarak yapılmayanlar

- **Çok kiracılı sorgu filtresi.** Ayrı tur, ayrı spec. Gerekçe §"Kapsam sınırı".
- **Kullanıcı taklidi.** Veri egemenliği iddiasıyla çelişir. Gerekçe §3.
- **TOTP / IP kısıtı.** Tek yönetici varken erken. Gerekçe §1.
- **Gizli panel adresi.** Güvenlik eklemiyor, erişilebilirlik götürüyor.
- **MRR / churn / LTV kutuları.** Faturalama yok; boş kutu olmayan ölçümü
  varmış gibi gösterir.
- **E-posta daveti / şifre sıfırlama postası.** Posta altyapısı yok; geçici
  parola akışı bunun yerine geçiyor ve posta geldiğinde değiştirilecek.
- **Chart kütüphanesi.** `TimeChart` var; üç üretim bağımlılığı olan bir
  frontend'e dördüncüsü chart için gelmez.
- **Rollup tabloları.** Bu ölçekte `date_trunc` yeterli; sonradan eklemek
  endpoint'i değiştirmiyor.
