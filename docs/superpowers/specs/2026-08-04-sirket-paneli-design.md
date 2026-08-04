# Şirket paneli — tasarım

**Tarih:** 4 Ağustos 2026
**Durum:** onaylandı, plana hazır
**Dal:** `docs/sirket-paneli` (origin/main'den)

## Ne yapıyoruz ve neden

[`yonetim-paneli` tasarımı](2026-08-04-yonetim-paneli-design.md) platform
operatörüne bir kontrol yüzeyi verdi: hesap açma, hukuki metin, adapter,
denetim. O panel **platform admin** içindir (`users.role = admin`),
`/yonetim` altında yaşar, `/admin/*` API'sini konuşur.

Şirket hesabı açılabiliyor (`organizations.type = 'company'` + sahip), ama
şirketin kendi yöneticisinin bakacağı bir yüzey yok. Owner, çalışanlarını
görmek, koltuk sınırı içinde üye eklemek veya "bu ay kaç analiz üretildi"
sormak için platform paneline sızamaz — ve sızmamalıdır. `/admin` ve
`/yonetim` operatör araçlarıdır; müşteriye açılmaz.

Bu iş o boşluğu kapatır: şirket yöneticisine **ayrı** bir kabuk (`/sirket`)
ve **ayrı** bir API (`/org/*`) verir. Platform paneli yerinde kalır; şirket
paneli onun yerine geçmez, onun altında yaşamaz, ürünün bir sekmesi olmaz.

## Platform vs şirket

| | Platform (`/yonetim`) | Şirket (`/sirket`) |
|---|---|---|
| Kim | `users.role = admin` | `org_role ∈ {owner, admin}` **ve** `organizations.type = company` |
| Ne görür | Tüm org'lar, hukuki metin, adapter, MCP, log, denetim | Yalnızca **kendi** org'u |
| API | `/admin/*` | `/org/*` |
| Şirket yaratır mı | Evet — Hesaplar | Hayır |
| Üye yönetir mi | Hesap detayında metadata; üye oluşturmaz (MVP) | Evet — geçici parola ile |
| Vaka metni | Yok | Yok |

İki yüzey aynı görsel dili paylaşır (sidebar, topbar, dark token seti) ama
kapıları, rotaları ve veri kapsamı ayrıdır. Birinin bozulması diğerini
açmaz.

## Hedefler

1. Şirket owner/admin'i kendi ekibini görür, üye ekler, rol değiştirir, üye
   çıkarır — platform operatörüne başvurmadan.
2. Aynı kişi org genelinde kullanım ve aktivite metadata'sını görür:
   kim ne zaman analiz üretti, hacim, şema uyumu — vaka metni veya transcript
   olmadan.
3. Yetki her `/org/*` çağrısında aktörün `org_id`'sine kilitlenir; çapraz-org
   erişim testle reddedilir.
4. Bireysel (`type = individual`) org'lar bu yüzeyi hiç görmez.

## Hedef olmayanlar (MVP)

- **Kota / kullanım limiti zorlaması.** `seat_limit` üye eklerken sayılır;
  analiz/LLM kotası, faturalama eşiği, "bu ay X kaldı" uyarısı yok.
- **E-posta daveti.** Posta altyapısı hâlâ yok
  ([KVKK spec](2026-08-01-kvkk-ve-veri-silme-design.md), yönetim paneli §3).
  Üye ekleme = geçici parola, ekranda bir kez.
- **Şirket yaratma.** Yalnızca platform admin, `/yonetim/hesaplar` üzerinden.
  Şirket paneli mevcut bir company org'u yönetir.
- **Platform admin taklidi / "şu hesabın yerine bak".** Veri egemenliği
  iddiasıyla çelişir; yönetim panelinde de reddedildi, burada da.
- **Vaka metni, rapor gövdesi, konuşma/transcript.** Metadata only.
- **`/admin/*` uçlarını org admin'e açmak.** Ayrı paket, ayrı gate.
- **`/yonetim` altında scoped bir bölüm** veya ürün içi sekme. Yerleşim
  kararı kilitli: ayrı `/sirket`.
- **TOTP, IP kısıtı, gizli rota adı.** Yönetim paneliyle aynı gerekçe.
- **MRR / churn / LTV.** Faturalama yok.
- **Çoklu org üyeliği.** Bir kullanıcı hâlâ tam olarak bir org'a aittir
  (012 modeli).

## 1. Rol modeli

Üç katman, birbirine karışmaz:

### Platform admin

- `users.role = admin` (bootstrap: `ADMIN_EMAIL`).
- `/yonetim` + `/admin/*`.
- Şirket yaratır: org + owner tek transaction
  ([`CreateCompany`](../../../mf-backend/internal/admin/accounts_store.go)).
- `/sirket`'e **org kimliğiyle** girmez. Platform admin'in kendi satırı
  bireysel org ise zaten kapı kapalıdır; company org'a owner olarak
  bağlanmış olsa bile bu turda platform paneli yeterli — şirket paneli
  müşteri yüzeyi.

### Org admin (şirket yöneticisi)

- `organizations.type = 'company'` **ve** `users.org_role ∈ {'owner', 'admin'}`.
- `/sirket` + `/org/*`.
- Ekip yönetimi, kullanım, aktivite.
- Owner ile admin aynı yetkiye sahiptir (MVP). Ayrım ileride
  (ör. owner seat_limit değiştirebilir, admin değiştiremez) için rol
  alanı durur; şimdi ikisi de tam org-admin.

### Member

- Aynı company org'da `org_role = 'member'`.
- Ürünü kullanır (`/`), `/sirket`'e giremez, `/org/*` 403.
- Kendi verisini bugünkü gibi `user_id` kapsamıyla görür.

### Bireysel org

- `type = 'individual'`. Owner tek kişidir.
- `/sirket` kapalı — tek kişilik "ekip paneli" ürün değildir, gürültüdür.
- OrgGate org tipini de kontrol eder; yalnızca role bakmak yetmez.

```
OrgGate geçer ⇔
  authenticated
  ∧ org_id IS NOT NULL
  ∧ org.type = 'company'
  ∧ org_role ∈ {owner, admin}
  ∧ org.status = 'active'
```

Askıya alınmış org (`status = 'suspended'`) zaten girişte reddediliyor
(yönetim paneli §3); OrgGate aynı varsayımı tekrarlar — askıya alma ile
panel arasında bir pencere kalmasın.

## 2. Erişim ve rota

### `/sirket` — ayrı Next.js rotası

`mf-frontend/src/app/sirket/` kendi `layout.tsx`'i ile. Ürün kabuğu
(header, StatusRail, alt bilgi) yok. Yönetim panelindeki
[`PanelShell`](../../../mf-frontend/src/components/yonetim/PanelShell.tsx)
görsel dilini paylaşır — aynı token'lar, sidebar + topbar — ama **ayrı
bileşen / ayrı nav listesi** (`OrgShell`, `orgNav`). `PanelShell`'i
doğrudan import edip "Yönetim" etiketini değiştirmek, iki paneli tek
bakım noktasına bağlar ve yanlış nav sızdırır; kopyalanan dil, paylaşılan
dosya değil.

Alt rotalar gerçek yollar:

```
/sirket              → özet (kutular + kısa aktivite)
/sirket/ekip         → üye listesi, ekleme, rol, çıkarma
/sirket/kullanim     → kullanım metrikleri (chart'lar)
/sirket/aktivite     → son olaylar (metadata feed)
```

### Oturum: ayrı ekran, ayrı oturum değil

Token seti aynı: `mf_access` / `mf_refresh`. `/sirket`'e oturumsuz gelen
kişi panelin kendi giriş ekranını görür; başarılı girişte OrgGate
karar verir:

- Kapı açık → panele, deep link korunur.
- Kapı kapalı (member, individual, platform-only, yanlış kimlik) → ürüne
  (`/`) yönlendirilir. Ekranda "yetkin yok" yazmaz; yönetim paneliyle
  aynı tek-mesaj kuralı.

Onay kapısı (`terms`) ürün tarafında kalır. Org admin de hizmeti tüketen
taraftır; koşulları kabul etmeden ürünü kullanamaz. Panelde ayrı bir
istisna yok — operatörün hukuki metni düzeltmesi gereken kilitlenme
senaryosu burada yok.

### Nav'dan görünürlük

Ürün header'ında, OrgGate'i geçen kullanıcıya `/sirket` bağlantısı.
Member ve bireysel hesap bunu görmez. Platform admin'in `/yonetim`
bağlantısı ayrı kalır; ikisi birbirinin yerine geçmez.

### Adresi gizlemiyoruz

`/sirket` tahmin edilebilir. Koruma OrgGate + `/org/*` authz'de.
Yönetim paneliyle aynı gerekçe.

## 3. Claims ve authz

Bugünkü `AuthClaims` (`UserID`, `Email`, `Role`, `PasswordReset`) org
bilgisi taşımıyor. `/org/*` her istekte DB'ye gidip org'u çözmek zorunda
kalırsa hem yavaşlar hem de "unutulan JOIN" riski artar.

Access token'a (ve refresh yolunun ürettiği yeni access'e) eklenir:

