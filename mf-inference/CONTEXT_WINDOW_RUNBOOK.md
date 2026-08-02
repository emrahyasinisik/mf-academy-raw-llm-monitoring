# Bağlam penceresini geri açmak

**2 Ağu 2026, kutuda koşulacak.** Ürünün kendi ana akışı şu an çalışmıyor:
örnek vaka rubrikle birlikte 2015 token ediyor, motor 1416'da kesiyor.

```
inference host rejected the request: Request prompt has 2015 tokens in total,
larger than the model input length limit 1416.
```

Mac'ten iki kez ölçüldü, 1416 sabit. Bu bir kapasite sınırı değil, **yürürlükte
olmayan bir override**: `docker-compose.yml`'in kendi notu, 4B build'in kendi
haline bırakıldığında 1366 verdiğini ve `max_num_sequence=1`'in KV cache'in
tamamını tek isteğe vererek bunu ~4 katına çıkardığını yazıyor. Gelen 1416,
override'lı değere değil override'sız değere komşu.

Kaybedilen şey küçük değil: 9 kriterli yatırım rubriğinin sistem promptu tek
başına ~873 token. 1416'lık pencerede vakaya kalan ~480 token, yani ~1500
karakter. Gerçek bir deck oraya sığmaz — ne örnek rapor üretilebilir, ne
tutarlılık sayısı ölçülebilir.

Komutlar **PowerShell**, Docker Desktop WSL2 arka ucuyla. Hepsi
`mf-inference/` içinden.

---

## 0. Ne yüklü, kartta ne var

```powershell
cd C:\...\mf-capstone\mf-inference     # kutudaki yol neyse
nvidia-smi
docker compose ps
```

`nvidia-smi`'de kartı kimin tuttuğuna bak. `mlc` ve `llamacpp` aynı anda
ayaktaysa ikisi de 6 GB'tan yiyor ve KV cache'e kalan yer o kadar azalıyor.

## 1. Motorun ne verdiğini oku

Asıl sayı burada, tahmin edilecek bir şey yok:

```powershell
docker compose logs mlc | Select-String max_single_sequence_length
```

`grep` yerine `Select-String` — compose dosyasındaki yorum bash yazımıyla,
kutuda çalışan bu.

- **~1400 çıkıyorsa** override yürürlükte değil, 2. adıma geç.
- **~5400 çıkıyorsa** override yürürlükte ve sorun başka yerde; 6. adıma atla ve
  bana o sayıyı söyle.

## 2. `MLC_OVERRIDES` gerçekten ne?

compose `${MLC_OVERRIDES:-max_num_sequence=1}` yazıyor, yani **değişken set
değilse** doğru değer kullanılıyor. Set edilmişse compose'un varsayılanı devre
dışı kalır ve orada ne yazıyorsa o geçer:

```powershell
Select-String MLC_OVERRIDES .env
```

Çıktı varsa ve içinde `max_num_sequence=1` yoksa, sorunun kaynağı bu satır.
Ya satırı sil ya da `MLC_OVERRIDES=max_num_sequence=1` yap.

## 3. Kartı boşalt

```powershell
docker compose stop llamacpp
nvidia-smi                       # llamacpp'nin yeri boşaldı mı, gözle doğrula
```

`llamacpp` Gemma GGUF hattı; rubrik ürünü ona dokunmuyor. Durdurulması
gateway'in `/llamacpp` yolunu cevapsız bırakır, MLC yolunu etkilemez. Ölçüm
bitince 7. adımda geri açılıyor.

## 4. MLC'yi yeniden kur

```powershell
docker compose up -d --force-recreate mlc
docker compose logs -f mlc          # "Loading model" bitene kadar, ~1-2 dk
```

`--force-recreate` şart: `up -d` tek başına, konteyner zaten ayakta ve tanım
değişmemişse hiçbir şey yapmadan çıkar — override `.env`'den geliyorsa tanım
"değişmiş" sayılmayabilir ve komut sessizce başarılı olur.

## 5. Yeniden oku

```powershell
docker compose logs mlc | Select-String max_single_sequence_length
```

**Beklenen mertebe ~5400.** Kesin sayı kartta kalan yere göre oynar; önemli olan
1416'nın katları mertebesine çıkmış olması. Hâlâ ~1400 ise durma noktası burası,
bana logun o bölümünü gönder — override'ın uygulanmadığını gösteren başka bir
şey var demektir.

