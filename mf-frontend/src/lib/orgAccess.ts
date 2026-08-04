export type OrgGateState = "booting" | "login" | "redirect" | "allow";

export type OrgGateUser = {
  org_id?: string | null;
  org_role?: string | null;
  org_type?: string | null;
  role?: string;
};

export function canAccessOrgPanel(user: OrgGateUser | null): boolean {
  if (!user?.org_id) return false;
  if (user.org_type !== "company") return false;
  return user.org_role === "owner" || user.org_role === "admin";
}

export function orgGate(input: {
  loading: boolean;
  user: OrgGateUser | null;
}): OrgGateState {
  if (input.loading) return "booting";
  if (!input.user) return "login";
  return canAccessOrgPanel(input.user) ? "allow" : "redirect";
}
