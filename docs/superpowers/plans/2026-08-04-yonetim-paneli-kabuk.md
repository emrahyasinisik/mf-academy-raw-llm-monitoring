# Yönetim Paneli — 1. Aşama (Kabuk) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Bugün ürün SPA'sının `#admin` hash rotasında duran yönetim yüzeyini, hiçbir özellik eklemeden veya çıkarmadan, kendi kabuğu ve kendi giriş ekranıyla `/yonetim` altına taşımak.

**Architecture:** Next.js App Router altında `src/app/yonetim/` bir rota grubu açılır; kök layout'taki `Providers` (Auth + Machine context) miras alınır, ürün kabuğu (header, StatusRail, footer) alınmaz. `AdminView.tsx` içindeki dört panel kendi dosyalarına bölünür ve rota sayfalarından çağrılır. Erişim ve rota kararları, JSX'ten ayrı iki saf modüle (`src/lib/adminNav.ts`, `src/lib/adminAccess.ts`) çıkarılır — bu projedeki test koşucusu yalnızca `src/lib/*.test.ts` dosyalarını çalıştırır, yani test edilebilir olmanın şartı saf TypeScript olmak.

**Tech Stack:** Next.js 16.2.10 (App Router), React 19.2.4, TypeScript 5, Tailwind v4, `node --experimental-strip-types --test` (node:test + node:assert/strict). Yeni bağımlılık yok.

**Spec:** [`docs/superpowers/specs/2026-08-04-yonetim-paneli-design.md`](../specs/2026-08-04-yonetim-paneli-design.md) §1, §2 ve §8'in 1. maddesi.

## Global Constraints

- **Yeni npm bağımlılığı eklenmeyecek.** `mf-frontend/package.json` üç üretim bağımlılığı taşıyor (`@mlc-ai/web-llm`, `next`, `react`/`react-dom`) ve bu iş hiçbirini artırmıyor.
- **Arayüz metinleri Türkçe.** Kod, yorum ve commit mesajları mevcut dosyanın dilini izler; ekranda görünen her string Türkçe.
- **Atölye dili yasak.** UI kopyasında kart, tünel, GPU, eğitim koşusu geçmez (`CLAUDE.md` → Positioning language). "AI karar verir", "en iyi model", "otomatik" ifadeleri de yasak.
- **Dark-only.** Açık tema yok; renkler `globals.css` token'larından gelir (`var(--panel)`, `var(--line)`, `var(--text)`, `var(--text-dim)`, `var(--text-faint)`, `var(--brand)`, `var(--ok)`, `var(--warn)`, `var(--bad)`).
- **Testler yalnızca `src/lib/` altında ve saf TypeScript.** `package.json` test betiği: `node --experimental-strip-types --test src/lib/*.test.ts`. JSX içeren dosya test edilemez. Test importları **uzantılı** yazılır (`from "./adminNav.ts"`), çünkü strip-types modu çözümlemeyi uzantıya bakarak yapar — `src/lib/terms.test.ts` bu deseni izliyor.
- **Bu aşamada yeni özellik yok.** Hesaplar, belgeler, denetim, chart'lar sonraki aşamalar. Sidebar'da var olmayan bölüme bağlantı konmaz.
- **`AdminView.tsx` taşınırken davranışı aynen korunur.** Panel içerikleri birebir taşınır; metin, sınıf adı, yorum değiştirilmez.
- **Commit mesajları:** ilk satır ne yapıldığını değil neden yapıldığını söyler, 72 karakteri aşmaz; gövde açıklar; son satır `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- **Doğrulama komutları** (`mf-frontend/` içinden): `npm test`, `npm run lint`, `npm run build`. Üçü de her commit öncesi geçmeli.

---

## File Structure

| Dosya | Sorumluluk |
|---|---|
| `src/lib/adminNav.ts` | **Yeni.** Panel bölümlerinin listesi, `pathname` → bölüm çözümü, eski `#admin` hash'lerinin yeni yola eşlenmesi. Saf, test edilebilir. |
| `src/lib/adminNav.test.ts` | **Yeni.** Yukarıdakinin testleri. |
| `src/lib/adminAccess.ts` | **Yeni.** Oturum + rol → panelin ne göstereceği kararı (`booting` / `login` / `redirect` / `allow`). Saf, test edilebilir. |
| `src/lib/adminAccess.test.ts` | **Yeni.** Yukarıdakinin testleri. |
| `src/components/yonetim/Stat.tsx` | **Yeni.** `AdminView.tsx`'ten taşınan tek sayı kartı. |
| `src/components/yonetim/OverviewPanel.tsx` | **Yeni.** `OverviewPanel` + `GrafanaCard`, taşınan. |
| `src/components/yonetim/ModelPanel.tsx` | **Yeni.** `ModelPanel`, taşınan. |
| `src/components/yonetim/MCPPanel.tsx` | **Yeni.** `MCPPanel`, taşınan. |
| `src/components/yonetim/LogsPanel.tsx` | **Yeni.** `LogsPanel`, taşınan. |
| `src/components/yonetim/PanelShell.tsx` | **Yeni.** Sidebar + topbar + breadcrumb. Kabuğun tamamı. |
| `src/components/yonetim/PanelLogin.tsx` | **Yeni.** Panelin kendi giriş ekranı. |
| `src/components/yonetim/PanelGate.tsx` | **Yeni.** `adminAccess` kararını uygulayan istemci bileşeni: booting / login / yönlendirme / kabuk. |
| `src/app/yonetim/layout.tsx` | **Yeni.** `PanelGate`'i sarar, panel sekmesinin başlığını verir. |
| `src/app/yonetim/page.tsx` | **Yeni.** Genel bölümü. |
| `src/app/yonetim/model/page.tsx` | **Yeni.** Model & Ayarlar. |
| `src/app/yonetim/mcp/page.tsx` | **Yeni.** MCP Sunucuları. |
| `src/app/yonetim/loglar/page.tsx` | **Yeni.** Log Monitörü. |
| `src/components/AppShell.tsx` | **Değişir.** `admin` master view'dan çıkar, `#admin` yönlendirilir, header'a admin'e görünen panel bağlantısı gelir. |
| `src/components/views/AdminView.tsx` | **Silinir.** İçeriğinin tamamı yukarıdaki beş dosyaya taşındıktan sonra çağıranı kalmıyor. |

