"use client";

// Şirket ekip paneli — AccountsPanel desenini kopyalar, import etmez.
// Yönetim paneli yüzeyini müşteri rotasına sızdırmamak için ayrı tutuldu.

import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { api, ApiError } from "@/lib/api";
import { seatFull } from "@/lib/orgTeam";
import type {
  CreateOrgMemberResponse,
  OrgAssignableRole,
  OrgMember,
  OrgSummary,
} from "@/lib/types";

const roleLabels: Record<string, string> = {
  owner: "Sahip",
  admin: "Yönetici",
  member: "Üye",
};

export function TeamPanel() {
  const [org, setOrg] = useState<OrgSummary | null>(null);
  const [members, setMembers] = useState<OrgMember[]>([]);
  const [created, setCreated] = useState<CreateOrgMemberResponse | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({
    name: "",
    email: "",
    org_role: "member" as OrgAssignableRole,
  });

  const full = seatFull(org?.member_count ?? 0, org?.seat_limit ?? 0);

  const load = useCallback(() => {
    setLoading(true);
    setError("");
    return Promise.all([api.org.me(), api.org.members.list()])
      .then(([me, list]) => {
        setOrg(me.org);
        setMembers(list.members);
      })
      .catch((e: unknown) => {
        setError(
          e instanceof ApiError ? e.message : "Ekip yüklenemedi.",
        );
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function createMember(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (full) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const res = await api.org.members.create({
        name: form.name.trim(),
        email: form.email.trim(),
        org_role: form.org_role,
      });
      setCreated(res);
      setForm({ name: "", email: "", org_role: "member" });
      await load();
    } catch (err) {
      setError(createErrorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  async function copyPassword() {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.temporary_password);
      setNotice("Geçici parola panoya kopyalandı.");
    } catch {
      setNotice("Kopyalanamadı. Parolayı bu ekrandan manuel olarak alın.");
    }
  }

  async function changeRole(member: OrgMember, org_role: OrgAssignableRole) {
    if (member.org_role === "owner" || member.org_role === org_role) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await api.org.members.setRole(member.id, org_role);
      setNotice(`${member.name || member.email} rolü güncellendi.`);
      await load();
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Rol güncellenemedi.",
      );
    } finally {
      setSaving(false);
    }
  }

  async function removeMember(member: OrgMember) {
    if (member.org_role === "owner") return;
    const ok = window.confirm(
      `"${member.name || member.email}" ekip üyesini çıkarmak istiyor musunuz?`,
    );
    if (!ok) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await api.org.members.remove(member.id);
      setNotice("Üye çıkarıldı.");
      if (created?.member.id === member.id) setCreated(null);
      await load();
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Üye çıkarılamadı.",
      );
    } finally {
      setSaving(false);
    }
  }

  const canSubmit =
    !full &&
    form.name.trim() !== "" &&
    form.email.trim() !== "" &&
    !saving;

  return (
    <div className="space-y-4">
      <section className="card p-4 space-y-3">
        <div className="flex items-start justify-between gap-3 flex-wrap">
          <div>
            <h3 className="font-display font-semibold">Ekip</h3>
            <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
              Koltuklar şirketinizin limiti içinde yönetilir; e-posta daveti
              henüz yok — geçici parola bir kez gösterilir.
            </p>
          </div>
          {org && (
            <span className="pill pill-brand">
              {org.member_count} / {org.seat_limit} koltuk
            </span>
          )}
        </div>

        {error && <div className="notice notice-bad">{error}</div>}
        {notice && <div className="notice notice-ok">{notice}</div>}

        <MemberTable
          members={members}
          loading={loading}
          saving={saving}
          onRoleChange={changeRole}
          onRemove={(m) => void removeMember(m)}
        />
      </section>

      <section className="card p-4 space-y-3">
        <h3 className="font-display font-semibold">Üye ekle</h3>
        {full && (
          <div className="notice notice-warn">
            Koltuk limiti doldu. Yeni üye eklemek için önce bir koltuk
            boşaltın veya platform yöneticisinden limit artırımı isteyin.
          </div>
        )}
        <form className="space-y-3" onSubmit={createMember}>
          <div className="grid gap-2 sm:grid-cols-2">
            <label>
              <span className="label">Ad soyad</span>
              <input
                className="input"
                value={form.name}
                disabled={full || saving}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </label>
            <label>
              <span className="label">E-posta</span>
              <input
                className="input"
                type="email"
                value={form.email}
                disabled={full || saving}
                onChange={(e) => setForm({ ...form, email: e.target.value })}
              />
            </label>
          </div>
          <label>
            <span className="label">Rol</span>
            <select
              className="input"
              value={form.org_role}
              disabled={full || saving}
              onChange={(e) =>
                setForm({
                  ...form,
                  org_role: e.target.value as OrgAssignableRole,
                })
              }
            >
              <option value="member">Üye</option>
              <option value="admin">Yönetici</option>
            </select>
          </label>
          <button className="btn btn-primary w-full" disabled={!canSubmit}>
            Üye ekle
          </button>
        </form>

        {created && (
          <div className="notice notice-warn view-in space-y-3">
            <p>
              <strong>Bu parola bir daha gösterilmeyecek.</strong> Kullanıcı
              ilk girişte kalıcı parola seçmek zorunda kalır.
            </p>
            <div className="well p-3 flex items-center gap-2">
              <code
                className="mono text-sm flex-1 break-all"
                style={{ color: "var(--text)" }}
              >
                {created.temporary_password}
              </code>
              <button className="btn btn-ghost btn-sm" onClick={() => void copyPassword()}>
                kopyala
              </button>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs" style={{ color: "var(--text-dim)" }}>
                {created.member.email}
              </span>
              <button
                className="btn btn-quiet btn-sm"
                onClick={() => setCreated(null)}
              >
                kapat
              </button>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function MemberTable({
  members,
  loading,
  saving,
  onRoleChange,
  onRemove,
}: {
  members: OrgMember[];
  loading: boolean;
  saving: boolean;
  onRoleChange: (member: OrgMember, role: OrgAssignableRole) => void;
  onRemove: (member: OrgMember) => void;
}) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2].map((i) => (
          <div key={i} className="skeleton h-9 w-full" />
        ))}
      </div>
    );
  }

  if (members.length === 0) {
    return (
      <p className="text-xs py-4" style={{ color: "var(--text-faint)" }}>
        Henüz ekip üyesi yok.
      </p>
    );
  }

  return (
    <div className="overflow-x-auto scrollbar-thin">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ background: "var(--panel-2)" }}>
            {["ad", "e-posta", "rol", "kayıt", ""].map((h) => (
              <th
                key={h || "actions"}
                scope="col"
                className="text-left px-4 py-2.5 eyebrow font-medium"
                style={{ borderBottom: "1px solid var(--line)" }}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {members.map((member) => {
            const isOwner = member.org_role === "owner";
            return (
              <tr
                key={member.id}
                style={{ borderTop: "1px solid var(--line)" }}
                className="hover:bg-[var(--panel-2)] transition-colors"
              >
                <td className="px-4 py-2.5 min-w-[140px]">
                  <span className="block truncate">{member.name || "—"}</span>
                </td>
                <td
                  className="px-4 py-2.5 mono"
                  style={{ color: "var(--text-faint)" }}
                >
                  {member.email}
                </td>
                <td className="px-4 py-2.5">
                  {isOwner ? (
                    <span className="pill">{roleLabels.owner}</span>
                  ) : (
                    <select
                      className="input"
                      style={{ minWidth: "8rem", paddingBlock: "0.35rem" }}
                      value={
                        member.org_role === "admin" || member.org_role === "member"
                          ? member.org_role
                          : "member"
                      }
                      disabled={saving}
                      onChange={(e) =>
                        onRoleChange(
                          member,
                          e.target.value as OrgAssignableRole,
                        )
                      }
                    >
                      <option value="admin">Yönetici</option>
                      <option value="member">Üye</option>
                    </select>
                  )}
                </td>
                <td
                  className="px-4 py-2.5 mono"
                  style={{ color: "var(--text-faint)" }}
                >
                  {formatDate(member.created_at)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  {!isOwner && (
                    <button
                      className="btn btn-danger btn-sm"
                      disabled={saving}
                      onClick={() => onRemove(member)}
                    >
                      Çıkar
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function createErrorMessage(err: unknown): string {
  if (!(err instanceof ApiError)) return "Üye eklenemedi.";
  if (err.status === 409) {
    if (/seat/i.test(err.message)) {
      return "Koltuk limiti doldu (409). Yeni üye eklenemez.";
    }
    return "Bu e-posta zaten kayıtlı.";
  }
  return err.message || "Üye eklenemedi.";
}

function formatDate(value: string) {
  return new Date(value).toLocaleString("tr-TR", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
