/** Zorunlu parola değiştirme kapısı gerekiyor mu — tek karar, tek yerde. */
export function needsPasswordGate(
  user: { must_change_password: boolean } | null,
): boolean {
  return user !== null && user.must_change_password;
}

/** API 403 yanıtı parola kapısını mı işaret ediyor. */
export function isPasswordChangeRequired(err: {
  status: number;
  code: string;
}): boolean {
  return err.status === 403 && err.code === "password_change_required";
}
