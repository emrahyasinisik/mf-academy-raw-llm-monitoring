"use client";

// Bu sayfa barındırdığımız demo'yu anlatır. Operatör kendi donanımına kurduğunda
// veri sorumlusu o olur ve bu metin onun için geçerli değildir.
export function GizlilikView() {
  return (
    <section className="max-w-2xl">
      <h1 className="text-lg">Verileriniz ve gizlilik</h1>

      <h2 className="eyebrow mt-6">Kim işliyor</h2>
      <p className="mt-2 text-sm">
        Bu demo MasterFabric tarafından işletiliyor. Girdiğiniz veriler bizim
        sunucumuzda saklanıyor.
      </p>

      <h2 className="eyebrow mt-6">Ne saklanıyor</h2>
      <ul className="mt-2 text-sm list-disc pl-5 space-y-1">
        <li>Analiz için yapıştırdığınız vaka metninin tamamı.</li>
        <li>Üretilen raporun bulguları ve kanıt alıntıları.</li>
        <li>Üreteç ekranında gönderdiğiniz istemler ve alınan yanıtlar.</li>
        <li>E-posta adresiniz ve oturum kayıtlarınız.</li>
      </ul>

      <h2 className="eyebrow mt-6">Ne için</h2>
      <p className="mt-2 text-sm">
        Raporu üretmek ve ürünün kendisini ölçmek için. Üçüncü taraflara
        aktarılmıyor, reklam için kullanılmıyor.
      </p>

      <h2 className="eyebrow mt-6">Ne kadar süreyle</h2>
      <p className="mt-2 text-sm">
        30 gün. Sonrasında vaka metni, kanıt alıntıları ve istemler
        kendiliğinden siliniyor; geriye puan, kapsam ve tarih gibi içeriği
        olmayan ölçüm kayıtları kalıyor. Bunun bir sonucu var ve saklamıyoruz:
        30 günden eski bir raporun puanını görebilirsiniz ama o puanın neye
        dayandığını artık gösteremiyoruz.
      </p>

      <h2 className="eyebrow mt-6">Daha erken silmek</h2>
      <p className="mt-2 text-sm">
        Analiz ekranındaki rapor listesinde her raporun yanında bir silme
        eylemi var. Bastığınızda 30. günde olacak şeyin aynısı hemen oluyor:
        içerik gidiyor, ölçüm kaydı kalıyor. Geri alınamıyor.
      </p>
      <p className="mt-2 text-sm">
        Bunun dışındaki talepler için henüz bir başvuru kanalımız yok. Demo
        dışında, gerçek verilerle kullanım için hazır olduğumuzu söylemiyoruz.
      </p>
    </section>
  );
}
