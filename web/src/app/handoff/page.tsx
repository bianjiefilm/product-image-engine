"use client";

// 跨应用续接入口(HUI-1745 I1):粘贴来源应用下发的交接文档 → 接受。
// 只创建/恢复工程,绝不触发生成;新版需求返回待采用差异,由用户确认后采用。
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

interface SourceContext {
  source_kind?: string;
  source_app?: string;
  principal_id?: string;
  order_ref?: string;
  stage_ref?: string;
  campaign_ref?: string;
  brief_version?: string;
  scopes?: string[];
  assets?: { asset_ref?: string; media_type?: string }[];
  delivery_spec?: { description?: string; media_type?: string };
}
interface Binding {
  id: string;
  source_app: string;
  source_ref: string;
  purpose: string;
  current_snapshot_id: string;
}
interface Snapshot {
  id: string;
  handoff_id: string;
  brief_version: string;
  source_revision: string;
}
interface DiffSide {
  handoff_id: string;
  source_revision: string;
  brief_version: string;
}
interface Diff {
  old?: DiffSide;
  new?: DiffSide;
}

export default function HandoffPage() {
  const router = useRouter();
  const [docText, setDocText] = useState("");
  const [purpose, setPurpose] = useState("主图");
  const [accepting, setAccepting] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<{
    resolution?: string;
    snapshot_resolution?: string;
    project?: { id: string; name: string; usage_kind: string };
    binding?: Binding;
    snapshot?: Snapshot;
    source_context?: SourceContext;
    pending_snapshot?: Snapshot;
    diff?: Diff;
  } | null>(null);
  const [adopting, setAdopting] = useState(false);
  const [adoptMsg, setAdoptMsg] = useState("");

  useEffect(() => {
    (async () => {
      const s = await fetch("/api/auth/session");
      if (s.status === 401) router.replace("/login");
    })();
  }, [router]);

  async function accept(e: React.FormEvent) {
    e.preventDefault();
    setAccepting(true);
    setError("");
    setResult(null);
    setAdoptMsg("");
    try {
      let doc: unknown;
      try {
        doc = JSON.parse(docText);
      } catch {
        setError("交接文档不是合法 JSON");
        return;
      }
      const res = await fetch("/api/handoffs/accept", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ handoff: doc, purpose }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setError(data?.error?.message ?? `接受失败(HTTP ${res.status})`);
        return;
      }
      setResult(data);
    } finally {
      setAccepting(false);
    }
  }

  async function adopt() {
    if (!result?.binding?.id || !result.pending_snapshot?.id) return;
    setAdopting(true);
    setAdoptMsg("");
    try {
      const res = await fetch(`/api/bindings/${result.binding.id}/adopt`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ snapshot_id: result.pending_snapshot.id }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setAdoptMsg(data?.error?.message ?? `采用失败(HTTP ${res.status})`);
        return;
      }
      setAdoptMsg("已采用新版需求(人工编辑与已选输出保留)");
      setResult({ ...result, pending_snapshot: undefined, diff: undefined });
    } finally {
      setAdopting(false);
    }
  }

  const ctx = result?.source_context;

  return (
    <div>
      <div className="card">
        <div className="row" style={{ alignItems: "center" }}>
          <h2 style={{ flex: 1, margin: 0 }}>接受跨应用交接</h2>
          <Link className="link" href="/projects">
            → 我的工程
          </Link>
        </div>
        <p className="muted" style={{ marginTop: 4 }}>
          粘贴来源应用(订单 / 活动 / 独立)下发的交接文档。接受只会创建或恢复工程;
          生成始终由本应用的开关独立控制,绝不因交接自动调用收费模型。
        </p>
        <form onSubmit={accept} style={{ marginTop: 12 }}>
          <div>
            <label>交接文档(order-handoff/v1 JSON)</label>
            <textarea
              rows={10}
              value={docText}
              onChange={(e) => setDocText(e.target.value)}
              placeholder='{"schema_version":"order-handoff/v1", …}'
              required
            />
          </div>
          <div className="row" style={{ marginTop: 8 }}>
            <div>
              <label>制作 purpose(用途,进入绑定唯一键)</label>
              <input
                value={purpose}
                onChange={(e) => setPurpose(e.target.value)}
                required
              />
            </div>
            <div style={{ flex: 0, alignSelf: "flex-end" }}>
              <button className="primary" type="submit" disabled={accepting}>
                {accepting ? "接受中…" : "接受交接"}
              </button>
            </div>
          </div>
        </form>
        {error ? <div className="banner">{error}</div> : null}
      </div>

      {result ? (
        <div className="card">
          <h2>续接结果</h2>
          <p>
            <span className="pill">{result.resolution}</span>{" "}
            <span className="pill">{result.snapshot_resolution}</span>{" "}
            {result.project ? (
              <Link className="link" href={`/projects/${result.project.id}`}>
                打开工程:{result.project.name}
              </Link>
            ) : null}
          </p>
          <h3>来源上下文(只读展示;来源文本仅作数据,不参与任何执行)</h3>
          <table>
            <tbody>
              <tr>
                <th>来源</th>
                <td>
                  {ctx?.source_kind} · {ctx?.source_app}
                  {ctx?.order_ref ? ` · 订单 ${ctx.order_ref}` : ""}
                  {ctx?.stage_ref ? ` · 阶段 ${ctx.stage_ref}` : ""}
                  {ctx?.campaign_ref ? ` · 活动 ${ctx.campaign_ref}` : ""}
                </td>
              </tr>
              <tr>
                <th>付款主体</th>
                <td>{ctx?.principal_id}</td>
              </tr>
              <tr>
                <th>需求版本</th>
                <td>{result.snapshot?.brief_version}</td>
              </tr>
              <tr>
                <th>用途 / 交付</th>
                <td>
                  {result.project?.usage_kind} ·{" "}
                  {ctx?.delivery_spec?.media_type}
                </td>
              </tr>
              <tr>
                <th>素材</th>
                <td>
                  {(ctx?.assets ?? []).length === 0
                    ? "—"
                    : (ctx?.assets ?? []).map((a, i) => (
                        <span className="pill" key={i}>
                          {a.asset_ref}
                        </span>
                      ))}
                </td>
              </tr>
              <tr>
                <th>授权范围</th>
                <td>{(ctx?.scopes ?? []).join(" / ")}</td>
              </tr>
            </tbody>
          </table>

          {result.pending_snapshot && result.diff ? (
            <div style={{ marginTop: 12 }}>
              <h3>新版需求待采用(不可变新快照;采用前不覆盖任何人工编辑)</h3>
              <table>
                <thead>
                  <tr>
                    <th>项</th>
                    <th>当前已采用</th>
                    <th>新版(待确认)</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>handoff</td>
                    <td>{result.diff?.old?.handoff_id ?? "—"}</td>
                    <td>{result.diff?.new?.handoff_id ?? "—"}</td>
                  </tr>
                  <tr>
                    <td>来源版本</td>
                    <td>{result.diff?.old?.source_revision ?? "—"}</td>
                    <td>{result.diff?.new?.source_revision ?? "—"}</td>
                  </tr>
                  <tr>
                    <td>需求版本</td>
                    <td>{result.diff?.old?.brief_version ?? "—"}</td>
                    <td>{result.diff?.new?.brief_version ?? "—"}</td>
                  </tr>
                </tbody>
              </table>
              <div className="row" style={{ marginTop: 8 }}>
                <div style={{ flex: 0 }}>
                  <button className="primary" onClick={adopt} disabled={adopting}>
                    {adopting ? "采用中…" : "确认差异并采用新版"}
                  </button>
                </div>
                {adoptMsg ? <span className="muted">{adoptMsg}</span> : null}
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
