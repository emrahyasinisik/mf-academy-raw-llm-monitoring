/** True when every seat is taken — the add button must not offer what the API will 409. */
export function seatFull(memberCount: number, seatLimit: number): boolean {
  return memberCount >= seatLimit;
}
