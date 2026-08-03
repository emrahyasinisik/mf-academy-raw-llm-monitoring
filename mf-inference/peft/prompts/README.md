# Prompt varyantları — ölçmek için, göndermek için değil

Bu dizindeki dosyalar `persona_eval.py --system-prompt-file` ile ölçülüyor.
**Hiçbiri çıkarım yoluna bağlı değil.** Üretimin gönderdiği prompt
`mf-backend/internal/decision`'da duruyor ve `GET /decision/prompt` ile
okunuyor; buradaki bir dosya kazanırsa oraya taşınması ayrı bir iştir. Yoksa
hiçbir şeyin göndermediği bir metin üzerinde iyi sayılar toplanır — veri
üretecinin prompt'u kopyalamak yerine çekmesinin sebebi de aynı.

## Neden bir prompt denemesi

`persona-measure`, `Qwen/Qwen3-4B-Instruct-2507` tabanını 100 satırlık
validation setinde ölçtü:

| metrik | taban |
|---|---:|
| `citation_valid` | 1.00 |
| `asked_when_thin` | 19/28 |
| `grounded_format` | **0.64** |
| `decision_match` | 15/72 |

Tabanda kapatılacak tek açık `grounded_format` ve o bir **biçim uyumu**
sorunu — modelin bilmediği bir şey değil, uymadığı bir kural. Fine-tune ile
çözmeye çalışmak bu hattın ilk koşusunda `asked_when_thin`'i 19/28'den 0/28'e
düşürdü. Prompt'un bedeli sıfır, riski sıfır ve geri alınması bir commit.

## `persona_v2.txt` — hipotez ve üç değişiklik

Mevcut prompt (`v1`, backend'in gönderdiği) doğru şeyleri söylüyor ama üç
yapısal zaafı var. Üçü de düzeltilebilir ve üçü de ayrı ayrı gerekçeli:

1. **Biçim şartı son söz değil.** `v1` formatı verdikten sonra bir paragraf daha
   ekliyor ("Türkçe, net ve dürüst ol..."). Modelin okuduğu son talimat üsluba
   dair oluyor, çıktı sözleşmesine değil. `v2` üslup paragrafını yukarı aldı;
   son satır artık "Bu üç satır cevabının son satırları olmalı."

2. **İki mod birbirinden ayrılmamış.** `v1`'de "soru sor" ve "karar ver" aynı
   `DAVRANIŞ` listesinin iki maddesi. `v2` bunları **BİÇİM 1 / BİÇİM 2** olarak
   isimlendirip aralarına "üçüncü bir biçim yok, ikisini karıştırma" koyuyor.
   Bu, yalnız `grounded_format` için değil `asked_when_thin` için de önemli:
   adapter'ın kaybettiği şey tam olarak bu ayrımdı, ve prompt'ta hiç açıkça
   yazmıyordu.

3. **Soru modunda ne YAZILMAYACAĞI söylenmemiş.** `v1` "tek soru sor ve dur"
   diyor ama KARAR satırını yazmamayı söylemiyor. `asked_when_thin` tam olarak
   bunu ölçüyor — cevapta `KARAR:` geçiyorsa satır sayılmıyor. `v2` açıkça
   yasaklıyor.

Beklenen: `grounded_format` yükselir, `asked_when_thin` **en azından korunur**.
İkincisi düşerse `v2` reddedilir — `v1`'in 0.64'ü, soru sormayı kaybetmiş bir
0.95'ten iyidir.

## Koşma

```bash
# Kaggle: kaggle/persona-prompt/persona-prompt.ipynb
# Yerel GPU'da ya da tunel uzerinden:
python3 persona_eval.py --local --base-only \
    --local-base-model Qwen/Qwen3-4B-Instruct-2507 \
    --system-prompt-file prompts/persona_v2.txt \
    --limit 100 --out out/prompt_v2.json
```

`--system-prompt-file` yalnız system turn'ünü değiştiriyor: kanıt, sıralama ve
yer gerçeği sabit kalıyor, yani karşılaştırma tek değişkenli. Satır başına tam
bir system turn bulamazsa çıkıyor, çünkü o hâlde karşılaştırma tek değişkenli
olmaktan çıkar.
