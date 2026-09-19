"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

interface BatchCounts {
  total: number;
  pending: number;
  submitted: number;
  succeeded: number;
  failed: number;
  blocked: number;
  cancelled: number;
}

interface Batch {
  id: string;
  name: string;
  status: string;
  error_code: string;
  project_id: string;
  counts: BatchCounts;
  preset_summary: {
    scenes?: string[];
    presets?: string[];
    usage_note?: string;
    finalize_with_variants?: boolean;
  };
  updated_at: string;
}

interface BatchItem {
  id: string;
  input_name: string;
  input_size: number;
  status: string;
  error_code: string;
  task_ref: string;
  output_id: string;
  size_variant_ids: string[];
  variants_note: string;
  attempts: number;
}

interface Preset {
  name: string;
  width: number;
  height: number;
  mode: string;
}

const STATUS_TEXT: Record<string, string> = {
  pending: "待提交",
  submitting: "提交中",
  running: "进行中",
  completing: "收集中",
  completed: "已完成",
  completed_with_failures: "部分失败",
  failed: "失败",
  cancelled: "已取消",
};

const ITEM_STATUS_TEXT: Record<string, string> = {
  pending: "待提交",
  submitted: "已提交",
  succeeded: "成功",
  failed: "失败",
  blocked: "受阻",
  cancelled: "已取消",
};

const ACTIVE_STATUSES = new Set(["pending", "submitting", "running", "completing"]);
const MAX_FILE_BYTES = 8 * 1024 * 1024;

