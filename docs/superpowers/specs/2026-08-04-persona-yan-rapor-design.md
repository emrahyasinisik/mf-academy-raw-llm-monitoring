# Persona + yan rapor paneli — tasarım

**Tarih:** 4 Ağustos 2026  
**Durum:** onaylandı, plana hazır  
**Yaklaşım:** Persona workspace (History | Chat | ReportPanel)

## Ne yapıyoruz ve neden

Bugün `#persona` sohbet + canlı araştırma + verdict üretiyor; `#analiz`
form + rubrik raporu. İkisi aynı “yatırılabilirlik” problemini çözüyor ama
yüzeyler ve veri bağları ayrı. Operatör Cursor’daki gibi konuşmak, sonucu
yanda görmek istiyor — form doldurmak değil.

Bu iş persona tarafında birleştirir: konuşma ortada kalır, onaylı bir
`analysis/run` sonrası mevcut rubrik `Rapor` sağda açılır. Analiz ekranı
sales / doğrudan form yolu olarak durur.

## Verilen kararlar

1. **Shell + gerçek `Rapor`** — placeholder yok; `AnalizView`’daki rapor
   bileşeni extract edilip paylaşılır.
2. **Hibrit tetikleyici** — verdict gelince “Rapor üret” önerisi; kullanıcı
   onaylamadan GPU analizi başlamaz.
3. **Intake hibrit** — FE’de zorunlu Konu + Amaç; persona sabit checklist’ten
   sırayla derinleştirme soruları sorar (tur başına tek soru, mevcut davranış
   korunur).
4. **Hafif BE link** — `conversations.assessment_id`; thread reopen’da panel
   hydrate. Junction / çoklu rapor yok.
5. **Case metni FE’de birleşir** — yeni `POST /decision/analyze` yok.
6. **Domain v1** — sabit `startup-investability`.
7. **ReportPanel resizable** — sürükleyerek büyüt/küçült; tercih saklanır.

## Kabuk ve kullanıcı akışı

### Layout (`#persona`, `lg+`)

```
┌──────────┬─────────────────────┬──────────────────┐
│ History  │  Chat (merkez)      │  ReportPanel     │
│ 264px    │  intake + transcript│  resizable       │
└──────────┴─────────────────────┴──────────────────┘
```

- Panel kapalı: bugünkü History | Chat.
- Panel açık: chat daralır; sağda rapor. X paneli gizler; `assessment_id`
  thread’de kalır — “Raporu aç” / thread seçimi ile yeniden hydrate.
- `md` altı: panel overlay / full-bleed drawer (üç kolon sığmaz).

### Resize

- Sol kenarda drag handle.
- Min ~280px, max ~viewport genişliğinin %55’i.
- Genişlik `localStorage` anahtarı: `persona.reportPanelWidth`.
- Sürüklerken `user-select: none`; bırakınca persist.

### Akış

1. Yeni thread: Konu + Amaç zorunlu (IntakeFields). İlk user turn’e gömülür.
2. Persona: research + checklist’ten tek netleştirici soru / tur; yeterliyse
   `KARAR` / `SKOR` / `GEREKÇE`.
3. Verdict parse edilince transcript’te **Rapor üret** CTA.
4. Onay → FE case birleştirir → `POST /analysis/run` → ReportPanel loading →
   `Rapor`.
5. Başarı → conversation’a `assessment_id` yazılır.
6. Thread reopen → `GET /analysis/{id}` ile panel hydrate (v1: bağlı rapor
   varsa panel açık gelir).

## Bileşenler

| Parça | Rol |
|---|---|
| `ReportPanel` | Sağ kabuk: header (başlık, skor özeti, kapat), resize handle, body |
| `Rapor` (extract) | `AnalizView` → paylaşılan UI; kriter satırları, skor, footer |
| `IntakeFields` | Konu + Amaç; ilk gönderimden sonra özet chip |
| `ReportCTA` | Verdict altında “Rapor üret”; bağlı rapor varsa “Raporu göster” |
| `PersonaView` | Üç kolon orkestrasyon, analysis çağrısı, link yazma |

`#analiz` form akışı aynı kalır; yalnızca `Rapor` import’a döner.

## Case birleştirme

`POST /analysis/run` body (mevcut contract):

- `domain`: `startup-investability`
- `subject_title`: intake Konu
- `subject`:

