# PEFT nedir? — soru soru fine-tuning turu

Bu doküman PEFT'i **kod yazmadan önce** anlamak için yazıldı. Kurulum yok,
komut listesi yok; burada **"bu teknik ne yapıyor, neden var, bizim kartta ne
sığar"** sorusunun cevabı var.

[nasil-calisiyor.md](./nasil-calisiyor.md) sistemin şu anki halini anlatıyor.
Bu doküman ise henüz yapmadığımız bir şeyi anlatıyor — okurken bunu aklında tut.

---

## 1. Fine-tuning nedir, neden düz yapamıyoruz?

Fine-tuning = hazır bir modelin ağırlıklarını kendi verinle biraz daha eğitmek.

Sorun bellek. `gemma-2-2b-it` 2.61 milyar parametre. Klasik (full) fine-tuning'de
GPU'da aynı anda şunları tutman gerekir:

| Ne | Boyut |
|---|---|
| Ağırlıklar (fp16) | 5.2 GB |
| Gradyanlar (fp16) | 5.2 GB |
| Adam optimizer durumu (fp32 master + `m` + `v`) | ~31 GB |
| Aktivasyonlar | + birkaç GB |
| **Toplam** | **~40 GB+** |

Bizim kart 6 GB (GTX 1660 Ti) ve Windows zaten masaüstünü onunla çiziyor.
Yani full fine-tuning **iki büyüklük mertebesi** uzakta. Bu bir optimizasyon
problemi değil, duvar.

**PEFT** (Parameter-Efficient Fine-Tuning) bu duvarın adı verilen ailenin adı:
"modelin tamamını değil, çok küçük bir kısmını eğit". LoRA bu ailenin en
yaygın üyesi.

---

## 2. LoRA tam olarak ne yapıyor?

Bir linear katman şunu hesaplar:

```
h = W·x
```

`W` donmuş (frozen), hiç dokunmuyoruz. Yanına iki küçük matris koyuyoruz:

```
h = W·x  +  (α/r)·B·A·x
     ↑            ↑
   donmuş      eğitilen
```

- `W` boyutu `d × k` — büyük.
- `A` boyutu `r × k`, `B` boyutu `d × r` — burada `r` çok küçük (8, 16, 32).
- `B·A` çarpımı da `d × k`, yani `W` ile aynı şekilde. Ama `A` ve `B`'nin
  toplam parametresi `r·(d+k)`, `W`'nin `d·k`'sının yanında hiçbir şey.

**Kilit fikir:** bir modeli yeni bir göreve uyarlarken ağırlıklarda oluşan
değişim `ΔW` "düşük ranklı" — yani `d·k` tane bağımsız sayıya ihtiyaç duymuyor,
çok daha az sayıyla ifade edilebiliyor. LoRA `ΔW`'yi doğrudan öğrenmek yerine
`B·A` olarak çarpanlarına ayırıp öğreniyor.

**Başlangıç hilesi:** `A` rastgele, `B` **sıfır** ile başlatılır. Yani `B·A = 0`
ve eğitimin ilk adımında model temel modelle **birebir aynı** çıktıyı verir.
Fine-tuning modeli bozarak değil, sıfırdan ekleyerek başlar.

### Bizim modelde sayılar

`gemma-2-2b` 26 katman, hidden 2304. `r=16` ile `q/k/v/o` projeksiyonlarını
hedeflersek:

```
katman başına ≈ 246.000 parametre × 26 katman ≈ 6.4 milyon
```

2.61 milyarın **%0.24'ü**. Diskte fp16 olarak ~13 MB'lık bir dosya. Bir LoRA
adapter'ı işte bu: 13 MB'lık bir `.safetensors`.

---

## 3. `r`, `alpha`, `target_modules` ne demek?

Ayarlayacağın üç şey bunlar:

| Parametre | Ne yapar | Pratik değer |
|---|---|---|
| `r` (rank) | Kapasite. Büyükse daha çok şey öğrenir, daha çok bellek ve daha çok overfit riski. | 8–32. Format/üslup işi için 8–16 yeter. |
| `lora_alpha` | Ölçek. Efektif öğrenme gücü `α/r` ile orantılı. | Genelde `2r` (r=16 → α=32). |
| `target_modules` | Hangi katmanlara takılacak. | `q,k,v,o` klasik. `gate/up/down` (MLP) eklemek gücü artırır, maliyeti de. |
| `lora_dropout` | Regülarizasyon. | 0.05 |