// 批量生成面板:多文件建批 → 提交 → 轮询收集 → 重试失败/取消。
// 全部动作都打本站 /api/batches*(BFF 纯透传),业务裁决在 Go server。
export default function BatchesPage() {
  const router = useRouter();
  const [batches, setBatches] = useState<Batch[] | null>(null);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");

  // 建批表单
  const [name, setName] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [scenes, setScenes] = useState("白底主图");
  const [presets, setPresets] = useState<string[]>([]);
  const [presetOptions, setPresetOptions] = useState<Preset[] | null>(null);
  const [usageNote, setUsageNote] = useState("");
  const [finalizeVariants, setFinalizeVariants] = useState(false);
  const [creating, setCreating] = useState(false);

  // 详情 + 轮询
  const [selected, setSelected] = useState<Batch | null>(null);
  const [items, setItems] = useState<BatchItem[]>([]);
  const [busy, setBusy] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    (async () => {
      const s = await fetch("/api/auth/session");
      if (s.status === 401) {
        router.replace("/login");
        return;
      }
      await reload();
      const pr = await fetch("/api/size-adapt/presets");
      if (pr.ok) {
        const pd = await pr.json().catch(() => null);
        setPresetOptions((pd?.presets ?? []) as Preset[]);
      } else {
        setPresetOptions([]); // 尺寸适配未开:不展示预设,变体诉求自然消失
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const loadDetail = useCallback(async (batchId: string) => {
    const res = await fetch(`/api/batches/${batchId}`);
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setError(data?.error?.message ?? "加载批次失败");
      return null;
    }
    setSelected((data?.batch ?? null) as Batch | null);
    setItems((data?.items ?? []) as BatchItem[]);
    return data?.batch as Batch | undefined;
  }, []);

  const reload = useCallback(async () => {
    const res = await fetch("/api/batches");
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setError(data?.error?.message ?? "加载失败");
      return;
    }
    setBatches((data?.batches ?? []) as Batch[]);
  }, []);

  // 在途批次轮询收集(5s);终态自动停。
  useEffect(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    if (!selected || !ACTIVE_STATUSES.has(selected.status)) return;
    const id = selected.id;
    pollRef.current = setInterval(async () => {
      await fetch(`/api/batches/${id}/collect`, { method: "POST" });
      const b = await loadDetail(id);
      if (b && !ACTIVE_STATUSES.has(b.status)) await reload();
    }, 5000);
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [selected, loadDetail, reload]);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setNote("");
    if (files.length === 0) {
      setError("请选择至少一个 PNG 文件");
      return;
    }
    const tooBig = files.filter((f) => f.size > MAX_FILE_BYTES);
    if (tooBig.length > 0) {
      setError(`以下文件超过 8MiB 上限:${tooBig.map((f) => f.name).join("、")}`);
      return;
    }
    setCreating(true);
    try {
      const items = await Promise.all(
        files.map(async (f) => ({
          file_name: f.name,
          content_type: "image/png",
          data_b64: await fileToB64(f),
        }))
      );
      const res = await fetch("/api/batches", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name,
          scenes: scenes
            .split(/[,,、\s]+/)
            .map((s) => s.trim())
            .filter(Boolean),
          presets,
          usage_note: usageNote,
          finalize_with_variants: finalizeVariants,
          items,
        }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setError(data?.error?.message ?? "建批失败");
        return;
      }
      if ((data?.duplicates ?? []).length > 0) {
        setNote(`以下文件内容重复,已去重:${(data.duplicates as string[]).join("、")}`);
      }
      setFiles([]);
      await reload();
      const batchId = (data?.batch as Batch | undefined)?.id;
      if (batchId) await loadDetail(batchId);
    } finally {
      setCreating(false);
    }
  }

  async function action(kind: "submit" | "collect" | "retry-failed" | "cancel") {
    if (!selected) return;
    setBusy(true);
    setError("");
    setNote("");
    try {
      const res = await fetch(`/api/batches/${selected.id}/${kind}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setError(data?.error?.message ?? "操作失败");
        return;
      }
      if (data?.note) setNote(String(data.note));
      await loadDetail(selected.id);
      await reload();
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className="card">
        <div className="row" style={{ alignItems: "center" }}>
          <h2 style={{ flex: 1, margin: 0 }}>批量生成</h2>
          <Link className="link" href="/projects">
            返回工程列表
          </Link>
        </div>
        <form onSubmit={create} style={{ marginTop: 12 }}>
          <div className="row">
            <div>
              <label>批次名称</label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="如:秋季上新批次"
                required
              />
            </div>
            <div>
              <label>产品图(PNG,≤8MiB,可多选)</label>
              <input
                type="file"
                accept="image/png"
                multiple
                onChange={(e) => setFiles(Array.from(e.target.files ?? []))}
              />
            </div>
          </div>
          <div className="row">
            <div>
              <label>场景(逗号分隔)</label>
              <input
                value={scenes}
                onChange={(e) => setScenes(e.target.value)}
                placeholder="白底主图,场景图"
              />
            </div>
            <div>
              <label>备注用途(可选)</label>
              <input
                value={usageNote}
                onChange={(e) => setUsageNote(e.target.value)}
                placeholder="如:秋季上新"
              />
            </div>
          </div>
          {presetOptions && presetOptions.length > 0 ? (
            <div className="row">
              <div>
                <label>尺寸变体预设(可选,多选)</label>
                <div className="row">
                  {presetOptions.map((p) => (
                    <label key={p.name} style={{ fontWeight: "normal" }}>
                      <input
                        type="checkbox"
                        checked={presets.includes(p.name)}
                        onChange={(e) =>
                          setPresets((cur) =>
                            e.target.checked
                              ? [...cur, p.name]
                              : cur.filter((n) => n !== p.name)
                          )
                        }
                      />{" "}
                      {p.name}({p.width}×{p.height})
                    </label>
                  ))}
                </div>
                <label style={{ fontWeight: "normal" }}>
                  <input
                    type="checkbox"
                    checked={finalizeVariants}
                    onChange={(e) => setFinalizeVariants(e.target.checked)}
                  />{" "}
                  成果登记后自动生成尺寸变体
                </label>
              </div>
            </div>
          ) : null}
          <div style={{ marginTop: 12 }}>
            <button className="primary" type="submit" disabled={creating}>
              {creating ? "建批中…" : `建批(${files.length} 个文件)`}
            </button>
          </div>
        </form>
        {error ? <div className="banner">{error}</div> : null}
        {note ? <div className="banner">{note}</div> : null}
      </div>

      <div className="card">
        {batches === null ? (
          <span className="muted">加载中…</span>
        ) : batches.length === 0 ? (
          <span className="muted">还没有批次,在上方建批后开始。</span>
        ) : (
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>状态</th>
                <th>进度(成功/总数)</th>
                <th>失败</th>
                <th>受阻</th>
                <th>更新时间</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {batches.map((b) => (
                <tr key={b.id}>
                  <td>{b.name}</td>
                  <td>
                    {STATUS_TEXT[b.status] ?? b.status}
                    {b.error_code ? ` (${b.error_code})` : ""}
                  </td>
                  <td>
                    {b.counts?.succeeded ?? 0}/{b.counts?.total ?? 0}
                  </td>
                  <td>{b.counts?.failed ?? 0}</td>
                  <td>{b.counts?.blocked ?? 0}</td>
                  <td className="muted">{b.updated_at}</td>
                  <td>
                    <button
                      onClick={async () => {
                        setNote("");
                        setError("");
                        await loadDetail(b.id);
                      }}
                    >
                      查看
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {selected ? (
        <div className="card">
          <div className="row" style={{ alignItems: "center" }}>
            <h3 style={{ flex: 1, margin: 0 }}>
              {selected.name}
              <span className="pill" style={{ marginLeft: 8 }}>
                {STATUS_TEXT[selected.status] ?? selected.status}
                {selected.error_code ? ` · ${selected.error_code}` : ""}
              </span>
            </h3>
            <button
              className="primary"
              disabled={busy}
              onClick={() => action("submit")}
            >
              提交批次
            </button>
            <button disabled={busy} onClick={() => action("collect")}>
              收集进度
            </button>
            <button disabled={busy} onClick={() => action("retry-failed")}>
              重试失败项
            </button>
            <button disabled={busy} onClick={() => action("cancel")}>
              取消批次
            </button>
          </div>
          {items.length === 0 ? (
            <span className="muted">加载中…</span>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>文件</th>
                  <th>状态</th>
                  <th>错误码</th>
                  <th>尝试</th>
                  <th>成果</th>
                  <th>变体</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.id}>
                    <td>{it.input_name}</td>
                    <td>{ITEM_STATUS_TEXT[it.status] ?? it.status}</td>
                    <td className="muted">{it.error_code || "—"}</td>
                    <td>{it.attempts}</td>
                    <td>{it.output_id ? "已登记" : "—"}</td>
                    <td className="muted">
                      {(it.size_variant_ids?.length ?? 0) > 0
                        ? `${it.size_variant_ids.length} 个`
                        : it.variants_note || "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <p className="muted" style={{ marginBottom: 0 }}>
            在途批次每 5 秒自动收集;失败项可重试(生成新任务);受阻项请先修复
            生成开关/上游服务配置再用「提交批次」重试。
          </p>
        </div>
      ) : null}
    </div>
  );
}

// fileToB64 浏览器端文件 → base64(去 data: 前缀)。
function fileToB64(f: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => {
      const s = String(r.result ?? "");
      const idx = s.indexOf(",");
      resolve(idx >= 0 ? s.slice(idx + 1) : s);
    };
    r.onerror = () => reject(r.error ?? new Error("读取文件失败"));
    r.readAsDataURL(f);
  });
}
