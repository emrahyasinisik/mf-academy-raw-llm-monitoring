/** Silinmiş bir raporun, boş gelmiş bir rapordan ayrılması. */

export function isRedacted(x: { redacted_at: string | null }): boolean {
  return x.redacted_at !== null;
}

// Üç durum var ve ikisi aynı boş dizeyle geliyor: içeriği silinmiş rapor,
// hiç başlık üretilememiş rapor. Ayıran tek şey redacted_at, ve ikisini
// birleştirmek kullanıcıya "verin kayıp" ile "verinizi sildik" arasındaki
// farkı kaybettirir.
export function reportTitle(
  x: { redacted_at: string | null; subject_title: string },
): string {
  if (isRedacted(x)) return "İçerik silindi";
  return x.subject_title || "Başlıksız";
}

// Koşumlar için aynı üç durum, farklı alan adıyla. reportTitle ile
// birleştirilmedi: ikisi farklı API tiplerinden besleniyor ve tek bir jenerik
// imza, çağıran tarafta hangi alanın okunduğunu görünmez yapardı.
export function runTitle(
  x: { redacted_at: string | null; prompt_preview: string },
): string {
  if (isRedacted(x)) return "İçerik silindi";
  return x.prompt_preview || "Başlıksız";
}