- `org_id` (string, boş olabilir — olmamalı ama NULL geçmişi 012'de var)
- `org_role` (`owner` | `admin` | `member`)
- `org_type` (`individual` | `company`) — gate'in tip kontrolü için

`RequireOrgAdmin` middleware'i claims üzerinden karar verir; handler'lar
**asla** istek gövdesinden veya path'ten `org_id` almaz. Kapsam =
`claims.OrgID`. Path'te `/org/{orgId}/...` yok; `/org/members` yeter.

Rol veya org değişince (üye çıkarma, rol düşürme, askıya alma) mevcut
access token bayat kalabilir. Access kısa ömürlüdür; kritik yazma
yollarında handler claims'teki role güvenmekle yetinmeyip satırı
yeniden okuyabilir — en azından `RemoveMember` / `SetRole` için.
Refresh, satırdan güncel `org_role` / `org_type` üretir.

`pwd_reset` / `RequirePasswordFresh` ürün ve `/org` alt ağaçlarında
geçerlidir; `/auth` dışında. Geçici parolalı yeni üye önce parolasını
değiştirir, sonra panele veya ürüne girer.

## 4. Ekranlar

### `/sirket` — özet

Üstte kutular: üye sayısı / koltuk, son 24s analiz, pencere içi analiz
toplamı, şema uyum oranı. Altında kısa aktivite listesi (son N olay) ve
"ekibe git" / "kullanıma git" bağlantıları.

Yoğun, tablo/chart ağırlıklı — ürün ekranlarının tersi, yönetim paneliyle
aynı his.

### `/sirket/ekip`

- Liste: ad, e-posta, rol, kayıt tarihi, son aktivite (metadata), durum.
- **Üye ekle:** ad + e-posta + rol (`admin` | `member`). Owner rolü
  oluşturma yok — tek owner, şirket yaratılırken atanır; transfer bu
  turun dışı.
- Yanıtta geçici parola **bir kez**; `must_change_password = true`.
- Rol değiştir: admin ↔ member. Owner'ın rolü değişmez; son owner'ı
  düşürmek veya çıkarmak reddedilir.
- Üye çıkar: satır silinmezse soft? **Hard delete kullanıcı satırı** —
  `ON DELETE CASCADE` zaten user'a bağlı veriyi götürür (retention /
  hesap silme ile aynı eksen). Onay adımı zorunlu. Owner çıkarılamaz.