**Not:** Gemma'nın embedding katmanı 256.000 × 2304 ≈ 590M parametre — modelin
beşte biri. LoRA'yı embedding'e takmak cazip gelebilir; gerek yoksa takma,
bütün bellek avantajını yer.

---

## 4. QLoRA nedir, LoRA'dan farkı ne?

LoRA optimizer belleğini çözdü ama **temel model hâlâ fp16 olarak GPU'da
duruyor** — bizim modelde 5.2 GB. 6 GB'lık kartta, üzerine aktivasyonlar
gelince, sığmaz.

**QLoRA** temel modeli 4-bit'e sıkıştırıp donduruyor, LoRA adapter'ını fp16
olarak onun üzerine takıyor. Üç tekniği var:

1. **NF4 (4-bit NormalFloat)** — sinir ağı ağırlıkları yaklaşık normal dağılımlı
   olduğu için, kuantizasyon seviyelerini normal dağılımın kuantillerine göre
   yerleştiren bir veri tipi. Düz int4'ten bilgi-teorik olarak daha iyi.
2. **Double quantization** — kuantizasyon sabitlerinin kendisini de kuantize
   eder. Parametre başına ~0.37 bit kazandırır.
3. **Paged optimizers** — bellek zirvelerinde NVIDIA unified memory'ye taşarak
   OOM'u önler.

### Bizim kartta bütçe

| Ne | Boyut |
|---|---|
| Temel model (NF4) | ~1.5 GB |
| LoRA parametreleri (fp16) | ~13 MB |
| Gradyanlar + Adam (sadece 6.4M param için) | ~90 MB |
| Aktivasyonlar (seq 512, batch 1, gradient checkpointing) | ~1–2 GB |
| **Toplam** | **~3–4 GB** |

6 GB'a sığar. Windows masaüstü ~1 GB yediği için rahat değil ama olur.
Ayar düğmeleri: `max_seq_length`, `per_device_train_batch_size=1`,
`gradient_accumulation_steps` ile telafi, `gradient_checkpointing=True`.

---

## 5. LoRA neyi iyi yapar, neyi yapamaz? (en önemli bölüm)

Bu ayrımı kaçırmak, projenin mimarisini yanlış kurmak demek.

**LoRA'nın iyi olduğu şey — davranış:**
- Çıktı **formatına** uyma (her zaman şu şemada JSON üret)
- Üslup, ton, dil
- Belirli bir görev kalıbı (sınıflandır, özetle, etiketle)
- Alan **jargonuna** alışma

**LoRA'nın yapamadığı şey — bilgi:**
- Modele **yeni gerçekler** öğretmek güvenilir çalışmaz. Eğitim verisindeki
  cümleleri ezberleyebilir ama sorulunca doğru hatırlaması garanti değil,
  ve hatırlamadığında **uydurur**.

> **Bunun projeye doğrudan sonucu:** Spec'teki "DeepKwiki bilgi bankası"
> fine-tuning işi **değil**. Bilgi bankası = **RAG**: dokümanları vektör
> veritabanında tut, sorguda ilgili parçaları getir, prompt'a koy. Fine-tuning
> ile bilgi bankası yapmaya çalışmak, hem pahalı hem yanlış hem de her doküman
> güncellemesinde yeniden eğitim demek.
>
> İkisi birlikte çalışır: **RAG bilgiyi getirir, LoRA formatı garantiler.**

---

## 6. Neden MLC'de "tek tıkla adapter yükle" yok?

Spec "adapter'ı hot-swap et, modeli yeniden başlatma" diyor. MLC bunu yapamaz —
ve sebebi mimari, eksiklik değil.

MLC modeli **TVM ile önceden derliyor**: hesap grafiği hedef GPU mimarisi için
(bizde `sm_75`) makine koduna çevriliyor, ağırlıklar `q4f16_1` olarak offline
paketleniyor. Çalışma zamanında "şu `W`'ye şu `B·A`'yı ekle" diyebileceğin bir
yol yok; o toplama derlenmiş grafikte mevcut değil.

