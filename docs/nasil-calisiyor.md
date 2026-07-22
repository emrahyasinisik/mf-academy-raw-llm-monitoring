# Nasıl çalışıyor? — soru soru sistem turu

Bu doküman sistemi öğrenmek için yazıldı. Kurulum talimatları için ilgili
klasörlerin `README.md` dosyalarına bak; burada **"bu parça neden var ve nerede
duruyor"** sorusunun cevabı var.

---

## 1. En baştan: bir prompt yazdığımda ne oluyor?

İki yol var, kullanıcı hangisini seçtiyse.

**Tarayıcı yolu** (eski, hâlâ duruyor):

```
Tarayıcı (WebGPU'da model koşar) → sonucu backend'e POST eder → kaydedilir
```

**Sunucu yolu** (yeni):

```
Tarayıcı → Render'daki Go backend → Cloudflare tüneli → senin Windows PC'n
        → Caddy gateway → MLC container → GPU → aynı yoldan geri
```

Fark şu: birincisinde model **kullanıcının** kartında koşar ve backend sadece
sonucu kaydeder. İkincisinde model **senin** kartında koşar, backend üretimi de
yapar.

İkisini birden tuttuk. Sebebi: aynı model id'si iki farklı donanımda koşunca
latency'leri karşılaştırabiliyoruz — projenin en değerli verisi bu.

---

## 2. Docker bağlantısı nerede? Kaç tane compose dosyası var?

**İki ayrı compose projesi** var ve ikisi de senin Windows PC'nde koşuyor:

| Dosya | Ne koşturuyor |
|---|---|
| `mf-inference/docker-compose.yml` | `mlc`, `gateway`, `cloudflared` |
| `mf-observability/docker-compose.yml` | `prometheus`, `loki`, `alloy`, `grafana` |

Ayrı olmalarının sebebi: biri modeli servis ediyor, diğeri onu izliyor. İzleme
çökerse servis etkilenmemeli.

**Peki ayrı projelerdeki container'lar nasıl haberleşiyor?**

Genelde haberleşmiyorlar — ve bu kasıtlı. Tek istisna Alloy: o, ağ üzerinden
değil **Docker soketi** üzerinden çalışıyor (`/var/run/docker.sock`), yani
daemon'a soruyor. Daemon bütün container'ları bildiği için Alloy `mf-inference`
projesindeki logları da görebiliyor.

Bu satır o yüzden önemli (`mf-observability/docker-compose.yml`):

```yaml
- /var/run/docker.sock:/var/run/docker.sock:ro
```

`:ro` = read-only. Yazılabilir olsaydı o container Docker daemon'ını kontrol
edebilirdi — yani makinenin tamamını.

---

## 3. Model Docker'a nasıl yüklendi?

Model **image'ın içinde değil.** Bu önemli bir tasarım kararı.

`mf-inference/mlc/Dockerfile` sadece **çalıştırma ortamını** kuruyor:

1. `FROM nvidia/cuda:12.8.1-devel-ubuntu22.04` — CUDA'lı taban
2. Miniforge ile conda ortamı, Python 3.10
3. `pip install mlc-llm-cu128 mlc-ai-cu128` — MLC motoru

Model ağırlıkları ise **ilk çalıştırmada indiriliyor**. Komut
`docker-compose.yml`'de:

```yaml
command:
  - python3
  - -m
  - mlc_llm
  - serve
  - HF://mlc-ai/gemma-2-2b-it-q4f16_1-MLC   # <-- burada
```

`HF://` öneki "bunu Hugging Face'ten indir" demek. ~1.5 GB'lık ağırlıklar
iniyor ve şuraya yazılıyor:

```yaml
volumes:
  - model-cache:/cache
```

**Neden image'a gömmedik?** Üç sebep:

- Image 1.5 GB şişerdi, her rebuild'de yeniden kopyalanırdı
- Modeli değiştirmek için image'ı yeniden inşa etmen gerekirdi; şimdi `.env`'de
  `MLC_MODEL` değiştirmek yetiyor
- Volume'da olduğu için container silinse bile ağırlıklar kalıyor

