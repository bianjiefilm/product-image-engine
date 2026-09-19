"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

interface Project {
  id: string;
  name: string;
  status: string;
  usage_kind: string;
  width_px: number;
  height_px: number;
  source_type: string;
  source_ref: string;
  created_by: string;
  updated_at: string;
}
interface InputItem {
  id: string;
  platform_asset_id: string;
  snapshot_name: string;
  snapshot_size: number;
  snapshot_content_type: string;
}
interface VersionItem {
  id: string;
  version_no: number;
  platform_task_id: string;
  platform_asset_id: string;
  status: string;
  created_at: string;
}

// 工程详情:用途/尺寸/已选输入/任务与费用事实;返回来源入口(无来源也可完整用)。
export default function ProjectDetailPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const id = params?.id;

  const [project, setProject] = useState<Project | null>(null);
  const [inputs, setInputs] = useState<InputItem[]>([]);
  const [versions, setVersions] = useState<VersionItem[]>([]);
  const [loadError, setLoadError] = useState("");
  const [notice, setNotice] = useState("");
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState({
    name: "",
    status: "draft",
    usage_kind: "",
    width_px: 0,
    height_px: 0,
  });
  const [newInput, setNewInput] = useState({
    platform_asset_id: "",
    snapshot_name: "",
    snapshot_size: 0,
    snapshot_content_type: "image/png",
  });
  const [balance, setBalance] = useState<string>("");

  const load = useCallback(async () => {
    if (!id) return;
    const res = await fetch(`/api/projects/${id}`);
    const data = await res.json().catch(() => null);
    if (res.status === 401) {
      router.replace("/login");
      return;
    }
    if (!res.ok) {
      setLoadError(data?.error?.message ?? "加载失败");
      return;
    }
    const p = data.project as Project;
    setProject(p);
    setForm({
      name: p.name,
      status: p.status,
      usage_kind: p.usage_kind,
      width_px: p.width_px,
      height_px: p.height_px,
    });
    setInputs((data.inputs ?? []) as InputItem[]);
    setVersions((data.versions ?? []) as VersionItem[]);
  }, [id, router]);

  useEffect(() => {
    (async () => {
      const s = await fetch("/api/auth/session");
      if (s.status === 401) {
        router.replace("/login");
        return;
      }
      await load();
    })();
  }, [load, router]);

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setNotice(data?.error?.message ?? "保存失败");
        return;
      }
      setNotice("已保存");
      await load();
    } finally {
      setSaving(false);
    }
  }

  async function addInput(e: React.FormEvent) {
    e.preventDefault();
    setNotice("");
    const res = await fetch(`/api/projects/${id}/inputs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(newInput),
    });
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setNotice(data?.error?.message ?? "挂素材失败");
      return;
    }
    setNewInput({
      platform_asset_id: "",
      snapshot_name: "",
      snapshot_size: 0,
      snapshot_content_type: "image/png",
    });
    await load();
  }

  async function removeInput(inputId: string) {
    await fetch(`/api/projects/${id}/inputs/${inputId}`, { method: "DELETE" });
    await load();
  }

  // 提交生成(占位链路):开关 off / 服务不可达时展示明确的失败事实。
  async function submitGeneration() {
    setNotice("");
    const res = await fetch(`/api/projects/${id}/versions`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setNotice(data?.error?.message ?? `提交失败(HTTP ${res.status})`);
      return;
    }
    await load();
  }

  async function loadBalance() {
    setBalance("读取中…");
    const res = await fetch("/api/billing/balance");
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setBalance(`不可用:${data?.error?.message ?? `HTTP ${res.status}`}`);
      return;
    }
    setBalance(`余额 ¥${data?.balance_cny}(account ${data?.account_id})`);
  }

  if (loadError) {
    return (
      <div className="card">
        <div className="banner">{loadError}</div>
        <Link className="link" href="/projects">
          ← 返回工程列表
        </Link>
      </div>
    );
  }
  if (!project) return <div className="card muted">加载中…</div>;

  return (
    <div>
      <div className="card">
        <div className="row" style={{ alignItems: "center" }}>
          <h2 style={{ flex: 1, margin: 0 }}>{project.name}</h2>
          <Link className="link" href="/projects">
            ← 返回工程列表
          </Link>
        </div>
        <p className="muted" style={{ marginTop: 4 }}>
          来源:
          {project.source_type ? (
            <>
              <span className="pill">
                {project.source_type} · {project.source_ref}
              </span>{" "}
              <button
                onClick={() =>
                  setNotice(
                    "返回来源入口:跨应用跳转由后续接入票实现(来源引用已保存)。"
                  )
                }
              >
                返回来源
              </button>
            </>
          ) : (
            "独立制作(无来源,不影响任何功能)"
          )}
        </p>
      </div>

      <div className="card">
        <h2>基本信息(用途/尺寸)</h2>
        <form onSubmit={save}>
          <div className="row">
            <div>
              <label>名称</label>
              <input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                required
              />
            </div>
            <div>
              <label>状态</label>
              <select
                value={form.status}
                onChange={(e) => setForm({ ...form, status: e.target.value })}
              >
                <option value="draft">draft</option>
                <option value="active">active</option>
                <option value="archived">archived</option>
              </select>
            </div>
          </div>
          <div className="row">
            <div>
              <label>用途</label>
              <input
                value={form.usage_kind}
                onChange={(e) =>
                  setForm({ ...form, usage_kind: e.target.value })
                }
              />
            </div>
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
          </div>
          <div style={{ marginTop: 12 }}>
            <button className="primary" type="submit" disabled={saving}>
              {saving ? "保存中…" : "保存"}
            </button>
          </div>
        </form>
        {notice ? <div className="banner warn">{notice}</div> : null}
      </div>

      <div className="card">
        <h2>已选输入(平台素材引用)</h2>
        {inputs.length === 0 ? (
          <p className="muted">尚未挂接素材。</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>asset_id</th>
                <th>快照名</th>
                <th>大小</th>
                <th>类型</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {inputs.map((i) => (
                <tr key={i.id}>
                  <td>{i.platform_asset_id}</td>
                  <td>{i.snapshot_name || "—"}</td>
                  <td>{i.snapshot_size}</td>
                  <td>{i.snapshot_content_type || "—"}</td>
                  <td>
                    <button onClick={() => removeInput(i.id)}>移除</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <form onSubmit={addInput} style={{ marginTop: 12 }}>
          <div className="row">
            <div>
              <label>平台 asset_id</label>
              <input
                value={newInput.platform_asset_id}
                onChange={(e) =>
                  setNewInput({ ...newInput, platform_asset_id: e.target.value })
                }
                required
              />
            </div>
            <div>
              <label>快照文件名</label>
              <input
                value={newInput.snapshot_name}
                onChange={(e) =>
                  setNewInput({ ...newInput, snapshot_name: e.target.value })
                }
              />
            </div>
            <div>
              <label>大小(字节)</label>
              <input
                type="number"
                value={newInput.snapshot_size}
                onChange={(e) =>
                  setNewInput({ ...newInput, snapshot_size: Number(e.target.value) })
                }
              />
            </div>
            <div style={{ flex: 0 }}>
              <button type="submit">挂接素材</button>
            </div>
          </div>
        </form>
        <p className="muted" style={{ marginBottom: 0 }}>
          真实上传(直传 OSS → complete)由后续 FEAT 票接入;当前保存的是平台素材引用与快照元数据。
        </p>
      </div>

      <div className="card">
        <h2>任务与费用事实</h2>
        {versions.length === 0 ? (
          <p className="muted">暂无输出版本。</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>版本</th>
                <th>状态</th>
                <th>平台任务</th>
                <th>资产</th>
                <th>创建时间</th>
              </tr>
            </thead>
            <tbody>
              {versions.map((v) => (
                <tr key={v.id}>
                  <td>v{v.version_no}</td>
                  <td>{v.status}</td>
                  <td>{v.platform_task_id || "—"}</td>
                  <td>{v.platform_asset_id || "—"}</td>
                  <td className="muted">{v.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div className="row" style={{ marginTop: 12 }}>
          <div style={{ flex: 0 }}>
            <button onClick={submitGeneration}>提交生成(占位)</button>
          </div>
          <div style={{ flex: 0 }}>
            <button onClick={loadBalance}>读取费用事实</button>
          </div>
          <div style={{ flex: 2 }}>
            {balance ? <span className="muted">{balance}</span> : null}
          </div>
        </div>
        <p className="muted" style={{ marginBottom: 0 }}>
          生成能力与计费开关默认关闭;失败时这里只会展示真实原因,不会伪造成功。
        </p>
      </div>
    </div>
  );
}
