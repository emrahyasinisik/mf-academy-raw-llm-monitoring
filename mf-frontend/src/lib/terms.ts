/** Kabul kapısı gerekiyor mu — tek karar, tek yerde. */
export function needsTermsGate(
  user: { terms_accepted_at: string | null } | null,
): boolean {
  return user !== null && user.terms_accepted_at === null;
}