**Bir de JIT derleme var.** MLC ilk istekte CUDA kernel'lerini senin kartının
mimarisine (sm_75) göre derliyor. `nvcc` gerektiği için taban image `runtime`
değil `devel` olmak zorunda — bunu ilk denemede yanlış yazmıştık, container
sürekli restart döngüsüne girmişti.

---

## 4. Go backend MLC'ye nasıl bağlanıyor?

`mf-backend/internal/llm/provider.go`

Kilit fikir: **`mlc_llm serve` OpenAI uyumlu bir API konuşuyor.** Yani backend
için MLC özel bir şey değil, sıradan bir HTTP endpoint'i.

```go
url := p.baseURL + "/v1/chat/completions"
```

Adres `LLM_BASE_URL` ortam değişkeninden geliyor (Render'da
`https://mlc.visevent.com`). Bunun pratik sonucu: inference'ı başka bir yere
taşımak **kod değişikliği değil, env var değişikliği**. Yarın Groq'a ya da
OpenAI'a geçsen bu dosyaya dokunmazsın.

**İki auth header'ı birden gönderiyoruz:**

```go
httpReq.Header.Set("X-API-Key", p.apiKey)
httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
```

Bizim Caddy gateway'i `X-API-Key` okuyor; hosted bir sağlayıcı `Authorization`
okur. İkisini birden yollamak `LLM_BASE_URL`'i takas edilebilir tutuyor.

---

## 5. Tünel nasıl çalışıyor? Router'da port açtık mı?

Hayır, açmadık. Ve açamazdık — çoğu ISS CGNAT kullanıyor.

`cloudflared` container'ı **içeriden dışarıya** bağlantı kuruyor. Cloudflare
`mlc.visevent.com`'a gelen isteği o açık bağlantı üzerinden içeri yolluyor.

```
İnternet → Cloudflare → (kurulu tünel) → cloudflared → gateway → mlc
```

Windows'un kendi public adresini bilmesi gerekmiyor. Hostname bilgisi
Cloudflare'in tarafındaki "route" tanımında duruyor, compose dosyasında değil.

**Neden Cloudflare'de Bot Fight Mode sorun çıkardı?** Cloudflare, Render'ın veri
merkezi IP'sinden gelen isteği bot sanıp "Just a moment..." challenge sayfası
döndürdü. Go client'ı JavaScript çalıştıramadığı için geçemedi. Çözüm: WAF skip
kuralı + client'a düzgün bir `User-Agent` vermek.

---

## 6. Gateway ne iş yapıyor? Neden araya bir Caddy koyduk?

`mf-inference/gateway/Caddyfile`

**Çünkü `mlc_llm serve`'ün kendi kimlik doğrulaması yok.** Tünel adresini bilen
herkes senin kartında model koşturabilirdi.

Caddy üç iş yapıyor:

1. **Auth** — `X-API-Key` yoksa 401
2. **Load balancing** — istekleri replikalara dağıtıyor
3. **Timeout yönetimi** — uzun üretimlerin kesilmemesi için

`mlc` servisi hiçbir host portu açmıyor (`expose: 8000`, `ports:` yok). Yani
dışarıdan tek giriş gateway'den. Bu, "secret'ı atlamak" diye bir ihtimali
ortadan kaldırıyor.

---

## 7. Yük dengeleme tam olarak nasıl oluyor?

İki parça:

**a) Kaç replika var?**

```powershell
docker compose up -d --scale mlc=2
```

Docker bunlara `mf-inference-mlc-1`, `mf-inference-mlc-2` diyor ve ikisine de
`mlc` DNS adını veriyor.

**b) Caddy bunları nasıl buluyor?**

```
dynamic a {
    name mlc
    port 8000
    refresh 10s
}
lb_policy round_robin
```

`dynamic a` kritik. Düz `reverse_proxy mlc:8000` yazsaydık Caddy DNS'i **sadece
açılışta** çözerdi ve sonradan eklenen replikaları hiç görmezdi — çalışıyormuş
gibi görünüp tek replikaya gitmeye devam ederdi.

**Neden `round_robin`, `least_conn` değil?**