- Koltuk: `member_count >= seat_limit` iken ekleme 409. `seat_limit`
  şirket panelinden **değişmez** — platform Hesaplar'da kalır (kota
  yönetimi MVP dışı; limit yalnızca tavan).

### `/sirket/kullanim`

Pencere seçici: `30d` | `90d`. Chart'lar Postgres'ten; Prometheus yok —
GPU kutusu kapalıyken şirket panelinin boşalması müşteriye "sizin
hizmet öldü" der, oysa ölü olan operatör altyapısıdır.

### `/sirket/aktivite`

Zaman tersine olay listesi: üye katıldı, analiz tamamlandı, şema
geçersiz, giriş (session metadata). Vaka başlığı / prompt / bulgu yok.
Satır tıklanınca üründeki rapora derin link **yok** — org admin başka
üyenin raporunu açmasın diye bilinçli; metadata yeterli.

## 5. Metrik envanteri

Tek endpoint, tek deadline — yönetim paneli §4 ile aynı gerekçe:

```
GET /org/stats?window=30d|90d
```

Tüm seriler aktörün `org_id`'sine bağlı üyelerin `user_id`'leri üzerinden.
Ham SQL örneği düşüncesi: `users.org_id = $actor_org` → o id'lerle
`assessments` / `llm_runs` / `sessions`.

| Gösterge | Ne | Kaynak |
|---|---|---|
| Üye sayısı / koltuk | anlık | `users` + `organizations.seat_limit` |
| Analiz hacmi | gün başına rapor | `assessments.created_at` (org üyeleri) |
| Şema uyum trendi | günlük oran | `assessments.schema_valid` |
| Çalışma hacmi | gün başına LLM koşumu, `target` kırılımlı | `llm_runs` |
| Üye aktivitesi | üye başına son analiz zamanı + sayım | `users` ⋈ `assessments` |
| Son 24s | kutu | `assessments` / `llm_runs` |

**Yok:** MRR, kota tüketimi, GPU/Prometheus, diğer org'ların sayıları,
vaka içeriği, kriter skorları (skor da içerik sayılır — yalnızca
`schema_valid` ve sayım).

