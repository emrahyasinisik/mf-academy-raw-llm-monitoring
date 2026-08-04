/** Kabul kapısı — versiyon karşılaştırması. */
export function needsTermsGate(
  user: {
    terms_accepted_at: string | null;
    terms_version?: string;
  } | null,
  requiredVersion: string | null | undefined,
): boolean {
  if (user === null) return false;
  if (user.terms_accepted_at === null) return true;
  // Yayın yoksa kapıyı açık bırakmak yerine kapatmak yanlış olurdu: seed'siz
  // bir deploy'da herkes kilitlenirdi. Yayın yoksa yalnızca "hiç kabul etmemiş"
  // kapısı çalışır (yukarıdaki satır).
  if (!requiredVersion) return false;
  return (user.terms_version ?? "") !== requiredVersion;
}
