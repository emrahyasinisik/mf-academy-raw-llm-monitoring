import { afterEach, test } from "node:test";
import assert from "node:assert/strict";
import { api, clearTokens, setTokens } from "./api.ts";

class MemoryStorage {
  private items = new Map<string, string>();

  getItem(key: string): string | null {
    return this.items.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.items.set(key, value);
  }

  removeItem(key: string): void {
    this.items.delete(key);
  }
}

const originalFetch = globalThis.fetch;
const originalLocalStorage = Object.getOwnPropertyDescriptor(
  globalThis,
  "localStorage",
);

afterEach(() => {
  globalThis.fetch = originalFetch;
  if (originalLocalStorage) {
    Object.defineProperty(globalThis, "localStorage", originalLocalStorage);
  }
});

test("parola değişimi yeni token çiftini saklar", async () => {
  const storage = new MemoryStorage();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });
  setTokens("old-access", "old-refresh");

  globalThis.fetch = async (input, init) => {
    assert.equal(String(input), "http://localhost:8080/auth/change-password");
    assert.equal(init?.method, "POST");
    assert.equal(init?.body, JSON.stringify({
      current_password: "gecici-parola",
      new_password: "kalici-parola",
    }));

    return new Response(JSON.stringify({
      access_token: "new-access",
      refresh_token: "new-refresh",
      token_type: "Bearer",
      expires_in: 3600,
      user: {
        id: "u1",
        email: "owner@example.com",
        name: "Owner",
        role: "user",
        must_change_password: false,
        created_at: "2026-08-04T00:00:00Z",
        updated_at: "2026-08-04T00:00:00Z",
        terms_accepted_at: "2026-08-04T00:00:00Z",
      },
    }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const data = await api.changePassword("gecici-parola", "kalici-parola");

  assert.equal(data.user.must_change_password, false);
  assert.equal(storage.getItem("mf_access"), "new-access");
  assert.equal(storage.getItem("mf_refresh"), "new-refresh");
  clearTokens();
});
