"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { loggedInHome } from "@/lib/first-image";

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
      router.replace(loggedInHome());
    } catch {
      setError("网络异常,请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="media-bench" data-layout="entry">
      <section className="media-stage" aria-label="产品图">
        <div className="stage-empty">
          <p className="stage-kicker">产品图</p>
          <h2>先看商品，再决定改哪里</h2>
          <p>登录之后从一张商品照片开始。商品本身保持不动。</p>
          <p className="muted">这里还没有成片。登录不会生成图片。</p>
        </div>
      </section>
      <aside className="media-rail">
        <div className="card">
          <h2>登录</h2>
          <form onSubmit={submit}>
            <div className="media-actions">
              <button className="primary" type="submit" disabled={busy}>
                {busy ? "登录中…" : "登录后开始"}
              </button>
            </div>
            <label htmlFor="login-email">邮箱</label>
            <input
              id="login-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              required
            />
            <label htmlFor="login-password">口令</label>
            <input
              id="login-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            {error ? <div className="banner" role="alert">{error}</div> : null}
          </form>
          <p className="muted" style={{ marginBottom: 0 }}>
            身份由平台统一派生，本应用不保存口令。
          </p>
        </div>
      </aside>
    </div>
  );
}
