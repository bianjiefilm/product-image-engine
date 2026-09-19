"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

interface Project {
  id: string;
  name: string;
  status: string;
  usage_kind: string;
  width_px: number;
  height_px: number;
  source_type: string;
  source_ref: string;
  updated_at: string;
}

// 项目列表:恢复/新建/打开工程。
export default function ProjectsPage() {
  const router = useRouter();
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [error, setError] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const [form, setForm] = useState({
    name: "",
    usage_kind: "电商主图",
    width_px: 800,
    height_px: 800,
    source_type: "standalone",
    source_ref: "",
  });
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    (async () => {
      const s = await fetch("/api/auth/session");
      if (s.status === 401) {
        router.replace("/login");
        return;
      }
      await reload();
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function reload() {
    const res = await fetch("/api/projects");
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setError(data?.error?.message ?? "加载失败");
      return;
    }
    setProjects((data?.projects ?? []) as Project[]);
  }

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError("");
    try {
      const res = await fetch("/api/projects", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setError(data?.error?.message ?? "创建失败");
        return;
      }
      setShowCreate(false);
      await reload();
      const id = data?.project?.id;
      if (id) router.push(`/projects/${id}`);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div>
      <div className="card">
        <div className="row" style={{ alignItems: "center" }}>
          <h2 style={{ flex: 1, margin: 0 }}>我的制作工程</h2>
          <Link className="link" href="/handoff">
            接受跨应用交接
          </Link>
          <button className="primary" onClick={() => setShowCreate((v) => !v)}>
            {showCreate ? "收起" : "新建工程"}
          </button>
          <button
            onClick={async () => {
              await fetch("/api/auth/logout", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: "{}",
              });
              router.replace("/login");
            }}
          >
            退出登录
          </button>
        </div>
        {showCreate ? (
          <form onSubmit={create} style={{ marginTop: 12 }}>
            <div className="row">
              <div>
                <label>工程名称</label>
                <input
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div>
                <label>用途</label>
                <input
                  value={form.usage_kind}
                  onChange={(e) =>
                    setForm({ ...form, usage_kind: e.target.value })
                  }
                />
              </div>
            </div>
            <div className="row">
              <div>
                <label>宽(px)</label>
                <input
                  type="number"
                  value={form.width_px}
                  onChange={(e) =>
                    setForm({ ...form, width_px: Number(e.target.value) })
                  }
                />
              </div>
              <div>
                <label>高(px)</label>
                <input
                  type="number"
                  value={form.height_px}
                  onChange={(e) =>
                    setForm({ ...form, height_px: Number(e.target.value) })
                  }
                />
              </div>
              <div>
                <label>来源类型</label>
                <select
                  value={form.source_type}
                  onChange={(e) =>
                    setForm({ ...form, source_type: e.target.value })
                  }
                >
                  <option value="standalone">独立制作(无订单)</option>
                  <option value="order">挂接订单</option>
                  <option value="campaign">挂接活动</option>
                </select>
              </div>
              {form.source_type !== "standalone" ? (
                <div>
                  <label>来源引用(source_ref)</label>
                  <input
                    value={form.source_ref}
                    onChange={(e) =>
                      setForm({ ...form, source_ref: e.target.value })
                    }
                    placeholder="来源私有引用,必填"
                  />
                </div>
              ) : null}
            </div>
            <div style={{ marginTop: 12 }}>
              <button className="primary" type="submit" disabled={creating}>
                {creating ? "创建中…" : "创建"}
              </button>
            </div>
          </form>
        ) : null}
        {error ? <div className="banner">{error}</div> : null}
      </div>

      <div className="card">
        {projects === null ? (
          <span className="muted">加载中…</span>
        ) : projects.length === 0 ? (
          <span className="muted">还没有工程,点右上角「新建工程」开始。</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>状态</th>
                <th>用途</th>
                <th>尺寸</th>
                <th>来源</th>
                <th>更新时间</th>
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => (
                <tr key={p.id}>
                  <td>
                    <Link className="link" href={`/projects/${p.id}`}>
                      {p.name}
                    </Link>
                  </td>
                  <td>{p.status}</td>
                  <td>{p.usage_kind || "—"}</td>
                  <td>
                    {p.width_px}×{p.height_px}
                  </td>
                  <td>
                    {p.source_type ? (
                      <span className="pill">
                        {p.source_type}
                        {p.source_ref ? ` · ${p.source_ref}` : ""}
                      </span>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td className="muted">{p.updated_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