Çalışma zamanı multi-LoRA'yı **vLLM** yapıyor (punica çekirdekleri ile, birden
çok adapter'ı aynı batch'te koşturabiliyor). Ama vLLM `sm_75` + 6 GB'da pratikte
ayağa kalkmaz — yani motoru değiştirmek de bir çıkış değil.

### MLC'de gerçek akış nasıl olur

```
1. QLoRA eğit            → 13 MB adapter (PEFT / HF transformers)
2. merge_and_unload()    → ΔW temel ağırlıklara gömülür, fp16 tam model
3. mlc_llm convert_weight → q4f16_1'e kuantize
4. mlc_llm gen_config     → chat şablonu, context penceresi
5. mlc_llm compile        → model kütüphanesi (ya da serve'de JIT)
6. Yeni model dizinini serve et, replikayı değiştir
```

Yani "adapter yüklemek" = **yeni bir model derlemek**. Dakikalar sürer,
tek tık değil. Dürüst tarif: *adapter kütüphanesi + build pipeline + aktif
sürüm seçimi*. Hot-swap değil.

### Dikkat: çift kuantizasyon kaybı

Adapter'ı NF4'e sıkıştırılmış temel model üzerinde eğitiyoruz, sonra fp16'ya
açıp merge ediyoruz, sonra tekrar `q4f16_1`'e sıkıştırıyoruz. **İki kayıplı
adım.** Kalite kayması gerçek ve ölçülmeli — "çalışıyor gibi" yeterli değil,
merge öncesi/sonrası aynı prompt setiyle karşılaştırma gerekir.

---

## 7. Bu projede neyi fine-tune ederdik?

Yukarıdaki "format iyi, bilgi kötü" ayrımına uyan, spec'in içinde zaten duran
bir hedef var: **Rich Result**.

Spec, LLM'in ham metnini frontend'in tablo/grafik/markdown içeren zengin bir
UI'a çevirmesini istiyor. Bunun çalışması için modelin **her seferinde**
ayrıştırılabilir bir şema üretmesi lazım. 2B'lik bir model prompt'la bunu
yapmaya çalışır ama sık sık şemadan kaçar — ve bu tam olarak LoRA'nın en iyi
olduğu problem.

Yan faydası: bu projenin zaten bir **skorlama** motoru var
([scoring.go](../mf-backend/internal/llm/scoring.go)). Yani fine-tuning'in işe
yarayıp yaramadığını ölçecek altyapı hazır — "şema uyum oranı önce %X, sonra
%Y" diyebilen bir capstone, "LoRA ekledim" diyen bir capstone'dan çok daha
değerli.

---

## 8. Gemma-2'ye özel tuzaklar

Genel PEFT anlatımlarında geçmeyen, bizi doğrudan vuracak olanlar:

- **Logit soft-capping.** Gemma-2 attention ve final logit'lerde softcap
  kullanıyor. Bu flash-attention-2 ile uyumsuz — eğitimde
  `attn_implementation="eager"` gerekiyor. Yoksa sessizce yanlış eğitirsin.
- **bf16 yok.** Turing (`sm_75`) bf16 desteklemiyor; Gemma-2 bf16 ile eğitilmiş.
  fp16'ya düşmek zorundayız, bu da taşma riski demek — loss scaling şart.
- **Değişken attention.** Katmanlar sırayla local (4096 sliding window) ve
  global attention kullanıyor. Kısa `max_seq_length` seçersen local katmanların
  pencere davranışını hiç eğitmemiş olursun.
- **Tied embeddings + 256k kelime dağarcığı.** Embedding modelin beşte biri;
  LoRA hedeflerine dahil etme.

---

## 9. Sırada ne var?

Bu doküman "ne olduğunu" anlatıyor. Yapılacaklar henüz yazılmadı. Kod aşamasına
geçmeden önce netleşmesi gerekenler:

1. **Eğitim verisi nereden gelecek?** Format öğretmek için ~500–2000 örnek
   yeter, ama örneklerin *doğru şemada* olması lazım. Elde var mı, üretecek
   miyiz?
2. **Eğitim nerede koşacak?** Aynı 6 GB kart hem serve hem train yapamaz —
   eğitim sırasında MLC container'ını durdurmak gerekir.
3. **Başarı nasıl ölçülecek?** Eğitim öncesi bir baseline ölçümü almadan
   eğitim yapmak, sonucu yorumlayamamak demek.