`least_conn` ("en boştakine gönder") teoride daha iyi, çünkü üretim süreleri çok
değişken. Ama denedik ve **çalışmadı**: 3 saniye boyunca çakışan istekler bile
hep ilk replikaya gitti. Sebep muhtemelen `least_conn`'un upstream başına aktif
bağlantı sayısı tutması, `dynamic a`'nın ise listeyi sürekli yeniden üretmesi —
sayaçlar birikmiyor.

`round_robin` sayaç tutmuyor, sadece sırayla dönüyor. Kusuru var (kısa bir istek
uzun birinin arkasına düşebilir) ama en azından ikinci replikayı kullanıyor.

**Bir zayıflık:** Caddy'nin aktif health check'leri dinamik upstream'lerde
çalışmıyor. Yeni başlamış, hâlâ model yükleyen bir replika rotasyonda kalıyor ve
502 dönüyor. Pasif kontrol (`fail_duration`) onu 30 saniyeliğine parkediyor,
yani sistem kendini toparlıyor — ama ölçeklemek bedava değil.

---

## 8. Metrikler nereden geliyor? Grafana backend'e mi bağlanıyor?

**Hayır.** En sık yapılan hata bu. Zincir şöyle:

```
backend /metrics  ←(scrape)—  Prometheus  ←(query)—  Grafana
```

Grafana backend'e **hiç dokunmuyor**. Prometheus 15 saniyede bir gidip
`/metrics`'i okuyor (pull), Grafana da Prometheus'a soruyor.

Uygulama hiçbir yere veri **göndermiyor** — sadece sorulduğunda cevap veriyor.
Bunun güzel sonucu: izleme sistemi tamamen çökse backend bunu fark etmez bile.

**Metrikler nerede üretiliyor?**

- `mf-backend/internal/common/metrics.go` — HTTP tarafı (istek sayısı, latency
  histogramı, anlık istek sayısı)
- `mf-backend/internal/llm/metrics_prom.go` — üretim tarafı (süre, token, hata)

**Kardinalite tuzağı — bunu bilmen lazım:**

```go
route := chi.RouteContext(r.Context()).RoutePattern()  // "/llm/runs/{id}"
```

Ham path (`/llm/runs/abc-123`) etiket olarak kullanılsaydı, her run id'si için
yeni bir zaman serisi oluşurdu. Bin kullanıcı × bin run = bir milyon seri ve
Prometheus ölür. Onun yerine **route deseni** kullanıyoruz — kaç run olursa
olsun tek seri.

Eşleşmeyen istekler de `route="unmatched"`'e düşüyor, yoksa rastgele URL tarayan
bir bot aynı patlamaya yol açardı.

**Neden `/metrics` token'lı?** Çünkü o çıktı servisin haritası: hangi
endpoint'ler var, ne kadar trafik alıyorlar, ne kadar sürüyorlar. Saldırgan için
bedava keşif bilgisi.

---

## 9. Loglar Grafana'ya nasıl gidiyor?

Metriklerin tersine, loglar **push** ediliyor — ama uygulamalar tarafından
değil.

```
container'lar → (stdout) → Docker → Alloy okur → Loki'ye push → Grafana sorar
```

`mf-observability/alloy/config.alloy`:

```
discovery.docker "containers" {
    host = "unix:///var/run/docker.sock"
}
```

Alloy Docker soketinden container'ları keşfediyor ve loglarını okuyor.
**Uygulamalar hiç değişmedi** — hâlâ stdout'a yazıyorlar, Loki'den haberleri
yok.

Etiketler: `container` (mlc-1 / mlc-2 ayrımı), `service` (hepsi birden),
`project`. Burada da kardinalite kuralı geçerli — container **id**'si etiket
olsaydı her yeni container yeni bir stream açardı.

Grafana'da sorgular:

```logql
{service="mlc"}                          # tüm replikalar
{container="mf-inference-mlc-2"}         # tek replika
{service="gateway"} |= "401"             # reddedilen istekler
```

**Promtail değil Alloy kullandık** çünkü Promtail 2 Mart 2026'da emekli oldu.

---

## 10. Frontend hangi yolu seçeceğini nasıl biliyor?