```
## Konu
…

## Amaç
…

## Sohbet özeti
(user cevapları + persona’nın son research/verdict gövdesi, kısa)

## Kaynaklar
(numaralı başlık/URL, varsa)
```

Token bütçesi aşılırsa Konu / Amaç / Kaynaklar korunur; ortadaki tekrarlayan
sohbet parçaları kırpılır.

## Backend

### Migration

```sql
ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS assessment_id UUID
  REFERENCES assessments(id) ON DELETE SET NULL;
```

Assessment silinince FK SET NULL — thread kalır. Redaction satırı silmez;
FE GET redacted/404’te linki temizler.

### API

- `Conversation` ve `ConversationSummary` JSON’a `assessment_id` eklenir
  (`*string` / omitempty).
- Link yazma: mevcut `PATCH /decision/conversations/{id}` genişler.
  Bugün body yalnızca `title` kabul ediyor; opsiyonel `assessment_id` eklenir
  (`null` gönderilirse link temizlenir). İkisinden en az biri gerekir.
  Ownership: conversation kullanıcının; `assessment_id` set edilirken
  assessment aynı `user_id`’ye ait olmalı (yoksa 404).
- `POST /analysis/run` değişmez.
- Yeni orchestrator endpoint yok.

### Persona prompt

`persona.go` checklist’e sabit alanlar eklenir: aşama, coğrafya, bütçe /
ticket büyüklüğü, zaman ufku. Davranış kuralı aynı kalır: **tur başına tek
soru**; listedekilerden seç, uydurma. Yeterliyse nihai karar formatı değişmez.

## Hata ve kenar durumları

| Durum | Davranış |
|---|---|
| Analiz fail / timeout | Panel açık, hata + Yeniden dene; chat bozulmaz |
| `assessment_id` var, GET 404 / redacted | Panel “rapor artık yok”; FE `assessment_id: null` PATCH ile linki temizler |
| Assessment satırı silinirse | FK `ON DELETE SET NULL`; thread kalır |
| Assessment yalnızca redakte (satır durur) | FK tetiklenmez; GET içeriği boş/404 sayılır; yukarıdaki temizleme uygulanır |
| İkinci “Rapor üret” | Yeni assessment; `assessment_id` overwrite (son kazanır) |
| Inference kapalı | Mevcut persona hatası; CTA disabled veya aynı 503 yolu |
| Intake eksik | Gönderim engelli; chat başlamaz |

## Test planı

**Backend**

- Migration + store: assessment_id set / clear / SET NULL on assessment delete.
- PATCH: yabancı assessment → reddedilir; kendi assessment → yazılır.
- GET conversation: assessment_id wire’da.

**Frontend**

- Intake: Konu/Amaç yokken gönderilemez.
- Verdict sonrası CTA görünür; onay öncesi `analysis/run` çağrılmaz.
- Başarılı run → panel `Rapor` gösterir; PATCH assessment_id.
- Thread seç → bağlı rapor hydrate.
- Resize: min/max clamp; reload’da genişlik geri gelir.
- `AnalizView` hâlâ kendi form + paylaşılan `Rapor` ile çalışır.

**Manuel**

- lg ekranda üç kolon + drag.
- Dar viewport’ta drawer.
- Analiz ~1 dk: send disabled; StatusRail “Rapor üretiliyor”.

## Bilinçli non-goals (bu faz)

- Domain picker / digital-marketing persona yolu
- Çoklu rapor geçmişi per thread (junction)
- `POST /decision/analyze` sunucu orkestrasyonu
- `#analiz` nav’ını kaldırmak veya formu sohbete taşımak
- Otomatik (onaysız) analysis tetikleme
- Report panel içeriğinin yeniden tasarımı (sonraki session)
- Autoscaling / yeni inference mimarisi

## Mimari özet

```
PersonaView
  ├─ HistoryPanel          (mevcut)
  ├─ Chat + Intake + CTA   (genişletilmiş)
  └─ ReportPanel (resizable)
         └─ Rapor (extract from AnalizView)

FE:  decision/chat → … → verdict → CTA
     → assemble case → analysis/run → PATCH assessment_id
     → GET analysis/{id} on reopen

BE:  conversations.assessment_id → assessments(id) ON DELETE SET NULL
```