Rollup tablosu yok; `date_trunc` ile okuma anında. Chart'lar mevcut
`TimeChart` ailesinden.

`GET /org/activity?limit=&before=` aktivite feed'i için ayrı, hafif
endpoint — stats chart'larını kirletmemek için.

## 6. API taslağı — `/org/*`

Yeni paket: `mf-backend/internal/org` (veya `internal/company`). 
`internal/admin` altına gömülmez: farklı gate, farklı tüketici, yanlış
import'u zorlaştırmak için fiziksel ayrım.

```
GET    /org/me                     oturum + org özeti (ad, tip, koltuk, rol)
GET    /org/stats?window=30d|90d   kullanım serileri
GET    /org/activity               aktivite feed (metadata)
GET    /org/members                üye listesi
POST   /org/members                üye oluştur (geçici parola yanıtta bir kez)
PATCH  /org/members/{id}           rol değiştir (admin|member)
DELETE /org/members/{id}           üyeyi çıkar
```

Mount:

```
r.Route("/org", func(r chi.Router) {
    r.Use(RequireAuth, RequirePasswordFresh, RequireOrgAdmin, Timeout(...))
    // handlers
})
```

### Authz kuralları (testle kilitli)

1. Actor'un `org_id`'si dışında hiçbir satıra okuma/yazma yok.
2. Path veya body'de `org_id` kabul edilmez; gönderilirse yok sayılır
   veya 400 — tercih: alan yok, yapısal olarak imkânsız.
3. Cross-org: org A admin'i, org B üyesinin UUID'si ile
   `PATCH`/`DELETE` → 404 (varlığı sızdırmamak) veya 403; **tek seçim:
   404**, yönetim hesabı detayıyla aynı "yokmuş gibi" stili.
4. Individual org token'ı → tüm `/org/*` 403.
5. `org_role = member` → 403.
6. `seat_limit` doluyken `POST /org/members` → 409.
7. Owner'a `PATCH` rol / `DELETE` → 400.
8. Son admin'i member'a düşürmek serbest (owner duruyor); owner yoksa
   zaten şirket yaratılamaz — invariant: her company org'da ≥1 owner.

Denetim: üye ekleme / rol / çıkarma `audit_log`'a yazılır
(`actor_id`, `action`, `target`, metadata-only `detail`). Platform
denetim ekranı bunları görür; şirket paneli MVP'de kendi denetim
sayfasını açmaz (gerekirse sonraki tur).

## 7. Ekip yönetimi akışları

### Üye ekleme (e-posta yok)

1. Org admin ad, e-posta, rol (`admin`|`member`) girer.
2. Sunucu e-posta çakışmasını kontrol eder (global unique).
3. `member_count < seat_limit` değilse 409.
4. `crypto/rand` geçici parola → bcrypt → kullanıcı satırı:
   `org_id = actor.org_id`, `org_role`, `must_change_password = true`.
5. Yanıtta `temporary_password` bir kez; hash saklanır, düz metin yok.
6. Admin parolayı güvenli kanaldan iletir (yüz yüze / mevcut şirket
   kanalı). Posta yok.
7. Yeni üye girişte parola değiştirmeye zorlanır
   (`RequirePasswordFresh` + refresh bayrağı — yönetim paneli §3 ile
   aynı mekanizma, yeniden icat yok).

### Rol değiştirme

- `admin` ↔ `member` only.
- Owner dokunulmaz.
- Claims bayatlığı: yazma handler satırdan güncel rolü okur.

### Üye çıkarma

- Onay UI.
- Owner çıkarılamaz.
- Kullanıcı silinir; cascade sessions / user-scoped rows.
- Aktif access token kısa ömürlü; refresh satır olmayınca düşer.

### Koltuk

- `organizations.seat_limit` platform Hesaplar'da set edilir.
- Şirket paneli okur, aşımı engeller, limiti yükseltmez.
- Kota ürünü (analiz hakkı, soft-cap uyarıları) yok.

## 8. Şirket yaratma (bu panelde değil)

Akış değişmiyor; sahiplik netleşiyor:

1. Platform admin `/yonetim/hesaplar` → tür şirket → unvan, vergi no,
   koltuk, sahip e-postası.
2. `CreateCompany`: org + owner, geçici parola bir kez.
3. Owner giriş yapar, parolayı değiştirir, `/sirket`'i görür.
4. Owner/admin çalışan ekler.

Şirket paneli "şirket ol" veya "org yarat" sunmaz. Self-serve company
signup sonraki tur, faturalama ile birlikte.

## 9. Yönetim paneliyle ilişki

