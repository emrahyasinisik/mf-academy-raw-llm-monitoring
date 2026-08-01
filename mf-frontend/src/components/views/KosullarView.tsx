"use client";

// Kullanım koşulları. Gizlilik metni ayrı bir sayfa ve orada kalıyor: bu belge
// hizmetin hangi şartlarla verildiğini, o belge veriyle ne yapıldığını
// anlatıyor. Saklama süresi gibi sayılar yalnızca birinde yazılı olmalı, yoksa
// ikisi ilk düzenlemede birbirinden ayrılır.
export function KosullarView() {
  return (
    <div className="mx-auto max-w-2xl px-4 sm:px-5 py-6">
      <h1 className="text-lg">Kullanım koşulları</h1>

      <h2 className="eyebrow mt-6">Bu hizmet nedir</h2>
      <p className="mt-2 text-sm">
        Bir vaka metni giriyorsunuz, önceden tanımlı bir rubriğe göre
        puanlanmış bir rapor alıyorsunuz. Puanı model vermiyor: model rubriği
        dolduruyor, ağırlıklı toplam bizim tarafımızda hesaplanıyor, ve her
        kriterin dayandığı alıntılar raporda gösteriliyor.
      </p>

      <h2 className="eyebrow mt-6">Rapor ne değildir</h2>
      <p className="mt-2 text-sm">
        Rapor bir yatırım tavsiyesi değildir ve sizin yerinize karar vermez.
        Bir ön eleme aracıdır: aynı ölçütü her vakaya aynı şekilde uygular ve
        gerekçesini gösterir. Kararı veren ve sonucundan sorumlu olan sizsiniz.
      </p>

      <h2 className="eyebrow mt-6">Garanti verilmiyor</h2>
      <p className="mt-2 text-sm">
        Bu bir demo. Doğruluk, kesintisizlik veya erişilebilirlik taahhüdü
        yok; hizmet önceden haber verilmeden değişebilir ya da durabilir.
        Üretilen raporların doğru olduğunu garanti etmiyoruz — ürünün amacı
        zaten değerlendirmeyi denetlenebilir kılmak, denetimi ortadan
        kaldırmak değil.
      </p>

      <h2 className="eyebrow mt-6">Girdiğiniz içerikten siz sorumlusunuz</h2>
      <p className="mt-2 text-sm">
        Yapıştırdığınız metni buraya girmeye yetkili olduğunuzu beyan etmiş
        olursunuz. Bu, üçüncü kişilere ait belgeler için de geçerlidir: bir
        başkasının şirketine ait bir dokümanı yüklüyorsanız, onu paylaşma
        hakkına sahip olduğunuzu varsayıyoruz.
      </p>

      <h2 className="eyebrow mt-6">Verileriniz</h2>
      <p className="mt-2 text-sm">
        Ne sakladığımız, ne kadar süreyle sakladığımız ve nasıl
        sildirebileceğiniz ayrı bir sayfada:{" "}
        <a href="#gizlilik">Verileriniz ve gizlilik</a>.
      </p>

      {/* Okuyanın görmediği sınır, sınır değildir. Bu cümle spec'te de var ama
          asıl yeri burası. */}
      <h2 className="eyebrow mt-6">Bu metnin sınırı</h2>
      <p className="mt-2 text-sm">
        Bu koşullar bir hukukçu tarafından hazırlanmadı. Demo için, ürünün
        gerçekte ne yaptığından türetilerek yazıldı. Gerçek müşteri verisiyle
        kullanılmadan önce gözden geçirilmesi gerekiyor.
      </p>
    </div>
  );
}
