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
interface SourceContext {
  source_kind?: string;
  source_app?: string;
  principal_id?: string;
  order_ref?: string;
  stage_ref?: string;
  campaign_ref?: string;
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
interface SnapshotItem {
  id: string;
  handoff_id: string;
  brief_version: string;
  source_revision: string;
}
interface BindingAgg {
  binding?: Binding;
  snapshots?: SnapshotItem[];
  source_context?: SourceContext;
}
interface OutputItem {
  id: string;
  platform_asset_id: string;
  file_name: string;
  result_sha256: string;
  result_size: number;
  media_type: string;
  brief_version: string;
  created_at: string;
}
interface ReceiptItem {
  id: string;
  output_id: string;
  event_id: string;
  target_app: string;
  status: string;
  attempts: number;
  last_error: string;
  updated_at: string;
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

  // ---- 跨应用续接(HUI-1745 I1):来源绑定 / 待采用新版 / 成果回执 ----
  const [binding, setBinding] = useState<Binding | null>(null);
  const [bindingAgg, setBindingAgg] = useState<BindingAgg | null>(null);
  const [pendingSnap, setPendingSnap] = useState<SnapshotItem | null>(null);
  const [returnURL, setReturnURL] = useState("");
  const [outputs, setOutputs] = useState<OutputItem[]>([]);
  const [receipts, setReceipts] = useState<ReceiptItem[]>([]);
  const [srcNotice, setSrcNotice] = useState("");
  const [outputFile, setOutputFile] = useState<File | null>(null);