- **Yerine geçmez.** `/yonetim` operatörde kalır.
- **Altında yaşamaz.** `/yonetim/sirket/...` yok.
- **API paylaşmaz.** Org admin `/admin/stats` çağırmaz; platform admin
  `/org/members` ile müşteri ekibine üye eklemez (MVP — gerekirse sonra
  Hesaplar detayına "üye ekle" ayrı karar).
- **Görsel dil paylaşır.** Token'lar, yoğunluk, chart ailesi.
- **Veri modeli paylaşır.** `organizations`, `users.org_*`,
  `must_change_password`, `audit_log` — 012/014 üzerine inşa.
- **Risk yorumu güncellenir.** Yönetim paneli "org_id sorgu filtresi
  değil" demişti. Bu tur, `/org/*` altında **bilinçli olarak**
  `org_id = actor` filtresi ekler. Bu, dört tabloya (`sessions`,
  `llm_runs`, `assessments`, `conversations`) ürün sorgularında genel
  çok kiracılı filtre demek **değildir** — yalnızca org API'sinin
  toplu metadata okuması. Ürün yolları hâlâ `user_id` kapsamlı.
  Genel tenant izolasyonu ayrı spec olarak duruyor; bu iş onu
  sessizce tamamlamış sayılmaz.

## 10. Uygulama sırası

Her aşama kendi başına sevk edilebilir:

1. **Claims + OrgGate + kabuk.** Token'a org alanları; `RequireOrgAdmin`;
   `/sirket` layout, giriş, nav linki, boş özet. Backend'de `/org/me`.
   Bittiğinde kapı doğru çalışır, içerik incedir.
2. **Ekip.** `GET/POST/PATCH/DELETE /org/members`, seat_limit, geçici
   parola, owner koruması, audit yazma, `/sirket/ekip`.
3. **Kullanım.** `GET /org/stats`, chart'lar, `/sirket/kullanim`, özet
   kutularını doldur.
4. **Aktivite.** `GET /org/activity`, `/sirket/aktivite`, özette kısa
   liste.

Kapsam kesilirse önce 4, sonra 3 kısaltılır (yalnızca kutular, chart
yok). Ekip (2) ürünün satış vaadinin parçası — kesilmez.

## 11. Test

**Backend** — fake store / handler testleri (`handler_swap_test` deseni):

- Org A admin'i org B üyesine `PATCH`/`DELETE` → 404; B'nin satırı
  değişmez.
- Individual ve member token → `/org/*` 403.
- `POST /org/members` seat doluyken 409; altındayken 201 ve
  `temporary_password` bir kez.
- Owner'a rol/silme → 400.
- Stats: yalnızca actor org'unun assessments/llm_runs sayılır; başka
  org'un satırı kovaya girmez (seed'li cross-org fixture).
- Claims: refresh sonrası güncel `org_role`; çıkarılan üyenin refresh'i
  fail.
- `pwd_reset` işaretli token `/org/*`'te 403, `/auth` açık.

**Frontend** — `node --test`:

- OrgGate: company+owner/admin açık; individual/member kapalı.
- Rota koruması: kapalı kullanıcı `/sirket/*` → `/`.
- Seat dolu UI: ekle düğmesi disabled + sunucu 409 mesajı.

## 12. Deploy

**Backend önce** — frontend `/org/me` ve gate için org claim'lerine
bakar; uçlar yokken panel boş veya sürekli 401/403 döner.
Yeni `/org/*` eski istemciyi bozmaz.

Kabuk (aşama 1) claim yokken bozulmamalı diye: önce backend claim +
`/org/me`, sonra frontend kabuk — aynı release penceresinde, backend
önce deploy.

Push yarım sürümdür; Render webhook'u pratikte ateşlemiyor. Deploy elle
doğrulanır.

## 13. Bilinçli olarak yapılmayanlar

- Platform admin impersonation / "üyenin yerine gör".
- Vaka metni, transcript, rapor gövdesi, kriter skoru paneli.
- Org admin'e `/admin/*` veya `/yonetim` erişimi.
- `/yonetim` altında scoped şirket bölümü; ürün içi sekme.
- E-posta daveti / şifre sıfırlama postası.
- Analiz/LLM kotası ve soft-cap uyarıları.
- Self-serve şirket kaydı.
- Owner transferi; birden fazla owner.
- Şirket paneli içinde denetim UI'sı (yazma var, yüzey yok).
- Genel ürün sorgularına `org_id` filtresi (çok kiracılı tur ayrı).
- Prometheus'a bağlı herhangi bir şirket-paneli widget'ı.
