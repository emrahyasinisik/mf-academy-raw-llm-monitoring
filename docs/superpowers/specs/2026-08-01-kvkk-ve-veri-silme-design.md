# KVKK aydınlatma metni ve veri silme — tasarım

**Tarih:** 1 Ağustos 2026
**Durum:** onaylandı, plana hazır

## Ne yapıyoruz ve neden

Barındırdığımız demo (Render + Vercel) şu anda başkalarının şirket belgelerini
süresiz saklıyor ve bunu hiçbir yerde söylemiyor. `assessments.subject` analiz
edilen vakanın tam metni, `llm_runs.prompt/response` üreteç ekranından geçen her
şey. Repoda `kvkk`, `gdpr`, `aydınlatma`, `saklama süresi` için sıfır eşleşme
var: ne metin, ne rıza, ne silme yolu.

Bu bir uyum kutusu işaretleme işi değil. Satılan eksen **veri egemenliği** ve
konumlandırma cümlesi *"Veriler sunucunuzdan çıkmaz"*
([`urun-ve-pazarlama.md`](../../urun-ve-pazarlama.md) §4). Demo'da veriler
bizim sunucumuzda duruyor ve silinemiyor — yani iddianın kendisi, prospect'in
deneyeceği ilk yerde karşılanmıyor.

## Verilen kararlar

1. **Veri sorumlusu biziz.** Metin bizim barındırdığımız demo'yu anlatır,
   operatörün kendi kurulumunu değil. Bu gerçek yükümlülük doğurur: beyan
   edilen saklama süresi fiilen işletilmeli, başvuru kanalı fiilen çalışmalı.
