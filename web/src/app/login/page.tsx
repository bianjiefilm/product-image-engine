"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

// 登录页:邮箱+口令(platform-identity 派生身份,本应用不自管账号)。
export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => null);
        setError(data?.error?.message ?? `登录失败(HTTP ${res.status})`);
        return;
      }
      router.replace("/projects");
    } catch {
      setError("网络异常,请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card" style={{ maxWidth: 420, margin: "48px auto" }}>
      <h2>登录</h2>
      <form onSubmit={submit}>
        <label>邮箱</label>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="you@example.com"
          required
        />
        <label>口令</label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        {error ? <div className="banner">{error}</div> : null}
        <div style={{ marginTop: 16 }}>
          <button className="primary" type="submit" disabled={busy}>
            {busy ? "登录中…" : "登录"}
          </button>
        </div>
      </form>
      <p className="muted" style={{ marginBottom: 0 }}>
        身份由平台统一派生(usr_/acct_),本应用不保存口令。
      </p>
    </div>
  );
}
