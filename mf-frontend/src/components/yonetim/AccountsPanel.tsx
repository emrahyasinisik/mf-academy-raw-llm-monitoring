"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import { api, ApiError } from "@/lib/api";
import type {
  AccountDetail,
  AccountListResult,
  AccountStatus,
  AccountSummary,
  AccountType,
  CreateAccountRequest,
  CreateAccountResponse,
} from "@/lib/types";

const PAGE_SIZE = 10;

const typeLabels: Record<AccountType, string> = {
  individual: "Bireysel",
  company: "Şirket",
};

const statusLabels: Record<AccountStatus, string> = {
  active: "Aktif",
  suspended: "Askıda",
};

const orgRoleLabels: Record<string, string> = {
  owner: "Sahip",
};

const emptyResult: AccountListResult = {
  accounts: [],
  total: 0,
  page: 1,
  limit: PAGE_SIZE,
};

export function AccountsPanel() {
  const [filters, setFilters] = useState({
    q: "",
    type: "" as AccountType | "",
    status: "" as AccountStatus | "",
  });
  const [query, setQuery] = useState(filters);
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<AccountListResult>(emptyResult);
  const [selectedId, setSelectedId] = useState("");
  const [detail, setDetail] = useState<AccountDetail | null>(null);
  const [created, setCreated] = useState<CreateAccountResponse | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [loadingList, setLoadingList] = useState(true);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({
    type: "individual" as AccountType,
    name: "",
    email: "",
    tax_id: "",
    seat_limit: 5,
  });

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(result.total / result.limit)),
    [result.limit, result.total],
  );

  const loadList = useCallback(() => {
    return api.admin.accounts
      .list({
        q: query.q.trim() || undefined,
        type: query.type || undefined,
        status: query.status || undefined,
        page,
        limit: PAGE_SIZE,
      })
      .then((res) => {
        setResult(res);
        if (res.accounts.length === 0) {
          setSelectedId("");
          setDetail(null);
        }
      })
      .catch((e: ApiError) => setError(e.message))
      .finally(() => setLoadingList(false));
  }, [page, query.q, query.status, query.type]);

  const loadDetail = useCallback((id: string) => {
    if (!id) return Promise.resolve();
    setLoadingDetail(true);
    setError("");
    return api.admin.accounts
      .get(id)
      .then((res) => {
        setDetail(res);
        setSelectedId(res.id);
      })
      .catch((e: ApiError) => setError(e.message))
      .finally(() => setLoadingDetail(false));
  }, []);

  useEffect(() => {
    loadList();
  }, [loadList]);

  function applyFilters(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setCreated(null);
    setError("");
    setLoadingList(true);
    setQuery(filters);
    setPage(1);
  }

  async function createAccount(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const payload: CreateAccountRequest = {
        type: form.type,
        name: form.name,
        email: form.email,
        ...(form.type === "company"
          ? { tax_id: form.tax_id, seat_limit: form.seat_limit }
          : {}),
      };
      const res = await api.admin.accounts.create(payload);
      setCreated(res);
      setSelectedId(res.account.id);
      setFilters({ q: "", type: "", status: "" });
      setQuery({ q: "", type: "", status: "" });
      setPage(1);
      setForm({
        type: "individual",
        name: "",
        email: "",
        tax_id: "",
        seat_limit: 5,
      });
      const firstPage = await api.admin.accounts.list({ page: 1, limit: PAGE_SIZE });
      setResult(firstPage);
      await loadDetail(res.account.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Hesap oluşturulamadı.");
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

  async function setSuspension(account: AccountDetail, suspend: boolean) {
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await (suspend
        ? api.admin.accounts.suspend(account.id)
        : api.admin.accounts.unsuspend(account.id));
      setNotice(suspend ? "Hesap askıya alındı." : "Hesabın askısı kaldırıldı.");
      await loadList();
      await loadDetail(account.id);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Durum güncellenemedi.");
    } finally {
      setSaving(false);
    }
  }

  async function deleteAccount(account: AccountDetail) {
    const ok = window.confirm(
      `"${account.name}" hesabını ve üyelerinin verisini kalıcı olarak silmek istiyor musunuz? Bu geri alınamaz.`,
    );
    if (!ok) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await api.admin.accounts.delete(account.id);
      setNotice("Hesap silindi.");
      setSelectedId("");
      setDetail(null);
      await loadList();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Hesap silinemedi.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4">
      <section className="card p-4 space-y-3">
        <div className="flex items-start justify-between gap-3 flex-wrap">
          <div>
            <h3 className="font-display font-semibold">Hesaplar</h3>
            <p className="text-xs mt-1" style={{ color: "var(--text-faint)" }}>
              Firma ve kullanıcı erişimi burada yönetilir; içerik metinleri bu
              panele gelmez.
            </p>
          </div>
          <span className="pill pill-brand">{result.total} kayıt</span>
        </div>

        <form className="grid gap-2 md:grid-cols-[1fr_150px_150px_auto]" onSubmit={applyFilters}>
          <label>
            <span className="label">Ara</span>
            <input
              className="input"
              value={filters.q}
              placeholder="ad, e-posta veya vergi no"
              onChange={(e) => setFilters({ ...filters, q: e.target.value })}
            />
          </label>
          <label>
            <span className="label">Tür</span>
            <select
              className="input"
              value={filters.type}
              onChange={(e) =>
                setFilters({ ...filters, type: e.target.value as AccountType | "" })
              }
            >
              <option value="">Tümü</option>
              <option value="individual">Bireysel</option>
              <option value="company">Şirket</option>
            </select>
          </label>
          <label>
            <span className="label">Durum</span>
            <select
              className="input"
              value={filters.status}
              onChange={(e) =>
                setFilters({ ...filters, status: e.target.value as AccountStatus | "" })
              }
            >
              <option value="">Tümü</option>
              <option value="active">Aktif</option>
              <option value="suspended">Askıda</option>
            </select>
          </label>
          <button className="btn btn-primary self-end" type="submit">
            Filtrele
          </button>
        </form>

        {error && <div className="notice notice-bad">{error}</div>}
        {notice && <div className="notice notice-ok">{notice}</div>}

        <AccountTable
          accounts={result.accounts}
          loading={loadingList}
          selectedId={selectedId}
          onSelect={loadDetail}
        />

        <div className="flex items-center justify-between gap-3 flex-wrap">
          <p className="text-xs mono" style={{ color: "var(--text-faint)" }}>
            Sayfa {result.page} / {totalPages}
          </p>
          <div className="flex gap-2">
            <button
              className="btn btn-ghost btn-sm"
              disabled={page <= 1 || loadingList}
              onClick={() => {
                setError("");
                setLoadingList(true);
                setPage((p) => Math.max(1, p - 1));
              }}
            >
              Önceki
            </button>
            <button
              className="btn btn-ghost btn-sm"
              disabled={page >= totalPages || loadingList}
              onClick={() => {
                setError("");
                setLoadingList(true);
                setPage((p) => p + 1);
              }}
            >
              Sonraki
            </button>
          </div>
        </div>
      </section>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,420px)_1fr]">
        <CreateAccountCard
          form={form}
          saving={saving}
          created={created}
          onDismissPassword={() => setCreated(null)}
          onCopyPassword={copyPassword}
          onSubmit={createAccount}
          onChange={setForm}
        />
        <DetailCard
          detail={detail}
          loading={loadingDetail}
          saving={saving}
          onSuspend={(account) => setSuspension(account, true)}
          onUnsuspend={(account) => setSuspension(account, false)}
          onDelete={(account) => void deleteAccount(account)}
        />
      </div>
    </div>
  );
}

