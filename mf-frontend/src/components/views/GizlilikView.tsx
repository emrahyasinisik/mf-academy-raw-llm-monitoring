"use client";

// Bu sayfa barındırdığımız demo'yu anlatır. Operatör kendi donanımına kurduğunda
// veri sorumlusu o olur ve bu metin onun için geçerli değildir.
export function GizlilikView() {
  return (
    // Kendi kabını taşıyor. Bu görünüm iki yerden monte ediliyor — AppShell'in
    // içinden ve giriş ekranından — ve AppShell hiçbir görünüme kenar boşluğu
    // vermiyor; her ekran kendi kabını kuruyor. Bu da öyle, yoksa oturum
    // açıkken metin pencerenin kenarına yapışık çıkıyordu.
    <div className="mx-auto max-w-2xl px-4 sm:px-5 py-6">
      <h1 className="text-lg">Verileriniz ve gizlilik</h1>

      <h2 className="eyebrow mt-6">Kim işliyor</h2>
      <p className="mt-2 text-sm">
        Bu demo MasterFabric tarafından işletiliyor. Girdiğiniz veriler bizim
        sunucumuzda saklanıyor. Raporlar kendi sunucularımızda ve kendi çıkarım
        makinemizde üretiliyor; analiz için vaka metnini başka bir şirketin
        modeline göndermiyoruz.
      </p>

      <h2 className="eyebrow mt-6">Ne saklanıyor</h2>
      <ul className="mt-2 text-sm list-disc pl-5 space-y-1">
        <li>Analiz için yapıştırdığınız vaka metninin tamamı.</li>
        <li>Üretilen raporun bulguları ve kanıt alıntıları.</li>
        <li>Üreteç ekranında gönderdiğiniz istemler ve alınan yanıtlar.</li>
        <li>
          Persona ekranındaki konuşmalarınız: yazdığınız mesajlar, aldığınız
          yanıtlar, ve o yanıtı üretirken toplanan araştırma sonuçları.
        </li>
        <li>Adınız, e-posta adresiniz ve oturum kayıtlarınız.</li>
      </ul>

      <h2 className="eyebrow mt-6">Ne için</h2>
      <p className="mt-2 text-sm">
        Raporu üretmek ve ürünün kendisini ölçmek için. Raporlarınızı ve
        istemlerinizi satmıyoruz, paylaşmıyoruz, reklam için kullanmıyoruz.
      </p>
      {/* Bu paragrafın önceki hali "üçüncü taraflara aktarılmıyor" diyordu ve
          doğru değildi: Persona ekranı canlı araştırma yapıyor, sorguyu da
          sizin yazdığınız metinden kuruyor. Bir gizlilik metninin yanlış
          olabileceği en kötü yer, kullanıcının en çok güvendiği cümledir. */}
      <p className="mt-2 text-sm">
        Bir istisna var ve saklamıyoruz: Persona ekranı canlı web araştırması
        yapıyor. Orada yazdığınız metnin bir bölümü, arama sorgusu olarak bir
        arama motoruna gidiyor — yani o metin bizde kalmıyor. Araştırma
        istemiyorsanız Persona ekranını kullanmayın; Analiz ve Üreteç
        ekranlarında dışarıya böyle bir çağrı yok.
      </p>

      <h2 className="eyebrow mt-6">Ne kadar süreyle</h2>
      <p className="mt-2 text-sm">
        30 gün. Sonrasında vaka metni, kanıt alıntıları ve istemler
        kendiliğinden siliniyor; geriye puan, kapsam ve tarih gibi içeriği
        olmayan ölçüm kayıtları kalıyor.
      </p>
      <p className="mt-2 text-sm">
        Bunun bir sonucu var ve saklamıyoruz: 30 günden eski bir raporun puanını
        görebilirsiniz ama o puanın neye dayandığını artık gösteremiyoruz.
      </p>
      <p className="mt-2 text-sm">
        Persona konuşmaları için silme daha basit: otuz gün dokunulmayan bir
        konuşma tamamen siliniyor, mesajlarıyla birlikte, geriye kayıt
        kalmıyor. Raporlarda içeriksiz bir ölçüm satırı kalmasının sebebi o
        satırın ürünün kendi ölçümlerini beslemesi; bir konuşma hiçbir şey
        beslemiyor, o yüzden saklanacak bir şeyi de yok.
      </p>
      <p className="mt-2 text-sm">
        Süre, konuşmanın açıldığı tarihten değil <strong>son
        mesajdan</strong> sayılıyor — sürdürdüğünüz bir konuşma otuzuncu
        gününde ortasından silinmiyor.
      </p>
      {/* Yukarıdaki liste beş şey sayıyor, 30 gün dördünü siliyor. Beşincisini
          burada söylemek zorundayız: aksi halde metin, hiç değinmediği bir şeyi
          sildiğini ima ediyor. */}
      <p className="mt-2 text-sm">
        Bu süre hesap bilgileriniz için geçerli değil: e-posta adresiniz ve
        adınız, hesap durdukça duruyor. Hesabı silme akışı bilerek yok — bu bir
        demo ve öyle bir söz vermiyoruz.
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
    </div>
  );
}