---

### Task 1: Rota çözümleyici

**Files:**
- Create: `mf-frontend/src/lib/adminNav.ts`
- Test: `mf-frontend/src/lib/adminNav.test.ts`

**Interfaces:**
- Consumes: yok — bu ilk görev.
- Produces:
  - `type PanelSection = "genel" | "model" | "mcp" | "loglar"`
  - `const PANEL_SECTIONS: readonly { id: PanelSection; label: string; path: string }[]`
  - `function sectionFromPath(pathname: string): PanelSection`
  - `function legacyHashToPath(hash: string): string | null`

- [x] **Step 1: Write the failing test**

`mf-frontend/src/lib/adminNav.test.ts`:

```typescript
import { test } from "node:test";
import assert from "node:assert/strict";
import { PANEL_SECTIONS, sectionFromPath, legacyHashToPath } from "./adminNav.ts";

test("kök yol Genel bölümüne düşer", () => {
  assert.equal(sectionFromPath("/yonetim"), "genel");
});

// Next.js bazı yapılandırmalarda sondaki eğik çizgiyi bırakıyor; iki yol da
// aynı bölüm olmalı, yoksa sidebar'da hiçbir şey seçili görünmez.
test("sondaki eğik çizgi bölümü değiştirmez", () => {
  assert.equal(sectionFromPath("/yonetim/"), "genel");
});

test("alt yollar kendi bölümlerine çözülür", () => {
  assert.equal(sectionFromPath("/yonetim/model"), "model");
  assert.equal(sectionFromPath("/yonetim/mcp"), "mcp");
  assert.equal(sectionFromPath("/yonetim/loglar"), "loglar");
});

// Bilinmeyen bir alt yolun boş bir kabuk yerine Genel'i seçmesi, ürünün
// geri kalanındaki davranışla aynı: AppShell de tanımadığı alt görünümü
// varsayılana düşürüyor.
test("bilinmeyen alt yol Genel'e düşer", () => {
  assert.equal(sectionFromPath("/yonetim/olmayan-bir-sey"), "genel");
});

test("her bölümün yolu kendi bölümüne geri çözülür", () => {
  for (const s of PANEL_SECTIONS) {
    assert.equal(sectionFromPath(s.path), s.id);
  }
});

// Eski hash rotaları ölmüyor: gömülü bağlantısı olan kimse 404 görmemeli.
// Bu eşleme AppShell'deki yönlendirmenin tek doğruluk kaynağı.
test("eski admin hash'leri yeni yollara eşlenir", () => {
  assert.equal(legacyHashToPath("#admin"), "/yonetim");
  assert.equal(legacyHashToPath("#admin/overview"), "/yonetim");
  assert.equal(legacyHashToPath("#admin/model"), "/yonetim/model");
  assert.equal(legacyHashToPath("#admin/mcp"), "/yonetim/mcp");
  assert.equal(legacyHashToPath("#admin/logs"), "/yonetim/loglar");
});

test("bilinmeyen admin alt sekmesi panelin köküne gider", () => {
  assert.equal(legacyHashToPath("#admin/kayip-sekme"), "/yonetim");
});

// Diğer master görünümlerin hash'lerine dokunulmuyor: null, "burada işim yok"
// demek ve AppShell'in yönlendirme yapmamasını sağlıyor.
test("panel dışı hash'ler eşlenmez", () => {
  assert.equal(legacyHashToPath("#analiz"), null);
  assert.equal(legacyHashToPath("#gizlilik"), null);
  assert.equal(legacyHashToPath(""), null);
});
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `Cannot find module './adminNav.ts'`

- [x] **Step 3: Write minimal implementation**

`mf-frontend/src/lib/adminNav.ts`:

```typescript
// Panelin rota tablosu — tek doğruluk kaynağı.
//
// Sidebar bunu okuyup çiziyor, sayfa başlığı bunu okuyup yazıyor, ve eski
// hash rotalarının yönlendirmesi de burada. Üçü ayrı yerlerde tutulsaydı bir
// bölüm eklemek üç dosyayı düzenlemek olurdu ve biri her seferinde unutulurdu.
//
// JSX'ten uzak tutuluyor çünkü bu projedeki test koşucusu yalnızca
// src/lib/*.test.ts dosyalarını çalıştırıyor: burada durması, rota mantığının
// test edilebilir tek biçimi.

export type PanelSection = "genel" | "model" | "mcp" | "loglar";

export const PANEL_SECTIONS: readonly {
  id: PanelSection;
  label: string;
  path: string;
}[] = [
  { id: "genel", label: "Genel", path: "/yonetim" },
  { id: "model", label: "Model & Ayarlar", path: "/yonetim/model" },
  { id: "mcp", label: "MCP Sunucuları", path: "/yonetim/mcp" },
  { id: "loglar", label: "Log Monitörü", path: "/yonetim/loglar" },
];

/** Adres çubuğundaki yol → hangi bölüm açık. Tanınmayan yol Genel'e düşer. */
export function sectionFromPath(pathname: string): PanelSection {
  const clean = pathname.replace(/\/+$/, "");
  const found = PANEL_SECTIONS.find((s) => s.path === clean);
  return found ? found.id : "genel";
}

// Panel `#admin` hash'inde yaşarken paylaşılmış bağlantılar var. Silmek yerine
// eşliyoruz — bu reponun kuralı: nav'dan inen rota adreslenebilir kalır.
const LEGACY_TABS: Record<string, string> = {
  "": "/yonetim",
  overview: "/yonetim",
  model: "/yonetim/model",
  mcp: "/yonetim/mcp",
  logs: "/yonetim/loglar",
};