## 6. Hangi build yüklü — atlanmaması gereken adım

```powershell
curl.exe -s -H "X-API-Key: $env:LLM_API_KEY" http://127.0.0.1:8080/v1/models
```

Mac'ten şu an dönen cevap:

```json
{"id": "/models/qwen3-4b-flutter-q4f16_1-MLC", "owned_by": "MLC-LLM"}
```

Yani kutu **Flutter adapter'ının build'ini** servis ediyor. Rubrik ürününün
istekleri de ona gidiyor, çünkü `mlc_llm` isteğin `model` alanını doğrulamıyor:
tek yüklü modelden cevap verir ve sorulan id'yi olduğu gibi geri yazar. Ölçülen
tutarlılık sayısı, hangi build yüklüyse **onu** tarif eder.

Satış materyaline girecek rakam için doğru olan taban build. `models/` altında
ne olduğuna bak:

```powershell
dir models
```

Rubrik/taban bir build varsa `.env`'de `MLC_MODEL` onu gösterecek şekilde
ayarlanıp 4. adım tekrarlanmalı. Yoksa ölçümü yine de alırız ama rakamın yanına
"Flutter build'i üzerinde ölçüldü" yazmak zorunda kalırız — ve o dipnot, sayıyı
satış materyalinde kullanılamaz hale getirir. Bu adımda ne gördüğünü bana yaz,
kararı birlikte verelim.

## 7. Uçtan uca doğrula

Önce kutudan, gateway üzerinden — 1416'yı aşan bir prompt artık geçmeli:

```powershell
curl.exe -s -X POST http://127.0.0.1:8080/v1/chat/completions `
  -H "X-API-Key: $env:LLM_API_KEY" -H "Content-Type: application/json" `
  -d '{\"model\":\"x\",\"messages\":[{\"role\":\"user\",\"content\":\"tek kelime: tamam\"}],\"max_tokens\":8}'
```

400 yerine bir cevap dönüyorsa yol açık. Gerisini ben Mac'ten yaparım:
`POST /analysis/run` örnek vakayla geçtiği anda `POST /analysis/trial` 5 tekrar
koşar, `stddev_score` ve `per_criterion_stddev` çıkar.

## 8. Bitince kartı geri ver

Ölçüm tamamlandıktan **sonra**:

```powershell
docker compose start llamacpp
docker compose ps
```

---

## Tuzaklar

- **`up -d` yeniden kurmaz.** Yalnızca `--force-recreate` kurar. Bu runbook'un
  var olma sebebi büyük ihtimalle tam olarak bu: override eklendi, `up -d`
  koşuldu, konteyner değişmedi ve kimse bir daha bakmadı.
- **Sayıyı logdan oku, hesaplama.** Motor kartta kalan yere göre karar veriyor;
  aynı override farklı bir anda farklı bir pencere verebilir.
- **`LLM_MAX_PROMPT_TOKENS` (backend, şu an 1200) motorun verdiği pencereyi
  aşmamalı.** Pencere büyüdükten sonra Render'da bu değeri de yükseltmek
  gerekiyor, yoksa persona hattı kendini gereksiz yere 1200'e kırpmaya devam
  eder. Analiz yolu bu değere zaten hiç bakmıyor — ayrı bir kusur, notu bende.
- **`mlc_llm` yanlış model id'sini hata saymaz.** 6. adım bu yüzden var: yanlış
  build yüklüyken her şey çalışır, yalnızca kayıtlar ve grafikler başka bir
  modeli işaret eder.
- **Tünel bu işin parçası değil.** `mlc.visevent.com` şu an sağlıklı (200);
  sorun pencerede, erişimde değil. Tünel profillerine dokunma.

## Bu düzeldiğinde ne açılıyor

Go-to-market sırasının 1. ve 3. kalemi, ikisi de buna bağlı: bir kusursuz örnek
rapor gerçek bir deck'le ancak pencere yeterse üretilebiliyor, tutarlılık sayısı
da o raporun vakası üzerinde ölçülüyor. Makine hazır — `POST /analysis/trial`,
`GET /analysis/trials/{group}`, `PerCriterionStdDev` — eksik olan tek şey
koşacak yer.