function AccountTable({
  accounts,
  loading,
  selectedId,
  onSelect,
}: {
  accounts: AccountSummary[];
  loading: boolean;
  selectedId: string;
  onSelect: (id: string) => void;
}) {
  if (loading) {
    return (
      <div className="space-y-2">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="skeleton h-9 w-full" />
        ))}
      </div>
    );
  }

  if (accounts.length === 0) {
    return (
      <p className="text-xs py-4" style={{ color: "var(--text-faint)" }}>
        Bu filtrelerle hesap bulunamadı.
      </p>
    );
  }

  return (
    <div className="overflow-x-auto scrollbar-thin">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ background: "var(--panel-2)" }}>
            {["hesap adı", "tür", "üye sayısı", "analiz sayısı", "son etkinlik", "durum", ""].map(
              (h) => (
                <th
                  key={h}
                  scope="col"
                  className="text-left px-4 py-2.5 eyebrow font-medium"
                  style={{ borderBottom: "1px solid var(--line)" }}
                >
                  {h}
                </th>
              ),
            )}
          </tr>
        </thead>
        <tbody>
          {accounts.map((account) => (
            <tr
              key={account.id}
              style={{
                borderTop: "1px solid var(--line)",
                background: account.id === selectedId ? "var(--panel-2)" : undefined,
              }}
              className="hover:bg-[var(--panel-2)] transition-colors"
            >
              <td className="px-4 py-2.5 min-w-[180px]">
                <span className="block truncate">{account.name}</span>
                <span className="mono" style={{ color: "var(--text-faint)" }}>
                  {account.tax_id || account.id.slice(0, 8)}
                </span>
              </td>
              <td className="px-4 py-2.5">
                <span className="pill">{typeLabels[account.type]}</span>
              </td>
              <td className="px-4 py-2.5 mono num">{account.member_count}</td>
              <td className="px-4 py-2.5 mono num">{account.assessment_count}</td>
              <td className="px-4 py-2.5 mono" style={{ color: "var(--text-faint)" }}>
                {formatDateTime(account.last_activity_at)}
              </td>
              <td className="px-4 py-2.5">
                <StatusPill status={account.status} />
              </td>
              <td className="px-4 py-2.5 text-right">
                <button className="btn btn-ghost btn-sm" onClick={() => onSelect(account.id)}>
                  aç
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CreateAccountCard({
  form,
  saving,
  created,
  onSubmit,
  onChange,
  onCopyPassword,
  onDismissPassword,
}: {
  form: {
    type: AccountType;
    name: string;
    email: string;
    tax_id: string;
    seat_limit: number;
  };
  saving: boolean;
  created: CreateAccountResponse | null;
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  onChange: (form: {
    type: AccountType;
    name: string;
    email: string;
    tax_id: string;
    seat_limit: number;
  }) => void;
  onCopyPassword: () => void;
  onDismissPassword: () => void;
}) {
  const canSubmit =
    form.name.trim() !== "" &&
    form.email.trim() !== "" &&
    (form.type === "individual" || form.seat_limit > 0);

  return (
    <section className="card p-4 space-y-3">
      <h3 className="font-display font-semibold">Yeni hesap</h3>
      <form className="space-y-3" onSubmit={onSubmit}>
        <label>
          <span className="label">Hesap türü</span>
          <select
            className="input"
            value={form.type}
            onChange={(e) => onChange({ ...form, type: e.target.value as AccountType })}
          >
            <option value="individual">Bireysel</option>
            <option value="company">Şirket</option>
          </select>
        </label>
        <label>
          <span className="label">
            {form.type === "company" ? "Şirket adı" : "Ad soyad"}
          </span>
          <input
            className="input"
            value={form.name}
            onChange={(e) => onChange({ ...form, name: e.target.value })}
          />
        </label>
        <label>
          <span className="label">Sahip e-postası</span>
          <input
            className="input"
            type="email"
            value={form.email}
            onChange={(e) => onChange({ ...form, email: e.target.value })}
          />
        </label>

        {form.type === "company" && (
          <div className="grid gap-2 sm:grid-cols-2">
            <label>
              <span className="label">Vergi no</span>
              <input
                className="input mono"
                value={form.tax_id}
                onChange={(e) => onChange({ ...form, tax_id: e.target.value })}
              />
            </label>
            <label>
              <span className="label">Koltuk limiti</span>
              <input
                className="input mono num"
                type="number"
                min={1}
                value={form.seat_limit}
                onChange={(e) =>
                  onChange({ ...form, seat_limit: Number(e.target.value) })
                }
              />
            </label>
          </div>
        )}

        <button className="btn btn-primary w-full" disabled={!canSubmit || saving}>
          Hesap oluştur
        </button>
      </form>

      {created && (
        <div className="notice notice-warn view-in space-y-3">
          <p>
            <strong>Bu parola bir daha gösterilmeyecek.</strong> Kullanıcı ilk
            girişte kalıcı parola seçmek zorunda kalır.
          </p>
          <div className="well p-3 flex items-center gap-2">
            <code className="mono text-sm flex-1 break-all" style={{ color: "var(--text)" }}>
              {created.temporary_password}
            </code>
            <button className="btn btn-ghost btn-sm" onClick={onCopyPassword}>
              kopyala
            </button>
          </div>
          <div className="flex items-center justify-between gap-3">
            <span className="text-xs" style={{ color: "var(--text-dim)" }}>
              Sahip: {created.owner.email}
            </span>
            <button className="btn btn-quiet btn-sm" onClick={onDismissPassword}>
              kapat
            </button>
          </div>
        </div>
      )}
    </section>
  );
}

function DetailCard({
  detail,
  loading,
  saving,
  onSuspend,
  onUnsuspend,
  onDelete,
}: {
  detail: AccountDetail | null;
  loading: boolean;
  saving: boolean;
  onSuspend: (account: AccountDetail) => void;
  onUnsuspend: (account: AccountDetail) => void;
  onDelete: (account: AccountDetail) => void;
}) {
  if (loading) {
    return (
      <section className="card p-4 space-y-3">
        <div className="skeleton h-5 w-40" />
        <div className="skeleton h-20 w-full" />
        <div className="skeleton h-28 w-full" />
      </section>
    );
  }

  if (!detail) {
    return (
      <section className="card p-4">
        <h3 className="font-display font-semibold">Hesap detayı</h3>
        <p className="text-xs mt-2" style={{ color: "var(--text-faint)" }}>
          Detayı görmek için listeden bir hesap aç.
        </p>
      </section>
    );
  }

  return (
    <section className="card p-4 space-y-4">
      <div className="flex items-start justify-between gap-3 flex-wrap">
        <div>
          <h3 className="font-display font-semibold">{detail.name}</h3>
          <p className="text-xs mt-1 mono" style={{ color: "var(--text-faint)" }}>
            {detail.id}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <StatusPill status={detail.status} />
          {detail.status === "active" ? (
            <button
              className="btn btn-danger btn-sm"
              disabled={saving}
              onClick={() => onSuspend(detail)}
            >
              Askıya al
            </button>
          ) : (
            <button
              className="btn btn-ghost btn-sm"
              disabled={saving}
              onClick={() => onUnsuspend(detail)}
            >
              Askıyı kaldır
            </button>
          )}
          <button
            className="btn btn-danger btn-sm"
            disabled={saving}
            onClick={() => onDelete(detail)}
          >
            Hesabı sil
          </button>
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-4">
        <MiniStat label="Tür" value={typeLabels[detail.type]} />
        <MiniStat label="Üye" value={String(detail.member_count)} />
        <MiniStat label="Analiz" value={String(detail.assessment_count)} />
        <MiniStat label="Oturum" value={String(detail.sessions.length)} />
      </div>

      <div>
        <h4 className="eyebrow mb-2">Üyeler</h4>
        {detail.members.length === 0 ? (
          <p className="text-xs" style={{ color: "var(--text-faint)" }}>
            Bu hesapta üye yok.
          </p>
        ) : (
          <ul className="space-y-1.5">
            {detail.members.map((member, i) => (
              <li
                key={member.id}
                className="item-in flex items-center gap-3 text-xs rounded-[var(--r-sm)] px-2.5 py-2"
                style={{
                  background: "var(--panel-2)",
                  border: "1px solid var(--line)",
                  ["--i" as string]: i,
                }}
              >
                <span className="flex-1 min-w-0">
                  <span className="block truncate" style={{ color: "var(--text)" }}>
                    {member.name || member.email}
                  </span>
                  <span className="mono" style={{ color: "var(--text-faint)" }}>
                    {member.email}
                  </span>
                </span>
                <span className="pill">
                  {orgRoleLabels[member.org_role] ?? member.org_role}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div>
        <h4 className="eyebrow mb-2">Aktif oturumlar</h4>
        {detail.sessions.length === 0 ? (
          <p className="text-xs" style={{ color: "var(--text-faint)" }}>
            Aktif oturum yok.
          </p>
        ) : (
          <ul className="space-y-1.5">
            {detail.sessions.map((session, i) => (
              <li
                key={session.id}
                className="item-in text-xs rounded-[var(--r-sm)] px-2.5 py-2"
                style={{
                  background: "var(--panel-2)",
                  border: "1px solid var(--line)",
                  ["--i" as string]: i,
                }}
              >
                <div className="flex items-center justify-between gap-3">
                  <span className="mono truncate" style={{ color: "var(--text)" }}>
                    {session.ip || "IP yok"}
                  </span>
                  <span className="mono" style={{ color: "var(--text-faint)" }}>
                    {formatDateTime(session.expires_at)}
                  </span>
                </div>
                <p className="truncate mt-1" style={{ color: "var(--text-faint)" }}>
                  {session.user_agent || "tarayıcı bilgisi yok"}
                </p>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="well p-3">
      <div className="eyebrow">{label}</div>
      <div className="font-display text-xl mt-1">{value}</div>
    </div>
  );
}

function StatusPill({ status }: { status: AccountStatus }) {
  return (
    <span className={`pill ${status === "active" ? "pill-ok" : "pill-warn"}`}>
      {statusLabels[status]}
    </span>
  );
}

function formatDateTime(value: string | null) {
  if (!value) return "—";
  return new Date(value).toLocaleString("tr-TR", {
    day: "2-digit",
    month: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