2. **Silme = redaksiyon.** Kişisel alanlar boşalır, ölçüm satırı kalır.
3. **Saklama süresi 30 gün**, sonrasında aynı redaksiyon otomatik uygulanır.
4. **Yüzey:** `#gizlilik` rotası (nav'a girmez), alt bilgi ve giriş ekranından
   bağlantı; silme aksiyonu rapor listesinde satır içinde.

### Neden redaksiyon, satırı silmek değil

`assessments` bilinçli olarak bir denetim izi: `criteria_snapshot` rubrik
değişse bile geçmiş raporun anlamını koruyor, `raw_response` "model saçmaladı"
ile "parser'ımız yanlış" ayrımı için birebir tutuluyor. Satırı silmek, o raporun
katkıda bulunduğu her toplu ölçümü geriye dönük değiştirir — `schema_valid`
oranı ve trial gruplarının tutarlılık figürü dahil. Bir trial grubunun tek ayağı
silinirse grup yarım kalır.

Redaksiyon ikisini ayırıyor: kişisel veri gider, ölçüm kalır. Kalan satır artık
anonim bir ölçümdür.

## Kabul edilen kayıp

`findings` de boşalıyor, çünkü içinde belgeden **birebir alıntılar** var.
Ürünün "kanıtı denetleyebilirsin" vaadini taşıyan alan, aynı zamanda en çok
kişisel veri taşıyan alan. Sonuç: redakte edilmiş bir rapor "4. kriter 2 puan"
der ama **neden** olduğunu gösteremez.

Yani denetlenebilirlik 30 günlük bir pencere. Bu bilinçli ve KVKK metninde de
bu şekilde yazılacak — sessizce kaybolan bir özellik değil, beyan edilen bir
sınır.

## Veri katmanı

### Migration 010 — 009 değil

**Tuzak:** bu dalda son migration 008, ama `feat/persona-history` dalında 009
kullanılmış (commit `a52aaf0`, `conversations` + `conversation_messages`). İki
dal birleşince aynı numarada iki dosya olur. Bu iş **010**'dan başlar.

```sql
ALTER TABLE assessments ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ;
ALTER TABLE llm_runs    ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ;

-- Kısmi indeks: süpürge yalnızca redakte edilmemiş satırları arar, ve işi
-- biten satır bir daha taramaya girmez. Tam indeks tablo büyüdükçe süpürgeyi
-- yavaşlatırdı; bu onu sabit tutuyor.
CREATE INDEX IF NOT EXISTS idx_assessments_unredacted
  ON assessments (created_at) WHERE redacted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_llm_runs_unredacted
  ON llm_runs (created_at) WHERE redacted_at IS NULL;
```

### Boşalan ve kalan alanlar

| Tablo | Boşalır | Kalır |
|---|---|---|
| `assessments` | `subject`, `subject_title`, `findings` (`'[]'`), `raw_response` | `overall_score`, `coverage`, `schema_valid`, `repair_attempts`, `criteria_snapshot`, `domain_version`, `model`, `target`, `latency_ms`, token sayıları, `trial_group`, `created_at` |
| `llm_runs` | `prompt`, `response`, `system_prompt`, `expected_keywords` (`'{}'`) | `model`, `target`, token sayıları, `latency_ms`, `temperature`, `metadata`, `created_at` |

`system_prompt` de boşalıyor: [`models.go:101`](../../../mf-backend/internal/llm/models.go)
`CreateRunRequest` onu frontend'den alıyor, yani kullanıcı oraya kişisel veri
yazabilir. "Bizim şablonumuz" varsayımı güvenli değil.

### Store metotları

Her iki store'da aynı çift, ikisi de **aynı UPDATE'i** paylaşır — kullanıcının
bastığı düğme ile 30. günde olanın birebir aynı işlem olması, metinde tek
cümleyle anlatılabilmesinin ve testte tek yolun doğrulanmasının şartı.

```go
// analysis.Store
RedactAssessment(ctx, userID, id string) (bool, error)   // sahip kapsamlı
SweepAssessments(ctx, olderThan time.Time) (int64, error)

// llm.Store
RedactRun(ctx, userID, id string) (bool, error)
SweepRuns(ctx, olderThan time.Time) (int64, error)
```

`Redact*` idempotent: zaten redakte edilmiş satır için `false` döner, hata
değil. `redacted_at` yalnızca ilk seferde yazılır.

## API

```
DELETE /analysis/{id}     204 No Content
```

- Sahip kapsamlı. Başkasının raporu **404**, 403 değil — varlığını sızdırmamak
  için. `GetAssessment` zaten bu deseni izliyor.
- Idempotent: zaten redakte edilmiş rapor da 204 döner.
- `defaultTimeout` altına, `Get("/{id}")` ile aynı gruba. Wildcard sırası
  önemli: literal yollar önce eşleşmeli
  ([`routes.go`](../../../mf-backend/internal/analysis/routes.go) notu).

**Mevcut `DELETE /llm/runs/{id}` değişmiyor.** O uç nokta satırı gerçekten
siliyor ve redaksiyondan daha koruyucu; sevk edilmiş davranışı bu iş kapsamında
değiştirmek gereksiz risk. Asimetri bilinçli ve spec'te kayıtlı: kullanıcı bir
üreteç koşumunu tamamen silebilir, bir raporu redakte eder.

## Arka plan işçisi

Yeni paket: `internal/retention`.

```go
func Sweep(ctx context.Context, a AssessmentSweeper, l RunSweeper, age time.Duration) (int64, error)
```

`cmd/server/main.go` içinde, `sessionCleanup` ile aynı desende: `workerCtx` ile
başlar, boot'ta bir kez koşar, sonra ticker. Yeni altyapı yok.

- Aralık: 6 saat (`RETENTION_SWEEP_INTERVAL`, `getDuration`).
- Yaş: 30 gün (`RETENTION_DAYS`, `getInt`).
- Her koşuda `slog.Info("retention sweep", "assessments", n, "runs", m)` —
  yalnızca sıfırdan büyükse, `sessionCleanup` gibi.

`RETENTION_DAYS=0` süpürgeyi **kapatır** ve boot'ta bir uyarı basar
(`Config.Warnings()`). Operatörün kendi kurulumunda bu meşru bir tercih;
demo'da açık kalır.

## Frontend

### `#gizlilik` rotası

Yeni görünüm `GizlilikView.tsx`. `MasterView` birliğine eklenir ama `NAV`
dizisine **eklenmez** — nav çalışma araçları için, bu bir belge. `isMaster`
şu an NAV üyeliğine bakıyor, bu yüzden rota tanınabilsin diye ayrı bir sabit
liste gerekiyor.

Bağlantı iki yerde: AppShell'in alt bilgisinde ve `AuthView`'da (kayıt olan
kişi, kaydolmadan önce görebilmeli).

### Rapor listesinde silme

`AnalizView`'daki "Önceki raporlar" listesinde her satırda bir silme aksiyonu.

- Onay adımı zorunlu: geri alınamaz.
- Başarıda satır listede kalır ama **redakte** görünür ("içerik silindi",
  skor ve tarih durur) — silinen şeyin ne olduğunu göstermek, satırı yok
  etmekten dürüst. Açık rapor görüntüleniyorsa o da redakte hâline geçer.
- Liste başlığının altında tek cümle: içeriğin 30 gün sonra otomatik
  silindiği.

### Tip değişikliği

`redacted_at` hem Go tarafındaki `Assessment`/`AssessmentSummary` modellerine
hem de frontend tiplerine eklenir: `redactedAt: string | null`.

`ListAssessments` zaten `subject`, `findings` ve `raw_response` sütunlarını
**seçmiyor** — liste satırları hiçbir zaman kişisel içerik taşımıyor, yalnızca
özet. Bu yüzden listeye eklenecek tek şey `redacted_at`; silme aksiyonu da
id üzerinden çalışır.

Redakte satırların `subject` ve `findings` alanları boş gelir; görünüm bunu
"veri yok" değil "silindi" olarak ayırt etmek için `redactedAt`'e bakar.

## KVKK metninin içereceği bölümler

1. **Veri sorumlusunun kimliği** — MasterFabric, iletişim adresi.
2. **İşlenen veriler** — yapıştırılan vaka metni, üreteç istem/yanıtları,
   e-posta ve oturum kayıtları.
3. **İşleme amacı** — analiz raporunun üretilmesi ve ürünün ölçülmesi.
4. **Hukuki sebep** — hizmetin ifası için gereklilik.
5. **Aktarım** — üçüncü tarafa aktarım yok. Çıkarım kendi donanımımızda koşar;
   demo'da vaka metni Render'daki Postgres'te ve tünel üzerinden GPU kutusunda
   işlenir. Bu somut olarak yazılır, "gerekli hallerde" gibi kaçamakla değil.
6. **Saklama süresi** — 30 gün, sonrasında içerik otomatik silinir; ölçüm
   satırı anonim olarak kalır. Denetlenebilirliğin bu pencereyle sınırlı
   olduğu açıkça belirtilir.
7. **İlgili kişi hakları** — KVKK m.11 sayımı, ve hangisinin üründe hangi
   düğmeye karşılık geldiği.
8. **Başvuru kanalı** — `kvkk@masterfabric.co`.

**Kabul kriteri:** `kvkk@masterfabric.co` kutusu açılıp yanıtlanmadan bu sayfa
yayına alınmaz. Çalışmayan bir başvuru kanalı, metnin tamamını yazılmış bir
vaatten ibaret bırakır.

## Test

**Backend**
- `RedactAssessment`: sahibi olmayan kullanıcı satırı değiştiremez; kişisel
  alanlar boşalır; skor/coverage/schema_valid dokunulmadan kalır; ikinci çağrı
  `false` döner ve `redacted_at` değişmez.
- `SweepAssessments`: yaş sınırının **altındaki** satıra dokunmaz; üstündekini
  redakte eder; zaten redakte edilmiş satırı tekrar saymaz.
- Aynı üçlü `llm.Store` için.
- Handler: 204, başkasının raporunda 404, kimliksiz istekte 401.
- `retention.Sweep`: iki store'u da çağırır, sayıları toplar, biri hata
  verirse diğerinin sonucunu yine de döndürür ve hatayı sarar.

**Frontend**
- Redakte rapor görünümü: `redactedAt` doluysa "silindi" durumu, boş
  `subject` yüzünden "veri yok" durumu değil.
- Silme akışı: onay olmadan istek gitmez.

## Bilinçli olarak yapılmayanlar

- **Rıza kutusu / onay kaydı.** Hukuki sebep hizmetin ifası; açık rıza
  gerektiren bir işleme yok. Onay kutusu, olmayan bir hukuki sebebi taklit
  ederdi.
- **Veri dışa aktarma (taşınabilirlik).** Gerçek bir talep gelene kadar
  yazılmaz; rapor zaten ekranda ve kopyalanabilir.
- **Hesap silme akışı.** `users` üzerindeki `ON DELETE CASCADE` zaten her şeyi
  götürüyor; bunu bir düğmeye bağlamak ayrı bir iş ve bu spec'in kapsamı değil.
- **Operatör kurulumu için ayrı metin.** Orada veri sorumlusu operatör; ona
  yazılacak şey bir şablon, ve bu spec demo'yu çözüyor.