/** Eski `#admin[/sekme]` hash'i → yeni yol. Panel dışı hash için null. */
export function legacyHashToPath(hash: string): string | null {
  const [view, tab = ""] = hash.replace(/^#/, "").split("/");
  if (view !== "admin") return null;
  return LEGACY_TABS[tab] ?? "/yonetim";
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd mf-frontend && npm test`
Expected: PASS — `adminNav.test.ts` içindeki 8 testin tamamı, mevcut testler de dahil hiçbir başarısızlık yok.

- [x] **Step 5: Commit**

```bash
cd mf-frontend
npm run lint
git add src/lib/adminNav.ts src/lib/adminNav.test.ts
git commit -m "$(cat <<'EOF'
Keep the panel's route table in one place, and keep #admin alive

Sidebar, page title and the legacy redirect all need to know the same four
sections. Three copies would mean adding a section is a three-file edit, and one
of them would be forgotten every time.

It sits in src/lib rather than beside the components because the test runner
only reaches src/lib/*.test.ts — pure TypeScript is the only shape of this
logic that can be tested at all.

legacyHashToPath is not politeness. #admin links have been shared, and this
repo's rule is that a route leaving the nav stays addressable.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Erişim kararı

**Files:**
- Create: `mf-frontend/src/lib/adminAccess.ts`
- Test: `mf-frontend/src/lib/adminAccess.test.ts`

**Interfaces:**
- Consumes: yok.
- Produces:
  - `type PanelGateState = "booting" | "login" | "redirect" | "allow"`
  - `function panelGate(input: { loading: boolean; user: { role: string } | null }): PanelGateState`

- [x] **Step 1: Write the failing test**

`mf-frontend/src/lib/adminAccess.test.ts`:

```typescript
import { test } from "node:test";
import assert from "node:assert/strict";
import { panelGate } from "./adminAccess.ts";

// Oturum henüz çözülmemişken karar verilemez. "Kullanıcı yok" ile "kullanıcı
// daha yüklenmedi" karıştırılırsa, sayfayı yenileyen yönetici bir anlığına
// giriş ekranını görür ve sonra panele atlar.
test("oturum çözülmeden karar verilmez", () => {
  assert.equal(panelGate({ loading: true, user: null }), "booting");
  assert.equal(panelGate({ loading: true, user: { role: "admin" } }), "booting");
});

test("oturum yoksa panelin kendi giriş ekranı", () => {
  assert.equal(panelGate({ loading: false, user: null }), "login");
});

// Yönetici olmayan biri panelin varlığını öğrenmiş olabilir; kapı burada
// kapanmaz, backend'de kapanır. Bu yalnızca çalışmayacak bir ekranı
// göstermemek için.
test("yönetici olmayan ürüne yönlendirilir", () => {
  assert.equal(panelGate({ loading: false, user: { role: "user" } }), "redirect");
});

test("yönetici panele girer", () => {
  assert.equal(panelGate({ loading: false, user: { role: "admin" } }), "allow");
});
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd mf-frontend && npm test`
Expected: FAIL — `Cannot find module './adminAccess.ts'`

- [x] **Step 3: Write minimal implementation**

`mf-frontend/src/lib/adminAccess.ts`:

```typescript
// Panele girişte verilen tek karar, tek yerde.
//
// Bu bir güvenlik sınırı DEĞİL. Sınır backend'de: /admin/* alt ağacı
// RequireAuth + RequireRole(admin) altında ve her istekte yeniden bakıyor.
// Buradaki karar yalnızca ekranın ne göstereceğini seçiyor — tarayıcının
// bildiği her şeyi tarayıcının kullanıcısı değiştirebilir.
//
// Dikkat edilen tek incelik `loading`: "kullanıcı yok" ile "kullanıcı henüz
// yüklenmedi" aynı şey değil. İkisi karışırsa sayfayı yenileyen yönetici bir
// kare boyunca giriş ekranını görür.
//
// Koşul kabulü kapısı burada YOK, ve bu bilerek: 4. aşamada hukuki metni
// panelden düzeltecek olan operatör, düzelteceği metnin eski hâlini kabul
// etmeden panele giremiyorsa metni hiç düzeltemez. Operatör hizmeti tüketen
// taraf değil, veri sorumlusunun kendisi.

export type PanelGateState = "booting" | "login" | "redirect" | "allow";

export function panelGate(input: {
  loading: boolean;
  user: { role: string } | null;
}): PanelGateState {
  if (input.loading) return "booting";
  if (!input.user) return "login";
  return input.user.role === "admin" ? "allow" : "redirect";
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd mf-frontend && npm test`
Expected: PASS — dört testin tamamı.

- [x] **Step 5: Commit**

```bash
cd mf-frontend
npm run lint
git add src/lib/adminAccess.ts src/lib/adminAccess.test.ts
git commit -m "$(cat <<'EOF'
Separate "no user" from "user not loaded yet" before the panel decides

The gate has four outcomes and only one of them is interesting to get wrong:
treating an unresolved session as a missing one shows the login screen for a
frame to an admin who is already signed in, then snaps to the panel.

No terms gate here, and that is deliberate. Phase four puts the legal text
editor in this panel; an operator who must accept the old wording before they
can reach the screen that fixes it can never fix it.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Dört paneli kendi dosyalarına ayır

`AdminView.tsx` 750 satır ve dört bağımsız modülü birden taşıyor. Bu bölme işin gerektirdiği bir temizlik: paneller artık ayrı rotalardan çağrılacak, ve tek dosyada kalmaları her rotanın dördünü birden yüklemesi demek.

**Bu görevde tek satır davranış değişmiyor.** Kod birebir taşınıyor: metin, sınıf adı, yorum, hepsi aynı.

**Files:**
- Create: `mf-frontend/src/components/yonetim/Stat.tsx`
- Create: `mf-frontend/src/components/yonetim/OverviewPanel.tsx`
- Create: `mf-frontend/src/components/yonetim/ModelPanel.tsx`
- Create: `mf-frontend/src/components/yonetim/MCPPanel.tsx`
- Create: `mf-frontend/src/components/yonetim/LogsPanel.tsx`
- Modify: `mf-frontend/src/components/views/AdminView.tsx` (taşınan gövdeler çıkarılır, dosya Task 7'de silinir)

**Interfaces:**
- Consumes: yok.
- Produces — sonraki görevler bu beş adı içe aktaracak:
  - `export function Stat(props: { label: string; value: string; hint?: string; tone?: string; index?: number }): React.ReactElement`
  - `export function OverviewPanel(): React.ReactElement`
  - `export function ModelPanel(): React.ReactElement`
  - `export function MCPPanel(): React.ReactElement`
  - `export function LogsPanel(): React.ReactElement`

- [x] **Step 1: `Stat.tsx` dosyasını oluştur**

`AdminView.tsx`'in **85–118. satırlarını** (`/** A single figure… */` yorumundan `Stat` fonksiyonunun kapanış süslü parantezine kadar) birebir kopyala. Dosyanın başına şunu ekle ve `function Stat` → `export function Stat` yap:

```tsx
"use client";
```

Başka import gerekmiyor: `Stat` yalnızca JSX döndürüyor, hook kullanmıyor.

- [x] **Step 2: `OverviewPanel.tsx` dosyasını oluştur**

`AdminView.tsx`'in **120–235. satırlarını** birebir kopyala (`OverviewPanel`, `GRAFANA_URL` sabiti ve `GrafanaCard`, aralarındaki yorumlarla birlikte). Başa şu bloğu koy, `function OverviewPanel` → `export function OverviewPanel` yap (`GrafanaCard` dışa açılmıyor, aynı dosyada kalıyor):

```tsx
"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { AdminOverview } from "@/lib/types";
import { Stat } from "./Stat";
```

- [x] **Step 3: `ModelPanel.tsx` dosyasını oluştur**

`AdminView.tsx`'in **237–493. satırlarını** birebir kopyala. Başa:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type {
  Adapter,
  ActivationResult,
  LLMSettings,
  ModelChoice,
} from "@/lib/types";
```

`function ModelPanel` → `export function ModelPanel`.

- [x] **Step 4: `MCPPanel.tsx` dosyasını oluştur**

`AdminView.tsx`'in **495–647. satırlarını** birebir kopyala. Başa:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { MCPServer } from "@/lib/types";
```

`function MCPPanel` → `export function MCPPanel`.

- [x] **Step 5: `LogsPanel.tsx` dosyasını oluştur**

`AdminView.tsx`'in **649–750. satırlarını** birebir kopyala. Başa:

```tsx
"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { AdminLogEntry } from "@/lib/types";
```

`function LogsPanel` → `export function LogsPanel`.

- [x] **Step 6: `AdminView.tsx`'i geçici olarak yeni dosyalara bağla**

`AdminView.tsx`'te **85. satırdan itibaren dosyanın sonuna kadar her şeyi sil** (`/** A single figure… */` yorumu dahil; 1–84 arası, yani `Tab` tipi, `TABS` dizisi ve `AdminView` bileşeni kalır) ve import bloğunu şununla değiştir (dosya Task 7'de silinecek, ama bu görevin sonunda uygulama derlenip çalışır durumda olmalı):

```tsx
import { useAuth } from "@/store/auth";
import { Segmented } from "@/components/ui/Segmented";
import { RoleGate } from "@/components/ui/RoleGate";
import { OverviewPanel } from "@/components/yonetim/OverviewPanel";
import { ModelPanel } from "@/components/yonetim/ModelPanel";
import { MCPPanel } from "@/components/yonetim/MCPPanel";
import { LogsPanel } from "@/components/yonetim/LogsPanel";
```

Kullanılmayan import kalmamalı: `useCallback`, `useEffect`, `useState`, `api`, `ApiError` ve `@/lib/types` importlarının tamamı gider.

- [x] **Step 7: Derleme, lint ve test**

```bash
cd mf-frontend
npm run lint
npm run build
npm test
```

Expected: üçü de temiz. `npm run build` başarılı, lint'te kullanılmayan değişken uyarısı yok, testler önceki iki görevdeki hâliyle geçiyor.

- [x] **Step 8: Uygulamayı gözle doğrula**

```bash
cd mf-frontend && npm run dev
```

`http://localhost:3000/#admin` adresine yönetici hesabıyla gir. Dört sekme de bugünkü gibi açılmalı: Genel'de sayı kartları, Model'de model seçimi ve adapter listesi, MCP'de sunucu listesi, Loglar'da tablo. Bu adım bir davranış değişikliği aramıyor — **hiçbir şeyin değişmemiş olması** doğrulanan şey.

- [x] **Step 9: Commit**

```bash
git add mf-frontend/src/components/yonetim/ mf-frontend/src/components/views/AdminView.tsx
git commit -m "$(cat <<'EOF'
Split the cockpit before moving it, so the move is not also a rewrite

Four independent modules were sharing one 750-line file. They are about to be
called from four separate routes, and leaving them together would mean every
route pulls all four.

Nothing here changes behaviour: the bodies are moved verbatim, comments
included. Doing the split and the relocation in one commit would have made the
diff unreviewable and hidden any accidental edit inside it.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Panel kabuğu

**Files:**
- Create: `mf-frontend/src/components/yonetim/PanelShell.tsx`

**Interfaces:**
- Consumes: `PANEL_SECTIONS`, `sectionFromPath` (Task 1).
- Produces: `export function PanelShell(props: { children: React.ReactNode }): React.ReactElement`

- [x] **Step 1: Kabuğu yaz**

`mf-frontend/src/components/yonetim/PanelShell.tsx`:

```tsx
"use client";

// Panelin kabuğu: solda sabit bölüm listesi, üstte ince bir şerit, ortada
// bölümün kendisi.
//
// Ürün ekranlarıyla bilerek zıt. Orada yerleşim az ve nefes alan, çünkü
// okunan şey bir rapor; burada yoğun ve liste ağırlıklı, çünkü okunan şey bir
// sistemin durumu. Aynı token setini paylaşıyorlar (globals.css) — panel için
// ayrı tema açmak iki bakım yüzeyi demek olurdu ve panelin ürüne ait olduğu
// görünmeli.
//
// Ürün kabuğundaki header, StatusRail ve alt bilgi burada yok: StatusRail
// çıkarım makinesini anlatıyor ve bu ekranların hiçbiri ona bakmıyor.

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/store/auth";
import { PANEL_SECTIONS, sectionFromPath } from "@/lib/adminNav";

export function PanelShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const active = sectionFromPath(pathname);
  const { user, logout } = useAuth();
  const label = PANEL_SECTIONS.find((s) => s.id === active)?.label ?? "Genel";

  return (
    <div className="min-h-screen grid md:grid-cols-[220px_1fr]">
      <a href="#panel-main" className="skip-link">
        İçeriğe geç
      </a>

      <aside
        className="hidden md:flex flex-col gap-1 p-3"
        style={{
          borderRight: "1px solid var(--line)",
          background: "var(--bg-sunk)",
        }}
      >
        <Link href="/" className="flex items-center gap-2.5 px-2 py-2 mb-2">
          <span
            className="grid place-items-center w-7 h-7 rounded-[var(--r-xs)] font-bold text-[0.7rem] mono"
            style={{
              background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
              color: "var(--brand-ink)",
              boxShadow: "var(--bevel), var(--shadow-1)",
            }}
          >
            MF
          </span>
          <span className="font-display text-sm tracking-tight">Yönetim</span>
        </Link>

        <nav className="flex flex-col gap-0.5" aria-label="Panel bölümleri">
          {PANEL_SECTIONS.map((s) => {
            const on = s.id === active;
            return (
              <Link
                key={s.id}
                href={s.path}
                aria-current={on ? "page" : undefined}
                className="px-2.5 py-2 rounded-[var(--r-sm)] text-sm"
                style={{
                  color: on ? "var(--text)" : "var(--text-dim)",
                  background: on ? "var(--panel-2)" : "transparent",
                  borderLeft: on
                    ? "2px solid var(--brand)"
                    : "2px solid transparent",
                  transition: "color var(--dur-2) var(--ease), background var(--dur-2) var(--ease)",
                }}
              >
                {s.label}
              </Link>
            );
          })}
        </nav>

        <div className="mt-auto pt-3">
          <Link
            href="/"
            className="block px-2.5 py-2 rounded-[var(--r-sm)] text-xs"
            style={{ color: "var(--text-faint)" }}
          >
            ← Uygulamaya dön
          </Link>
        </div>
      </aside>

      <div className="flex flex-col min-w-0">
        <header
          className="sticky top-0 z-20 glass flex items-center justify-between gap-4 px-4 sm:px-5 h-12"
          style={{ borderBottom: "1px solid var(--line)" }}
        >
          {/* Kırıntı yolu, mobilde sidebar gizliyken bölümün adını taşıyan tek
              yer — bu yüzden dekorasyon değil. */}
          <nav aria-label="Konum" className="text-xs min-w-0 truncate">
            <span style={{ color: "var(--text-faint)" }}>Yönetim</span>
            <span aria-hidden style={{ color: "var(--text-faint)" }}>
              {" / "}
            </span>
            <span style={{ color: "var(--text)" }}>{label}</span>
          </nav>

          <div className="flex items-center gap-2.5 shrink-0">
            <span
              className="text-xs mono hidden sm:block max-w-[180px] truncate"
              style={{ color: "var(--text-faint)" }}
              title={user?.email}
            >
              {user?.email}
            </span>
            <button className="btn btn-ghost btn-sm" onClick={logout}>
              Çıkış
            </button>
          </div>
        </header>

        {/* Mobilde sidebar yok; bölümler yatay bir şeride iniyor. Ayrı bir
            menü bileşeni açmıyoruz — dört bölüm bir satıra sığıyor. */}
        <nav
          className="md:hidden flex gap-1 overflow-x-auto scrollbar-thin px-3 py-2"
          style={{ borderBottom: "1px solid var(--line)" }}
          aria-label="Panel bölümleri"
        >
          {PANEL_SECTIONS.map((s) => (
            <Link
              key={s.id}
              href={s.path}
              aria-current={s.id === active ? "page" : undefined}
              className="px-2.5 py-1 rounded-[var(--r-xs)] text-xs whitespace-nowrap"
              style={{
                color: s.id === active ? "var(--text)" : "var(--text-dim)",
                background: s.id === active ? "var(--panel-2)" : "transparent",
              }}
            >
              {s.label}
            </Link>
          ))}
        </nav>

        <main id="panel-main" className="flex-1 min-h-0 p-4 sm:p-5">
          <h1 className="font-display text-xl font-semibold tracking-tight mb-4">
            {label}
          </h1>
          {children}
        </main>
      </div>
    </div>
  );
}
```

- [x] **Step 2: Lint ve derleme**

```bash
cd mf-frontend
npm run lint
npm run build
```

Expected: temiz. (Bu aşamada `PanelShell`'in çağıranı yok; derleme yalnızca dosyanın geçerliliğini doğruluyor.)

- [x] **Step 3: Commit**

```bash
git add mf-frontend/src/components/yonetim/PanelShell.tsx
git commit -m "$(cat <<'EOF'
Give the panel a shell that is dense where the product is spare

The product screens are laid out for reading one report; this one is laid out
for reading the state of a system. Same design tokens, opposite density — a
separate theme would have been two maintenance surfaces and would have made the
panel look like somebody else's software.

StatusRail is not here. It describes the inference machine and none of these
screens ask it anything.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Panelin giriş ekranı

**Files:**
- Create: `mf-frontend/src/components/yonetim/PanelLogin.tsx`

**Interfaces:**
- Consumes: `useAuth` (`login`), `ApiError`.
- Produces: `export function PanelLogin(): React.ReactElement`

- [x] **Step 1: Giriş ekranını yaz**

`mf-frontend/src/components/yonetim/PanelLogin.tsx`:

```tsx
"use client";

// Panelin kendi kapısı.
//
// Ayrı olan ekran, oturum değil: token seti ürünle aynı (mf_access /
// mf_refresh). İki ayrı set iki ayrı yenileme döngüsü demek olurdu ve 401
// sonrası hangisinin yenileneceği belirsizleşirdi; bir sekmede yapılan çıkış
// diğerinde yarım oturum bırakırdı.
//
// Kayıt bağlantısı yok ve bilerek yok: bu ekranın işi hesap açmak değil.

import { useState } from "react";
import Link from "next/link";
import { useAuth } from "@/store/auth";
import { ApiError } from "@/lib/api";

export function PanelLogin() {
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(email, password);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Bir şeyler ters gitti.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen grid place-items-center p-5">
      <div className="w-full max-w-sm">
        <div className="flex items-center gap-3 mb-6">
          <span
            className="grid place-items-center w-10 h-10 rounded-[var(--r-sm)] font-bold text-xs mono"
            style={{
              background: "linear-gradient(180deg, var(--brand-hi), var(--brand-lo))",
              color: "var(--brand-ink)",
              boxShadow: "var(--bevel), var(--shadow-2)",
            }}
          >
            MF
          </span>
          <div>
            <div className="font-display text-lg tracking-tight">Yönetim</div>
            <div className="text-xs" style={{ color: "var(--text-faint)" }}>
              MasterFabric
            </div>
          </div>
        </div>

        <form onSubmit={submit} className="card p-5 space-y-3.5 view-in">
          <label className="block">
            <span className="label">E-posta</span>
            <input
              className="input"
              type="email"
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </label>

          <label className="block">
            <span className="label">Parola</span>
            <input
              className="input"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </label>

          {error && <div className="notice notice-bad">{error}</div>}

          <button className="btn btn-primary w-full" disabled={busy} type="submit">
            {busy ? "Kontrol ediliyor…" : "Giriş yap"}
          </button>
        </form>

        <div className="mt-4 text-xs" style={{ color: "var(--text-faint)" }}>
          <Link href="/" className="hover:text-[var(--text-dim)]">
            ← Uygulamaya dön
          </Link>
        </div>
      </div>
    </div>
  );
}
```

- [x] **Step 2: Lint ve derleme**

```bash
cd mf-frontend
npm run lint
npm run build
```

Expected: temiz.

- [x] **Step 3: Commit**

```bash
git add mf-frontend/src/components/yonetim/PanelLogin.tsx
git commit -m "$(cat <<'EOF'
Give the panel its own door, not its own session

The screen is separate; the tokens are not. Two token sets would mean two
refresh cycles and no clear answer to which one a 401 should renew, and a logout
in one tab would leave half a session in the other.

No register link. This door is not where accounts are made.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Rota ve kapı

**Files:**
- Create: `mf-frontend/src/components/yonetim/PanelGate.tsx`
- Create: `mf-frontend/src/app/yonetim/layout.tsx`
- Create: `mf-frontend/src/app/yonetim/page.tsx`
- Create: `mf-frontend/src/app/yonetim/model/page.tsx`
- Create: `mf-frontend/src/app/yonetim/mcp/page.tsx`
- Create: `mf-frontend/src/app/yonetim/loglar/page.tsx`

**Interfaces:**
- Consumes: `panelGate` (Task 2), `PanelShell` (Task 4), `PanelLogin` (Task 5), `OverviewPanel` / `ModelPanel` / `MCPPanel` / `LogsPanel` (Task 3).
- Produces: `/yonetim`, `/yonetim/model`, `/yonetim/mcp`, `/yonetim/loglar` rotaları.

- [x] **Step 1: `PanelGate.tsx` yaz**

```tsx
"use client";

// panelGate'in kararını ekrana çeviren yer. Karar mantığı burada değil
// src/lib/adminAccess.ts'te — orası test edilebilir, burası değil.

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/store/auth";
import { panelGate } from "@/lib/adminAccess";
import { PanelShell } from "./PanelShell";
import { PanelLogin } from "./PanelLogin";

export function PanelGate({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const router = useRouter();
  const state = panelGate({ loading, user });

  // Yönlendirme render sırasında değil bir efektte: render sırasında router'a
  // dokunmak React'in aynı geçişte başka bir bileşeni güncellemesi demek.
  useEffect(() => {
    if (state === "redirect") router.replace("/");
  }, [state, router]);

  if (state === "booting" || state === "redirect") {
    return (
      <div className="min-h-screen grid place-items-center">
        <span
          className="mono text-xs tracking-wider uppercase"
          style={{ color: "var(--text-faint)" }}
        >
          oturum çözümleniyor
        </span>
      </div>
    );
  }

  if (state === "login") return <PanelLogin />;

  return <PanelShell>{children}</PanelShell>;
}
```

- [x] **Step 2: `src/app/yonetim/layout.tsx` yaz**

```tsx
import type { Metadata } from "next";
import { PanelGate } from "@/components/yonetim/PanelGate";

// Sekme başlığı ürününkinden ayrı: operatörün açık duran iki sekmesi
// birbirinden ayırt edilebilmeli.
export const metadata: Metadata = {
  title: "Yönetim — MasterFabric",
};

export default function YonetimLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <PanelGate>{children}</PanelGate>;
}
```

- [x] **Step 3: Dört sayfa dosyasını yaz**

`src/app/yonetim/page.tsx`:

```tsx
import { OverviewPanel } from "@/components/yonetim/OverviewPanel";

export default function YonetimGenel() {
  return <OverviewPanel />;
}
```

`src/app/yonetim/model/page.tsx`:

```tsx
import { ModelPanel } from "@/components/yonetim/ModelPanel";

export default function YonetimModel() {
  return <ModelPanel />;
}
```

`src/app/yonetim/mcp/page.tsx`:

```tsx
import { MCPPanel } from "@/components/yonetim/MCPPanel";

export default function YonetimMCP() {
  return <MCPPanel />;
}
```

`src/app/yonetim/loglar/page.tsx`:

```tsx
import { LogsPanel } from "@/components/yonetim/LogsPanel";

export default function YonetimLoglar() {
  return <LogsPanel />;
}
```

- [x] **Step 4: Lint, test ve derleme**

```bash
cd mf-frontend
npm run lint
npm test
npm run build
```

Expected: üçü de temiz. Build çıktısında `/yonetim`, `/yonetim/model`, `/yonetim/mcp`, `/yonetim/loglar` rotaları listelenmeli.

- [x] **Step 5: Uygulamayı gözle doğrula**

```bash
cd mf-frontend && npm run dev
```

Sırasıyla:
1. Oturumu kapat, `http://localhost:3000/yonetim` → panelin kendi giriş ekranı gelir, ürünün marka paneli gelmez.
2. Yönetici hesabıyla gir → panel açılır, solda dört bölüm, üstte "Yönetim / Genel".
3. Sidebar'dan Model, MCP ve Loglar'a geç → adres çubuğu `/yonetim/model` vb. olur, seçili bölüm işaretlenir, sayfa yenilemesi olmaz.
4. `/yonetim/model` adresini doğrudan yenile → doğru bölüm açık gelir.
5. Yönetici olmayan bir hesapla `/yonetim` → ürüne (`/`) döner.

- [x] **Step 6: Commit**

```bash
git add mf-frontend/src/app/yonetim mf-frontend/src/components/yonetim/PanelGate.tsx
git commit -m "$(cat <<'EOF'
Put the panel on real routes, so a section can be linked and refreshed

Four tabs behind one hash meant a bookmarked section was a hash fragment the
server never saw, and a refresh landed wherever the default was. Real routes
make each section addressable, refreshable, and separately loaded.

The gate renders the decision; the decision itself stays in src/lib, where the
test runner can reach it.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Ürün kabuğunu temizle ve `#admin`'i yönlendir

**Files:**
- Modify: `mf-frontend/src/components/AppShell.tsx`
- Delete: `mf-frontend/src/components/views/AdminView.tsx`

**Interfaces:**
- Consumes: `legacyHashToPath` (Task 1).
- Produces: değişiklik yok — bu görev yalnızca eski yüzeyi kaldırıyor.

- [x] **Step 1: `AdminView` importunu ve `admin` master görünümünü kaldır**

`AppShell.tsx` üzerinde:

1. `import { AdminView } from "./views/AdminView";` satırını sil.
2. `MasterView` birliğinden `"admin"` çıkar:

```tsx
export type MasterView =
  | "analiz" | "codegen" | "persona" | "metrics" | "gizlilik" | "kosullar";
```

3. `NAV` dizisinden `{ id: "admin", label: "Yönetim", Icon: IconSliders }` satırını sil.
4. Görünüm listesinden `{view === "admin" && <AdminView sub={sub} onNavigate={goSub} />}` satırını sil.
5. `IconSliders` fonksiyonunun tek çağıranı gitti. Sil — `IconCode`'dan farklı olarak bu ikonun geri gelecek bir rotası yok; panel artık kendi kabuğunda ve kendi işaretlemesini kullanıyor.

- [x] **Step 2: `NAV` üstündeki yorumu gerçeğe uydur**

`NAV` dizisinin üstündeki blok yorumda Yönetim'in nav'da neden listelendiğini anlatan iki cümle var ve artık doğru değil. O iki cümleyi şununla değiştir:

```tsx
// Yönetim artık burada değil: kendi rotasında, kendi kabuğunda (/yonetim).
// Nav'dan çıkması bir gizleme değil, bir ayrım — ürün ekranları bir vakayı
// değerlendirmek için, panel sistemi işletmek için, ve ikisi aynı başlık
// çubuğunu paylaşınca hangisinde olduğun kayboluyordu. Panele giden bağlantı
// header'da, yalnızca yönetici rolüne görünür.
```

- [x] **Step 3: `#admin` yönlendirmesini ekle**

`AppShell.tsx`'in import bloğuna:

```tsx
import { useRouter } from "next/navigation";
import { legacyHashToPath } from "@/lib/adminNav";
```

`AppShell` fonksiyonunun içinde, mevcut `useEffect(() => { const sync = …` bloğunun **üstüne**:

```tsx
  const router = useRouter();

  // #admin ölmedi, taşındı. Bağlantısı paylaşılmış olan kimse 404 görmesin —
  // bu reponun kuralı: nav'dan inen rota adreslenebilir kalır. Yönlendirme
  // hem ilk yüklemede hem sonraki hash değişimlerinde çalışıyor, çünkü ikisi
  // de aynı bağlantıya tıklamanın sonucu olabiliyor.
  useEffect(() => {
    const jump = () => {
      const target = legacyHashToPath(window.location.hash);
      if (target) router.replace(target);
    };
    jump();
    window.addEventListener("hashchange", jump);
    return () => window.removeEventListener("hashchange", jump);
  }, [router]);
```

- [x] **Step 4: Header'a panele giden bağlantıyı ekle**

`AppShell.tsx`'te header'daki e-posta `<span>`'inin **hemen öncesine**:

```tsx
            {user.role === "admin" && (
              <a
                href="/yonetim"
                className="btn btn-ghost btn-sm"
                title="Yönetim paneli"
              >
                Yönetim
              </a>
            )}
```

`<a>` kullanılıyor, `<Link>` değil: panel ayrı bir kabuk ve tam sayfa yüklenmesi doğru olan — ürünün açık kalan görünümlerini (mount edilmiş `Pane`'leri) panelin arkasında tutmanın bir anlamı yok.

- [x] **Step 5: `AdminView.tsx`'i sil**

```bash
git rm mf-frontend/src/components/views/AdminView.tsx
```

Bu dosya `codegen` gibi "nav'dan indi ama duruyor" durumunda değil: içeriğinin tamamı Task 3'te `src/components/yonetim/` altına taşındı ve geriye kalan kabuk `/yonetim` tarafından değiştirildi. Duran bir kopya, iki panelin sessizce ayrışması demek olurdu.

- [x] **Step 6: Lint, test ve derleme**

```bash
cd mf-frontend
npm run lint
npm test
npm run build
```

Expected: üçü de temiz. Lint'te kullanılmayan import veya fonksiyon uyarısı olmamalı (`IconSliders` silindi, `AdminView` importu silindi).

- [x] **Step 7: Uygulamayı gözle doğrula**

```bash
cd mf-frontend && npm run dev
```

1. Yönetici hesabıyla `http://localhost:3000/#admin` → `/yonetim`'e döner.
2. `http://localhost:3000/#admin/logs` → `/yonetim/loglar`'a döner.
3. `http://localhost:3000/#analiz` → ürün açık kalır, hiçbir yönlendirme olmaz.
4. Ürün header'ında yönetici için "Yönetim" bağlantısı görünür; yönetici olmayan hesapta görünmez.
5. Ürün nav'ında dört değil üç sekme var: Analiz, Persona, Metrikler.

- [x] **Step 8: Commit**

```bash
git add -A mf-frontend/src
git commit -m "$(cat <<'EOF'
Take the panel out of the product nav without breaking a single old link

Operating the system and evaluating a case were sharing a title bar, and the
tab strip made them look like the same task at four levels of detail. They are
not, so the panel now lives at its own address.

#admin and #admin/logs still resolve — they redirect. Links to them have been
shared, and a route that leaves the nav stays addressable here.

AdminView.tsx is deleted rather than parked: every line of it now lives under
components/yonetim, and a second copy would only drift.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 8: Aşamayı kapat

**Files:**
- Modify: `docs/superpowers/plans/2026-08-04-yonetim-paneli-kabuk.md` (bu dosya — kutucuklar işaretlenir)

**Interfaces:**
- Consumes: Task 1–7'nin tamamı.
- Produces: sevk edilebilir bir 1. aşama.

- [x] **Step 1: Tam doğrulama**

```bash
cd mf-frontend
npm test
npm run lint
npm run build
```

Expected: üçü de sıfır hata. `npm test` çıktısında `adminNav.test.ts` ve `adminAccess.test.ts` testleri geçmiş görünmeli; mevcut `terms`, `report`, `rubric`, `verdict` testleri de.

- [x] **Step 2: Backend'e dokunulmadığını doğrula**

```bash
cd /Users/emrah/dev/mf-capstone
git diff main --stat -- mf-backend
```

Expected: **boş çıktı.** Bu aşama yalnızca frontend. Backend'de bir değişiklik varsa kapsam dışına çıkılmış demektir.

- [x] **Step 3: Kalan referansları tara**

```bash
cd /Users/emrah/dev/mf-capstone
grep -rn "AdminView\|#admin" mf-frontend/src
```

Expected: yalnızca `src/lib/adminNav.ts` (eşleme tablosu), `src/lib/adminNav.test.ts` (testleri) ve `src/components/AppShell.tsx` (yönlendirme yorumu) eşleşmeli. `AdminView` adının hiçbir import'ta kalmamış olması gerekiyor.

- [x] **Step 4: Deploy notu**

Bu aşama **yalnızca frontend** ve hiçbir yeni backend ucu okumuyor, dolayısıyla tek başına Vercel'e sevk edilebilir; backend'i beklemez.

Sevk edildikten sonra üretimde doğrulanacaklar: `/yonetim` açılıyor, `#admin` yönleniyor, yönetici olmayan bir hesap panele giremiyor.

**`render.yaml` `autoDeploy: true` diyor ama GitHub webhook'u pratikte ateşlemiyor.** Bu aşamada Render'a giden bir değişiklik yok; yine de bir sonraki aşamada backend değişecek ve deploy elle tetiklenip doğrulanacak. Doğrulanmadan hiçbir şey "çıktı" diye raporlanmayacak.

- [x] **Step 5: Planın kutucuklarını işaretle ve commit'le**

```bash
cd /Users/emrah/dev/mf-capstone
git add docs/superpowers/plans/2026-08-04-yonetim-paneli-kabuk.md
git commit -m "$(cat <<'EOF'
Close out phase one of the panel: the move, with nothing else in it

Every panel that worked before works now, at a real address, in a shell of its
own. No feature was added and none was lost, which is what made this phase
worth separating from the four that follow.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Sonraki aşamalar

Bu plan spec'in 1. aşamasını kapsıyor. Kalanlar sırayla ayrı planlar olacak
([spec §8](../specs/2026-08-04-yonetim-paneli-design.md)):

2. **Hesaplar** — migration 012, org modeli, hesap açma, geçici parola akışı.
3. **Panel** — `GET /admin/stats`, chart'lar. Chart koduna başlamadan önce `dataviz` skill'i okunacak.
4. **Belgeler** — migration 013, seed, public uç, editör, onay kapısı.
5. **Denetim ve saklama** — migration 014, denetim kaydı, saklama ayarı, hesap silme.