  const loadSource = useCallback(async () => {
    if (!id) return;
    // 来源绑定(可空:standalone 完整保留,无来源不影响任何功能)。
    const bres = await fetch(`/api/bindings?project_id=${id}`);
    const bdata = await bres.json().catch(() => null);
    const b = (bdata?.bindings ?? [])[0] as Binding | undefined;
    setBinding(b ?? null);
    if (!b) return;
    const aggRes = await fetch(`/api/bindings/${b.id}`);
    const agg = (await aggRes.json().catch(() => null)) as BindingAgg | null;
    setBindingAgg(agg);
    // 待采用新版:绑定下最新快照 ≠ 当前已采用快照。
    const snaps = agg?.snapshots ?? [];
    setPendingSnap(
      snaps.length > 0 && snaps[0].id !== b.current_snapshot_id
        ? snaps[0]
        : null
    );
    // 成果与回执。
    const ores = await fetch(`/api/projects/${id}/outputs`);
    const odata = await ores.json().catch(() => null);
    setOutputs((odata?.outputs ?? []) as OutputItem[]);
    const rres = await fetch(`/api/projects/${id}/receipts`);
    const rdata = await rres.json().catch(() => null);
    setReceipts((rdata?.receipts ?? []) as ReceiptItem[]);
  }, [id]);

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
      await loadSource();
    })();
  }, [load, loadSource, router]);

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

  // ---- 来源面板动作(HUI-1745 I1) ------------------------------------------

  // 采用新版需求:仅移动绑定指针;人工编辑与已选输出不动。
  async function adoptPending() {
    if (!binding || !pendingSnap) return;
    setSrcNotice("");
    const res = await fetch(`/api/bindings/${binding.id}/adopt`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ snapshot_id: pendingSnap.id }),
    });
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setSrcNotice(data?.error?.message ?? `采用失败(HTTP ${res.status})`);
      return;
    }
    setSrcNotice("已采用新版需求(人工编辑与已选输出保留)");
    await loadSource();
  }

  // 返回来源:登记表白名单内的 launch target;解析失败仅提示,不影响编辑。
  async function goBackToSource() {
    if (!binding) return;
    setSrcNotice("");
    const res = await fetch(`/api/bindings/${binding.id}/return-target`);
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setSrcNotice(data?.error?.message ?? "来源暂不可返回;本工程可继续编辑");
      return;
    }
    setReturnURL(data?.url ?? "");
  }

  // 登记成果:本地选择合法 PNG → 经平台资产设施登记(不触发生成/扣费)。
  async function registerOutput(e: React.FormEvent) {
    e.preventDefault();
    if (!outputFile || !id) return;
    setSrcNotice("");
    const buf = await outputFile.arrayBuffer();
    let bin = "";
    const bytes = new Uint8Array(buf);
    for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    const res = await fetch(`/api/projects/${id}/outputs`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        file_name: outputFile.name,
        content_type: outputFile.type || "image/png",
        data_b64: btoa(bin),
      }),
    });
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setSrcNotice(data?.error?.message ?? `登记失败(HTTP ${res.status})`);
      return;
    }
    setSrcNotice(data?.duplicate ? "同内容成果已登记过(幂等返回)" : "成果已登记");
    setOutputFile(null);
    await loadSource();
  }

  // 回传来源:定向回执,只回传既有成果;失败不追加生成任务或扣费。
  async function sendReceipt(outputId: string) {
    setSrcNotice("");
    const res = await fetch(
      `/api/projects/${id}/outputs/${outputId}/receipt`,
      { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }
    );
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setSrcNotice(data?.error?.message ?? `回执失败(HTTP ${res.status})`);
      return;
    }
    const st = data?.receipt?.status;
    setSrcNotice(
      data?.duplicate ? "该成果已回执过(幂等返回)" : `回执已投递(${st})`
    );
    await loadSource();
  }

  // 查询恢复:失败/超时/重复场景只重传既有结果。
  async function resendReceipt(receiptId: string) {
    setSrcNotice("");
    const res = await fetch(`/api/receipts/${receiptId}/resend`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setSrcNotice(data?.error?.message ?? `重传失败(HTTP ${res.status})`);
      return;
    }
    setSrcNotice(data?.resent ? "已重传既有成果" : "已投递,无需重传");
    await loadSource();
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
              <span className="muted">(来源详情与回传入口见下方卡片)</span>
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

      {binding && bindingAgg ? (
        <div className="card">
          <h2>来源信息(跨应用续接)</h2>
          <table>
            <tbody>
              <tr>
                <th>来源</th>
                <td>
                  <span className="pill">
                    {bindingAgg.source_context?.source_kind} ·{" "}
                    {bindingAgg.source_context?.source_app}
                  </span>{" "}
                  {bindingAgg.source_context?.order_ref ? (
                    <span className="pill">
                      订单 {bindingAgg.source_context.order_ref}
                      {bindingAgg.source_context.stage_ref
                        ? ` · 阶段 ${bindingAgg.source_context.stage_ref}`
                        : ""}
                    </span>
                  ) : null}
                  {bindingAgg.source_context?.campaign_ref ? (
                    <span className="pill">
                      活动 {bindingAgg.source_context.campaign_ref}
                    </span>
                  ) : null}
                </td>
              </tr>
              <tr>
                <th>付款主体</th>
                <td>{bindingAgg.source_context?.principal_id ?? "—"}</td>
              </tr>
              <tr>
                <th>需求版本</th>
                <td>
                  {bindingAgg.snapshots?.find(
                    (s) => s.id === binding.current_snapshot_id
                  )?.brief_version ?? "—"}
                </td>
              </tr>
              <tr>
                <th>用途 / 交付</th>
                <td>
                  {binding.purpose} ·{" "}
                  {bindingAgg.source_context?.delivery_spec?.media_type ?? "—"}
                </td>
              </tr>
              <tr>
                <th>素材(交接输入)</th>
                <td>
                  {(bindingAgg.source_context?.assets ?? []).length === 0
                    ? "—"
                    : (bindingAgg.source_context?.assets ?? []).map((a, i) => (
                        <span className="pill" key={i}>
                          {a.asset_ref}
                        </span>
                      ))}
                </td>
              </tr>
              <tr>
                <th>授权范围</th>
                <td>
                  {(bindingAgg.source_context?.scopes ?? []).join(" / ") || "—"}
                </td>
              </tr>
            </tbody>
          </table>
          <div className="row" style={{ marginTop: 12 }}>
            <div style={{ flex: 0 }}>
              <button onClick={goBackToSource}>返回来源</button>
            </div>
            {returnURL ? (
              <div style={{ flex: 0 }}>
                <a className="link" href={returnURL} target="_blank" rel="noreferrer">
                  打开来源应用 ↗
                </a>
              </div>
            ) : null}
            <div style={{ flex: 2 }}>
              <span className="muted">
                来源文本仅作数据展示;续接编辑在本工程进行。
              </span>
            </div>
          </div>
          {pendingSnap ? (
            <div className="banner warn" style={{ marginTop: 8 }}>
              来源需求已有新版({pendingSnap.brief_version},handoff{" "}
              {pendingSnap.handoff_id})待确认。
              <button onClick={adoptPending} style={{ marginLeft: 8 }}>
                确认并采用新版
              </button>
              <span className="muted">
                (采用只切换需求版本;人工编辑与已选输出不会被覆盖)
              </span>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="card">
          <h2>来源信息</h2>
          <p className="muted" style={{ margin: 0 }}>
            独立制作(无来源绑定);不影响任何功能。需要从订单 / 活动续接时,在
            <Link className="link" href="/handoff">
              接受跨应用交接
            </Link>
            页粘贴交接文档。
          </p>
        </div>
      )}

      <div className="card">
        <h2>成果登记与回传来源</h2>
        <p className="muted" style={{ marginTop: 4 }}>
          明确选择输出版本后登记(合法 PNG,经平台资产设施);回传来源只发送既有成果的
          不可变引用与任务事实,回执失败不追加生成任务、不扣费。
        </p>
        <form onSubmit={registerOutput} className="row">
          <div style={{ flex: 2 }}>
            <label>选择成果文件(仅 PNG)</label>
            <input
              type="file"
              accept="image/png"
              onChange={(e) => setOutputFile(e.target.files?.[0] ?? null)}
            />
          </div>
          <div style={{ flex: 0, alignSelf: "flex-end" }}>
            <button className="primary" type="submit" disabled={!outputFile}>
              登记成果
            </button>
          </div>
        </form>
        {outputs.length === 0 ? (
          <p className="muted">尚未登记成果。</p>
        ) : (
          <table style={{ marginTop: 12 }}>
            <thead>
              <tr>
                <th>文件</th>
                <th>资产引用</th>
                <th>需求版本</th>
                <th>大小</th>
                <th>登记时间</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {outputs.map((o) => {
                const rcpt = receipts.find((r) => r.output_id === o.id);
                return (
                  <tr key={o.id}>
                    <td>{o.file_name || "—"}</td>
                    <td>{o.platform_asset_id}</td>
                    <td>{o.brief_version || "—"}</td>
                    <td>{o.result_size}</td>
                    <td className="muted">{o.created_at}</td>
                    <td>
                      {binding ? (
                        rcpt ? (
                          <span>
                            <span className="pill">
                              回执 {rcpt.status}
                              {rcpt.attempts > 1 ? ` ×${rcpt.attempts}` : ""}
                            </span>{" "}
                            {rcpt.status !== "delivered" ? (
                              <button onClick={() => resendReceipt(rcpt.id)}>
                                重传
                              </button>
                            ) : null}
                          </span>
                        ) : (
                          <button onClick={() => sendReceipt(o.id)}>
                            回传来源
                          </button>
                        )
                      ) : (
                        <span className="muted">无来源,不回传</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {srcNotice ? (
          <div className="banner warn" style={{ marginTop: 8 }}>
            {srcNotice}
          </div>
        ) : null}
      </div>
    </div>
  );
}
