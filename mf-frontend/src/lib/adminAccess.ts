// Panele girişte verilen tek karar, tek yerde.
//
// Bu bir güvenlik sınırı DEĞİL. Sınır backend'de: /admin/* alt ağacı
// RequireAuth + RequireRole(admin) altında ve her istekte yeniden bakıyor.
// Buradaki karar yalnızca ekranın ne göstereceğini seçiyor — tarayıcının
// bildiği her şeyi tarayıcının kullanıcısı değiştirebilir.
//
// Dikkat edilen tek incelik `loading`: "kullanıcı yok" ile "kullanıcı henüz
// yüklenmedi" aynı şey değil. İkisi karışırsa sayfayı yenileyen yönetici bir
// kare boyunca giriş ekranını görür.
//
// Koşul kabulü kapısı burada YOK, ve bu bilerek: 4. aşamada hukuki metni
// panelden düzeltecek olan operatör, düzelteceği metnin eski hâlini kabul
// etmeden panele giremiyorsa metni hiç düzeltemez. Operatör hizmeti tüketen
// taraf değil, veri sorumlusunun kendisi.

export type PanelGateState = "booting" | "login" | "redirect" | "allow";

export function panelGate(input: {
  loading: boolean;
  user: { role: string } | null;
}): PanelGateState {
  if (input.loading) return "booting";
  if (!input.user) return "login";
  return input.user.role === "admin" ? "allow" : "redirect";
}
