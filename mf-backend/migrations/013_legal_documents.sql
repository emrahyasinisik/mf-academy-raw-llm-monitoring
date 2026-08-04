-- 013_legal_documents.sql
-- Hukuki metinler append-only: her yayın yeni satır. Üzerine yazmak, altı ay
-- sonra "kullanıcı neyi kabul etti" sorusunun cevabını yok eder.
-- Taslak aynı tabloda (is_draft): iki tablo iki şema bakımı demek.

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

-- Seed: bugünkü GizlilikView / KosullarView metinleri. Seed olmadan deploy
-- gizlilik sayfasını boşaltır. Versiyon auth.TermsVersion ile aynı kalır ki
-- mevcut kabul kayıtları kapıyı yanlışlıkla yeniden açmasın.
INSERT INTO legal_documents (slug, title, version, body, requires_reconsent, is_draft, published_at)
SELECT * FROM (VALUES
(
  'gizlilik',
  'Verileriniz ve gizlilik',
  '2026-08-01',
  $giz$
# Verileriniz ve gizlilik

## Kim işliyor

Bu demo MasterFabric tarafından işletiliyor. Girdiğiniz veriler bizim sunucumuzda saklanıyor. Raporlar kendi sunucularımızda ve kendi çıkarım makinemizde üretiliyor; analiz için vaka metnini başka bir şirketin modeline göndermiyoruz.

## Ne saklanıyor

- Analiz için yapıştırdığınız vaka metninin tamamı.
- Üretilen raporun bulguları ve kanıt alıntıları.
- Üreteç ekranında gönderdiğiniz istemler ve alınan yanıtlar.
- Persona ekranındaki konuşmalarınız: yazdığınız mesajlar, aldığınız yanıtlar, ve o yanıtı üretirken toplanan araştırma sonuçları.
- Adınız, e-posta adresiniz ve oturum kayıtlarınız.

## Ne için

Raporu üretmek ve ürünün kendisini ölçmek için. Raporlarınızı ve istemlerinizi satmıyoruz, paylaşmıyoruz, reklam için kullanmıyoruz.

Bir istisna var ve saklamıyoruz: Persona ekranı canlı web araştırması yapıyor. Orada yazdığınız metnin bir bölümü, arama sorgusu olarak bir arama motoruna gidiyor — yani o metin bizde kalmıyor. Araştırma istemiyorsanız Persona ekranını kullanmayın; Analiz ve Üreteç ekranlarında dışarıya böyle bir çağrı yok.

## Ne kadar süreyle

30 gün. Sonrasında vaka metni, kanıt alıntıları ve istemler kendiliğinden siliniyor; geriye puan, kapsam ve tarih gibi içeriği olmayan ölçüm kayıtları kalıyor.

Bunun bir sonucu var ve saklamıyoruz: 30 günden eski bir raporun puanını görebilirsiniz ama o puanın neye dayandığını artık gösteremiyoruz.

Persona konuşmaları için silme daha basit: otuz gün dokunulmayan bir konuşma tamamen siliniyor, mesajlarıyla birlikte, geriye kayıt kalmıyor. Raporlarda içeriksiz bir ölçüm satırı kalmasının sebebi o satırın ürünün kendi ölçümlerini beslemesi; bir konuşma hiçbir şey beslemiyor, o yüzden saklanacak bir şeyi de yok.

Süre, konuşmanın açıldığı tarihten değil **son mesajdan** sayılıyor — sürdürdüğünüz bir konuşma otuzuncu gününde ortasından silinmiyor.

Bu süre hesap bilgileriniz için geçerli değil: e-posta adresiniz ve adınız, hesap durdukça duruyor. Hesabı silme akışı bilerek yok — bu bir demo ve öyle bir söz vermiyoruz.

## Daha erken silmek

Analiz ekranındaki rapor listesinde her raporun yanında bir silme eylemi var. Bastığınızda 30. günde olacak şeyin aynısı hemen oluyor: içerik gidiyor, ölçüm kaydı kalıyor. Geri alınamıyor.

Bunun dışındaki talepler için henüz bir başvuru kanalımız yok. Demo dışında, gerçek verilerle kullanım için hazır olduğumuzu söylemiyoruz.
$giz$::text,
  false,
  false,
  now()
),
(
  'kosullar',
  'Kullanım koşulları',
  '2026-08-01',
  $kos$
# Kullanım koşulları

## Bu hizmet nedir

Bir vaka metni giriyorsunuz, önceden tanımlı bir rubriğe göre puanlanmış bir rapor alıyorsunuz. Puanı model vermiyor: model rubriği dolduruyor, ağırlıklı toplam bizim tarafımızda hesaplanıyor, ve her kriterin dayandığı alıntılar raporda gösteriliyor.

## Rapor ne değildir

Rapor bir yatırım tavsiyesi değildir ve sizin yerinize karar vermez. Bir ön eleme aracıdır: aynı ölçütü her vakaya aynı şekilde uygular ve gerekçesini gösterir. Kararı veren ve sonucundan sorumlu olan sizsiniz.

## Garanti verilmiyor

Bu bir demo. Doğruluk, kesintisizlik veya erişilebilirlik taahhüdü yok; hizmet önceden haber verilmeden değişebilir ya da durabilir. Üretilen raporların doğru olduğunu garanti etmiyoruz — ürünün amacı zaten değerlendirmeyi denetlenebilir kılmak, denetimi ortadan kaldırmak değil.

## Girdiğiniz içerikten siz sorumlusunuz

Yapıştırdığınız metni buraya girmeye yetkili olduğunuzu beyan etmiş olursunuz. Bu, üçüncü kişilere ait belgeler için de geçerlidir: bir başkasının şirketine ait bir dokümanı yüklüyorsanız, onu paylaşma hakkına sahip olduğunuzu varsayıyoruz.

## Verileriniz

Ne sakladığımız, ne kadar süreyle sakladığımız ve nasıl sildirebileceğiniz ayrı bir sayfada anlatılıyor: gizlilik metni.

## Bu metnin sınırı

Bu koşullar bir hukukçu tarafından hazırlanmadı. Demo için, ürünün gerçekte ne yaptığından türetilerek yazıldı. Gerçek müşteri verisiyle kullanılmadan önce gözden geçirilmesi gerekiyor.
$kos$::text,
  false,
  false,
  now()
)
) AS v(slug, title, version, body, requires_reconsent, is_draft, published_at)
WHERE NOT EXISTS (
  SELECT 1 FROM legal_documents d WHERE d.slug = v.slug AND d.is_draft = false
);
