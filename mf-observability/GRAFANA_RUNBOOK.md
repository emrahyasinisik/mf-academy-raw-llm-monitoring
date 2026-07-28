# Grafana'yı tünelden yayına alma — GPU kutusu kılavuzu

Admin panelindeki Grafana linkini çalışır hale getiren tek oturumluk kılavuz.
Baştan sona kutuda koşulur; son adım Vercel'de.

> **Yalnız Metrikler sekmesi lazımsa** adım 2 ve 4 yeterli: uygulamadaki
> grafikler Prometheus'u gateway üzerinden okuyor, o da `mf-edge` ağını ve
> yeniden oluşturulmuş konteynerleri istiyor. Cloudflare adımlarının (1, 6, 7)
> tamamı Grafana'nın kendi arayüzünü yayına almak içindir — grafikler onlarsız
> da çalışır, çünkü tünelde zaten `mlc` hostname'i var.

Docker komutları **WSL2 içindeki bash'te** çalışır, PowerShell'de değil —
[`peft/PERSONA_RUNBOOK.md`](../mf-inference/peft/PERSONA_RUNBOOK.md) ile aynı
kural. Docker Desktop açık olmalı.

Neden gömme değil de link, ve mimarinin gerekçesi:
[`README.md`](README.md#reaching-grafana-from-the-admin-ui).

**Ön koşullar:** tünel zaten `mlc.<alan-adı>`'nı servis ediyor olmalı
(`--profile tunnel`), alan adı Cloudflare'de bir zone olmalı.

---

## 1. Access uygulamasını hostname'den ÖNCE oluştur

Sıra tersine dönerse, aradaki sürede Grafana'nın login sayfası internete açık
kalır ve arada duran tek şey `GRAFANA_PASSWORD` olur. Cloudflare, henüz
yönlendirilmemiş bir hostname için uygulama tanımlamana izin veriyor —
kapıyı, kapıdan geçilecek yolu açmadan önce kur.

Zero Trust → **Access → Applications → Add an application → Self-hosted**

| alan | değer |
|---|---|
| Application name | `grafana` |
| Subdomain / Domain | `grafana` / `<alan-adın>` |
| Policy name | `admin-only` |
| Action | **Allow** |
| Include | **Emails** → kendi e-postan |

Politikayı `Everyone` bırakma. Access'in varsayılan davranışı sadece kimlik
doğrulamaktır — kim olduğunu bilmek yetkilendirmek değildir.

## 2. Ortak ağı oluştur

```bash
docker network create mf-edge
```

Bir kez. İki compose projesi de bu ağa `external` olarak bakıyor, yani onu
hiçbiri sahiplenmiyor: birinde `docker compose down` çalıştırmak diğerinin
altından ağı çekmiyor. Zaten varsa Docker "already exists" der, sorun değil.

## 3. `GRAFANA_ROOT_URL`'ü ayarla

`mf-observability/.env` içine:

```
GRAFANA_ROOT_URL=https://grafana.<alan-adın>
```

Grafana yönlendirmelerini ve paylaşım linklerini bu değerden kuruyor. Boş
bırakılırsa varsayılan `http://localhost:3001` kalır ve tünelden gelen bir
ziyaretçiyi login'den sonra **kendi makinesine** atar — hata vermez, sadece
çalışmaz.

> **Windows tuzağı:** `.env`'i Notepad ile kaydedersen dosyanın başına BOM
> ekleyebilir ve ilk değişken adı okunamaz hale gelir. VS Code'da düzenle ve
> sağ alttaki kodlamanın **UTF-8** olduğundan emin ol, `UTF-8 with BOM` değil.

## 4. İki yığını da yeniden oluştur

Ağ üyeliği konteyner oluşturulurken belirlenir; `restart` yetmez, `up -d`
gerekiyor.

```bash
cd mf-observability && docker compose up -d
cd ../mf-inference   && docker compose --profile tunnel up -d
```

`--profile tunnel` şart: o olmadan `cloudflared` compose'un gözünde yoktur ve
eski ağıyla çalışmaya devam eder — bu, adım 6'da "grafana bulunamadı" olarak
geri döner.

## 5. Ağı doğrula — hostname eklemeden önce

```bash
docker network inspect mf-edge --format '{{range .Containers}}{{.Name}}{{"\n"}}{{end}}'
```

Listede `grafana`, `cloudflared` ve `gateway` olmalı — sonuncusu uygulamadaki
Metrikler sekmesi için, Prometheus'u o okuyor. Hepsi aynı ağdaysa isim
çözümlemesi çalışır; kabuk gerektirmeden bunu doğrulamanın yolu bu.

Biri eksikse adım 4'ü o proje için tekrarla.

Metrik yolunu da burada dene — anahtar `mf-inference/.env`'deki `LLM_API_KEY`:

```bash
curl -s -H "X-API-Key: $LLM_API_KEY" \
  'http://127.0.0.1:8080/prom/api/v1/query?query=up' | head -c 200
```

`"status":"success"` bekleniyor. `401` anahtarın yanlış olduğunu, `502`
gateway'in `prometheus`'u çözemediğini (adım 4'te gateway yeniden
oluşturulmamış), `403` ise izin verilen iki uçtan biri dışında bir yol
denendiğini söyler.

## 6. Public hostname'i ekle

Zero Trust → **Networks → Tunnels** → `mlc`'yi servis eden tünel →
**Public Hostname → Add**

| alan | değer |
|---|---|
| Subdomain | `grafana` |
| Domain | `<alan-adın>` |
| Type | `HTTP` |
| URL | `grafana:3000` |

`grafana:3000`, `localhost:3001` değil. Tünel konteynerin içinden bakıyor:
oradaki `localhost` cloudflared'in kendisi, `3001` ise yalnız Windows'un
loopback'inde var.

## 7. Doğrula

Gizli pencerede `https://grafana.<alan-adın>`:

- **Cloudflare Access ekranı geliyorsa** doğru. E-postanı gir, kod gelsin,
  ardından Grafana açılsın.
- **Doğrudan Grafana login'i geliyorsa dur.** Politika bu hostname'e bağlanmamış
  demektir. Public hostname'i hemen sil, adım 1'e dön; o ekran açıkken pano
  şifresi tek savunma.

Grafana açıldıktan sonra bir panoya gir ve URL'in `grafana.<alan-adın>` üzerinde
kaldığını gör — `localhost:3001`'e atıyorsa adım 3 uygulanmamış ya da konteyner
yeniden oluşturulmamıştır.

## 8. Linki UI'da aç

Vercel → proje ayarları → Environment Variables:

```
NEXT_PUBLIC_GRAFANA_URL = https://grafana.<alan-adın>
```

Sonra **yeniden deploy et**. `NEXT_PUBLIC_*` build zamanında gömülüyor, restart
değeri değiştirmez. Değişken boşken kart hiç render edilmiyor, yani bu adımı
atlamak paneli bozmaz — sadece link çıkmaz.

---

## Ne bozulur, nasıl anlarsın

| belirti | sebep |
|---|---|
| `network mf-edge declared as external, but could not be found` | Adım 2 atlanmış |
| Tünel logunda `dial tcp: lookup grafana ... no such host` | `cloudflared` eski ağıyla koşuyor — adım 4, `--profile tunnel` ile |
| Access yerine Grafana login'i | Politika hostname'e bağlı değil — adım 7'deki uyarı |
| Login sonrası `localhost:3001`'e yönlenme | `GRAFANA_ROOT_URL` yok ya da grafana yeniden oluşturulmamış |
| Panolar açılıyor ama boş | Grafana'nın sorunu değil: `mf-backend-local` hedefi bu kutuda **bilerek** down. [`README.md`](README.md#verify) |
| Kart admin panelinde görünmüyor | `NEXT_PUBLIC_GRAFANA_URL` yok ya da deploy edilmemiş |

## Geri alma

Public hostname'i Cloudflare'den sil — Grafana o anda dışarıdan erişilemez hale
gelir, `127.0.0.1:3001` çalışmaya devam eder. `mf-edge` ağını ve compose
değişikliklerini bırakabilirsin; ağ tek başına hiçbir şeyi yayına almıyor,
yayına alan tek şey hostname'di.