`mf-frontend/src/components/views/PlaygroundView.tsx`

Backend `GET /llm/models` çağrısında iki şey söylüyor:

```json
{
  "models": [{ "id": "gemma-...", "targets": ["browser", "server"] }],
  "server_inference": true
}
```

- `server_inference` — bu deployment'ta inference host bağlı mı
- `targets` — bu model nerede koşabilir

Seçim kartı **sadece ikisi de uygunsa** görünüyor. Değilse hiç gösterilmiyor —
başarısız olacak bir butonu sunmaktansa saklamak daha doğru.

Hedef `server` ise `POST /llm/generate`, `browser` ise eski akış: tarayıcıda üret
→ `POST /llm/runs`.

---

## 11. Veritabanında ne değişti?

`mf-backend/migrations/003_run_target.sql` — `llm_runs` tablosuna `target`
sütunu eklendi (`browser` | `server`).

**Neden metadata JSON'ına gömmedik?** Çünkü bu sütun gruplanıp filtreleniyor.
JSON içinden çıkarmak hem yavaş hem indexlenemez olurdu.

**Neden bu sütun bu kadar önemli?** Çünkü iki hedefin latency'si aynı şeyi
ölçmüyor:

- Tarayıcı ölçümü = ziyaretçinin ekran kartı
- Sunucu ölçümü = senin kartın + ağ gecikmesi

İkisini aynı ortalamada toplarsan hiçbirini tarif etmeyen bir sayı elde
edersin. Ölçtük: 2 tarayıcı run'ı 1000ms, 2 sunucu run'ı 500ms → karışık
ortalama 750ms, yani doğru olmayan bir sayı.

O yüzden `GET /llm/metrics` artık `by_target` da dönüyor.

---

## 12. Timeout'lar neden route'a göre farklı?

`mf-backend/internal/llm/routes.go`

Çoğu endpoint tek bir veritabanı sorgusu — milisaniyeler sürer, 5 saniyelik
sınır yeterli ve gereklidir (takılan sorgu havuz bağlantısını tutmasın diye).

Ama `/llm/generate` bir GPU'yu bekliyor, üstelik tünelin arkasında. Ona 25
saniye lazım.

**Buradan çıkan ders:** Go'da **çocuk context ebeveynin deadline'ını
uzatamaz.** İlk yazdığımızda global 5 saniyelik timeout kök router'daydı ve
route'a koyduğumuz 25 saniye hiç devreye girmedi — istekler tam 5001ms'de
ölüyordu, MLC de onları "cancelled" olarak logluyordu.

Çözüm: kısa sınırı köke değil **alt ağaçlara** uygulamak, `/llm`'i o gruptan
dışarıda tutmak.

---

## 13. Hangi ölçümleri elde ettik?

| Ölçüm | Değer |
|---|---|
| Yerel GPU, tek istek | ~670ms (72 token) → ~107 token/sn |
| Render üzerinden, tek istek | ~1817ms → aradaki fark ağ gecikmesi |
| Tek replika, 1 eşzamanlı | 1.9s |
| Tek replika, 3 eşzamanlı | ~24s (her biri) |
| İki replika VRAM | 5.4 / 6.1 GB |

Son satır önemli: iki replika ağırlıkları **iki kez** yüklüyor. 6 GB kartta
ikisine de batch yapacak yer kalmıyor. Yani tek GPU'da replika ile ölçeklemek
ters tepiyor — doğru eksen süreç içi batching.

---

## 14. Neyi yapmadık?

Dürüst olmak için:

- **Otomatik ölçekleme yok.** Spec "aşırı kullanımda ölçeklensin" diyor; bizde
  ölçekleme elle. Tetikleyici yazmadık — çünkü yukarıdaki ölçüm replika
  eklemenin bu donanımda faydasız olduğunu gösteriyor.
- **Kubernetes yok.** "Pod scaling" k8s terimi; bizde container ölçekleme var.
- **Tarayıcı MLC'si kaldırılmadı.** Bilerek: karşılaştırma verisi üretiyor.
- **Web LLM eş zamanlı yükleme koruması eksik.** Load butonu yükleme sürerken
  hâlâ tıklanabiliyor.
