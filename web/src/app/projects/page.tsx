"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ErrorBanner } from "@/components/ui/error-banner";
import { jsonBody, jsonHeaders } from "@/lib/api-client";
import { projectSourceLabel, projectStatusLabel } from "@/lib/project-labels";

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
      let session: Response;
      try {
        session = await fetch("/api/auth/session");
      } catch {
        // 网络不可达不等于未登录:给诚实错误态和恢复动作,不偷偷跳登录。
        setError("网络不可用,暂时无法确认登录状态,请稍后重试。");
        return;
      }
      if (session.status === 401) {
        router.replace("/login");
        return;
      }
      await reload();
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function reload() {
    let res: Response;
    try {
      res = await fetch("/api/projects");
    } catch {
      setError("网络不可用,暂时拿不到工程列表,请稍后重试。");
      return;
    }
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setError(data?.error?.message ?? "加载失败");
      return;
    }
    setError("");
    setProjects((data?.projects ?? []) as Project[]);
  }

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    setError("");
    try {
      const res = await fetch("/api/projects", {
        method: "POST",
        headers: jsonHeaders,
        body: jsonBody(form),
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
    <div className="list-workspace" data-layout="list">
      <div className="card">
        <div className="row row-center">
          <h2 className="row-title">我的制作工程</h2>
          <Link className="link" href="/start">开始做产品图</Link>
          <Link className="link" href="/batches">批量</Link>
          <Link className="link" href="/handoff">
            接受跨应用交接
          </Link>
          <button className="primary" onClick={() => setShowCreate((v) => !v)}>
            {showCreate ? "收起" : "新建工程"}
          </button>
        </div>
        {showCreate ? (
          <form onSubmit={create} className="mt-12">
            <div className="media-actions">
              <button className="primary" type="submit" disabled={creating}>
                {creating ? "创建中…" : "创建"}
              </button>
            </div>
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
          </form>
        ) : null}
        {error ? (
          <div data-state="error">
            <ErrorBanner message={error} onRetry={() => void reload()} />
          </div>
        ) : null}
      </div>

      <div className="card">
        {projects === null ? (
          <span className="muted" data-state="loading">加载中…</span>
        ) : projects.length === 0 ? (
          <span className="muted" data-state="empty">还没有工程。<Link className="link" href="/start">去开始做产品图</Link></span>
        ) : (
          <>
            <ul className="asset-strip" aria-label="工程">
              {projects.map((p) => (
                <li key={p.id}>
                  <Link className="link" href={`/projects/${p.id}`}>
                    {p.name}
                  </Link>
                  <div><span className="pill">{projectStatusLabel(p.status)}</span></div>
                  <div>{p.usage_kind || "—"}</div>
                  <div>{p.width_px}×{p.height_px}</div>
                  <div>
                    {p.source_type ? (
                      <span className="pill">
                        {projectSourceLabel(p.source_type)}
                        {p.source_ref ? ` · ${p.source_ref}` : ""}
                      </span>
                    ) : (
                      "—"
                    )}
                  </div>
                  <div className="muted">{p.updated_at || "时间未知"}</div>
                </li>
              ))}
            </ul>
            <details data-technical="true">
              <summary>工程技术详情</summary>
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
                      <td>{p.name}</td>
                      <td>{p.status || "状态未知"}</td>
                      <td>{p.usage_kind || "—"}</td>
                      <td>{p.width_px}×{p.height_px}</td>
                      <td>{p.source_type || "—"}{p.source_ref ? ` · ${p.source_ref}` : ""}</td>
                      <td className="muted">{p.updated_at}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </details>
          </>
        )}
      </div>
    </div>
  );
}
