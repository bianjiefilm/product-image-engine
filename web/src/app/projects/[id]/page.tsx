"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { usePublishWorkbench } from "@/components/eco-nav/workbench";
import { SubjectFidelityPanel } from "@/components/SubjectFidelityPanel";
import { sourceLabel } from "@/lib/eco-nav";
import { jsonBody, jsonHeaders } from "@/lib/api-client";
import { projectSourceLabel, projectStatusLabel } from "@/lib/project-labels";
import styles from "./project-detail.module.css";
import { BackgroundReplacePanel } from "@/components/background-replace/Panel";
import { LightScenePanel } from "@/components/light-scene/Panel";
import { ShowcaseVideoPanel } from "@/components/showcase-video/Panel";
import { TextImagePanel } from "@/components/text-image/Panel";
import { PhotoUpload } from "@/components/plate-lock/PhotoUpload";
import { RecoveryStrip } from "@/components/recovery/RecoveryStrip";
import { MotionRefNote } from "@/components/motion-ref/MotionRefNote";
import { RevisionRail } from "@/components/revision/RevisionRail";
import { SourceImagePanel } from "@/components/source-image/Panel";

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
interface SizePreset {
  name: string;
  width: number;
  height: number; // 0 = fit-width(等比)
  mode: string; // pad | cover
  label?: string;
}
interface VariantItem {
  id: string;
  file_name: string;
  preset_name: string;
  variant_mode: string;
  variant_width: number;
  variant_height: number;
  result_size: number;
  media_type: string;
  created_at: string;
}

function versionFact(status: string): string {
  if (status === "adopted") return "已采用";
  if (!status) return "未采用";
  if (status === "failed" || status === "error" || status === "rejected") return "失败";
  return "状态未知";
}

function receiptFact(status: string): string {
  if (status === "delivered") return "已送达";
  if (status === "failed") return "回执失败";
  if (status === "pending") return "待送达";
  return "状态未知";
}

const TECHNICAL_LEAK = /provider|qwen|\bmodel\b|sha256|run_id|\bdebug\b/i;

function publicOrTechnical(raw: string): { show: string; technical: string } {
  if (!raw) return { show: "", technical: "" };
  if (TECHNICAL_LEAK.test(raw)) {
    return {
      show: "失败原因含技术细节，已放进技术详情。没有伪造成功，也没有另扣费用。",
      technical: raw,
    };
  }
  return { show: raw, technical: "" };
}

function PublicFact({ raw, tone = "warn" }: { raw: string; tone?: "warn" | "plain" }) {
  const fact = publicOrTechnical(raw);
  if (!fact.show) return null;
  return (
    <>
      {tone === "plain" ? (
        <span className="muted">{fact.show}</span>
      ) : (
        <div className="banner warn" role="alert">{fact.show}</div>
      )}
      {fact.technical ? (
        <details data-technical="true">
          <summary>技术详情</summary>
          <p className="muted">{fact.technical}</p>
        </details>
      ) : null}
    </>
  );
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

  // ---- 尺寸适配(HUI-1703 FEAT-0204):预设探测 off → 面板隐藏 ----
  const [sizePresets, setSizePresets] = useState<SizePreset[] | null>(null);
  const [variants, setVariants] = useState<VariantItem[]>([]);
  const [saSource, setSaSource] = useState("");
  const [saFile, setSaFile] = useState<File | null>(null);
  const [saSelected, setSaSelected] = useState<string[]>([]);
  const [saFormat, setSaFormat] = useState("png");
  const [saNotice, setSaNotice] = useState("");
  const [saBusy, setSaBusy] = useState(false);

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
    const rt = await fetch(`/api/bindings/${b.id}/return-target`);
    if (rt.ok) {
      const rtData = await rt.json().catch(() => null);
      if (typeof rtData?.url === "string" && rtData.url) setReturnURL(rtData.url);
    }
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
    // 尺寸适配探测:预设 404(开关 off)→ 面板整体隐藏;on → 拉变体列表。
    const presRes = await fetch(`/api/size-adapt/presets`);
    if (presRes.ok) {
      const pdata = await presRes.json().catch(() => null);
      setSizePresets((pdata?.presets ?? []) as SizePreset[]);
      const vres = await fetch(`/api/projects/${id}/size-adapt`);
      const vdata = await vres.json().catch(() => null);
      setVariants((vdata?.variants ?? []) as VariantItem[]);
    } else {
      setSizePresets(null);
      setVariants([]);
    }
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
        headers: jsonHeaders,
        body: jsonBody(form),
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
      headers: jsonHeaders,
      body: jsonBody(newInput),
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

  // 产品照片上传(HUI-1697 FEAT-0198):文件经本产品 BFF 中继上传,
  // 服务端单点校验(主体/租户/用途/大小/魔数/真解码/内容幂等);
  // 成功后把返回的资产引用挂接到本工程(同内容重传走幂等,不重复登记)。
  const [photoFile, setPhotoFile] = useState<File | null>(null);
  const [photoBusy, setPhotoBusy] = useState(false);
  // Bumped whenever a photo is attached so the plate-lock panel re-reads which inputs it supports.
  const [plateReload, setPlateReload] = useState(0);

  async function uploadPhoto(e: React.FormEvent) {
    e.preventDefault();
    if (!photoFile) return;
    setPhotoBusy(true);
    setNotice("");
    try {
      const bytes = await photoFile.arrayBuffer();
      const qs = new URLSearchParams({
        purpose: "project_photo",
        filename: photoFile.name,
      });
      const up = await fetch(`/api/photos?${qs.toString()}`, {
        method: "POST",
        body: bytes,
      });
      const upData = await up.json().catch(() => null);
      if (!up.ok) {
        setNotice(upData?.error?.message ?? `上传失败(HTTP ${up.status})`);
        return;
      }
      const photo = upData?.photo ?? {};
      const attach = await fetch(`/api/projects/${id}/inputs`, {
        method: "POST",
        headers: jsonHeaders,
        body: jsonBody({
          platform_asset_id: photo.platform_asset_id ?? "",
          snapshot_name: photo.original_name ?? photoFile.name,
          snapshot_size: photo.size_bytes ?? bytes.byteLength,
          snapshot_content_type: photo.media_type ?? "application/octet-stream",
        }),
      });
      const attachData = await attach.json().catch(() => null);
      if (!attach.ok) {
        setNotice(
          attachData?.error?.message ?? "上传成功,但挂接工程失败(资产已保存)"
        );
        return;
      }
      setNotice(
        upData?.idempotent ? "同内容已存在(幂等),引用已挂接" : "上传成功,已挂接到工程"
      );
      setPhotoFile(null);
      await load();
      setPlateReload((k) => k + 1);
    } finally {
      setPhotoBusy(false);
    }
  }

  // 提交生成(占位链路):开关 off / 服务不可达时展示明确的失败事实。
  async function submitGeneration() {
    setNotice("");
    const res = await fetch(`/api/projects/${id}/versions`, {
      method: "POST",
      headers: jsonHeaders,
      body: jsonBody({}),
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
    setBalance(typeof data?.balance_cny === "string" || typeof data?.balance_cny === "number" ? `余额 ¥${data.balance_cny}` : "费用待确认");
  }

  // ---- 来源面板动作(HUI-1745 I1) ------------------------------------------

  // 采用新版需求:仅移动绑定指针;人工编辑与已选输出不动。
  async function adoptPending() {
    if (!binding || !pendingSnap) return;
    setSrcNotice("");
    const res = await fetch(`/api/bindings/${binding.id}/adopt`, {
      method: "POST",
      headers: jsonHeaders,
      body: jsonBody({ snapshot_id: pendingSnap.id }),
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
      headers: jsonHeaders,
      body: jsonBody({
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

  // ---- 尺寸适配动作(HUI-1703 FEAT-0204) -------------------------------------

  function togglePreset(name: string) {
    setSaSelected((cur) =>
      cur.includes(name) ? cur.filter((n) => n !== name) : [...cur, name]
    );
  }

  // 生成尺寸变体:纯确定性变换,不触发生成/扣费。成果登记只存平台引用,
  // 每次生成需重携源 PNG 字节,服务端按 sha256 与登记成果绑定校验。
  // 同 (源,预设,模式) 重复请求幂等返回既有变体(不重复变换)。
  async function generateVariants() {
    if (!id || !saFile || !saSource || saSelected.length === 0) return;
    setSaNotice("");
    setSaBusy(true);
    try {
      const buf = await saFile.arrayBuffer();
      const bytes = new Uint8Array(buf);
      let bin = "";
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      const dataB64 = btoa(bin);
      const made: string[] = [];
      const dups: string[] = [];
      for (const p of saSelected) {
        const res = await fetch(`/api/projects/${id}/size-adapt`, {
          method: "POST",
          headers: jsonHeaders,
          body: jsonBody({
            source_output_id: saSource,
            preset_name: p,
            format: saFormat,
            data_b64: dataB64,
          }),
        });
        const data = await res.json().catch(() => null);
        if (!res.ok) {
          setSaNotice(
            data?.error?.message ?? `生成失败(HTTP ${res.status},预设 ${p})`
          );
          return;
        }
        (data?.duplicate ? dups : made).push(p);
      }
      const parts: string[] = [];
      if (made.length) parts.push(`已生成:${made.join("、")}`);
      if (dups.length) parts.push(`已有变体(幂等返回):${dups.join("、")}`);
      setSaNotice(parts.join(";"));
      await loadSource();
    } finally {
      setSaBusy(false);
    }
  }

  const navLabel = sourceLabel({
    sourceType: project?.source_type || "",
    sourceRef: project?.source_ref || "",
    campaignRef: bindingAgg?.source_context?.campaign_ref,
    orderRef: bindingAgg?.source_context?.order_ref,
  });
  usePublishWorkbench(
    project
      ? {
          sourceType: project.source_type || "standalone",
          sourceLabel: navLabel,
          sourceTenantId: null,
          returnHref: returnURL || null,
          enterpriseAssets: false,
        }
      : null
  );

  if (loadError) {
    const opened = publicOrTechnical(loadError);
    return (
      <section id="recovery" className={`card ${styles.shell}`} aria-label="出错与恢复" data-state="error">
        <h2>这个工程没有打开</h2>
        <div className="banner" role="alert">{opened.show}</div>
        {opened.technical ? (
          <details data-technical="true">
            <summary>技术详情</summary>
            <p className="muted">{opened.technical}</p>
          </details>
        ) : null}
        <p className="muted">不会重新做图，也不会再扣一次。</p>
        <div className="media-actions">
          <Link className="link" href="/projects">返回工程列表</Link>
          <Link className="link" href="/start">开始做产品图</Link>
        </div>
      </section>
    );
  }
  if (!project) return <div className={`card muted ${styles.shell}`} data-state="loading">加载中…</div>;

  const sourceKind = bindingAgg?.source_context?.source_kind ?? "";
  const sourceApp = bindingAgg?.source_context?.source_app ?? "";
  const principalId = bindingAgg?.source_context?.principal_id ?? "";

  const benchMaterials = (
    <>
      <div className="card">
        <div className="row row-center">
          <h2 className="row-title">{project.name}</h2>
          <Link className="link" href="/projects">
            ← 返回工程列表
          </Link>
        </div>
        <p className="muted mt-4">
          来源:
          {project.source_type ? (
            <>
              <span className="pill">
                {projectSourceLabel(project.source_type)} · {project.source_ref}
              </span>{" "}
              <span className="muted">(来源详情与回传入口见下方卡片)</span>
            </>
          ) : (
            "独立制作(无来源,不影响任何功能)"
          )}
        </p>
      </div>

      <div className="card">
        <h2>简报</h2>
        <form onSubmit={save}>
          <div className={styles.briefBar}>
            <dl className={styles.factDl}>
              <div>
                <dt>状态</dt>
                <dd><span className="pill">{projectStatusLabel(project.status)}</span></dd>
              </div>
              <div>
                <dt>用途</dt>
                <dd>{project.usage_kind || "未填写"}</dd>
              </div>
              <div>
                <dt>尺寸</dt>
                <dd>{project.width_px}×{project.height_px} px</dd>
              </div>
            </dl>
            <div className="media-actions">
              <button className="primary" type="submit" disabled={saving}>
                {saving ? "保存中…" : "保存简报"}
              </button>
            </div>
          </div>
          <details>
            <summary>改简报</summary>
          <div className="row">
            <div>
              <label htmlFor="proj-name">名称</label>
              <input
                id="proj-name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                required
              />
            </div>
            <div>
              <label htmlFor="proj-status">状态</label>
              <select
                id="proj-status"
                value={form.status}
                onChange={(e) => setForm({ ...form, status: e.target.value })}
              >
                <option value="draft">草稿</option>
                <option value="active">进行中</option>
                <option value="archived">已归档</option>
              </select>
            </div>
          </div>
          <div className="row">
            <div>
              <label htmlFor="proj-usage">用途</label>
              <input
                id="proj-usage"
                value={form.usage_kind}
                onChange={(e) =>
                  setForm({ ...form, usage_kind: e.target.value })
                }
              />
            </div>
            <div>
              <label htmlFor="proj-width">宽(px)</label>
              <input
                id="proj-width"
                type="number"
                value={form.width_px}
                onChange={(e) =>
                  setForm({ ...form, width_px: Number(e.target.value) })
                }
              />
            </div>
            <div>
              <label htmlFor="proj-height">高(px)</label>
              <input
                id="proj-height"
                type="number"
                value={form.height_px}
                onChange={(e) =>
                  setForm({ ...form, height_px: Number(e.target.value) })
                }
              />
            </div>
          </div>
          </details>
        </form>
        {notice ? <PublicFact raw={notice} /> : null}
      </div>

      <div className="card">
        <h2>商品素材</h2>
        {inputs.length === 0 ? (
          <p className="muted" data-state="empty">尚未挂接素材。</p>
        ) : (
          <ul className="asset-strip">
            {inputs.map((i) => (
              <li key={i.id}>
                <img alt={i.snapshot_name || "商品素材"} src={`/api/projects/${id}/inputs/${i.id}/content`} />
                <div>{i.snapshot_name || "未命名素材"}</div>
                <a href={`/api/projects/${id}/inputs/${i.id}/content`} target="_blank" rel="noreferrer">查看</a>
                {" "}
                <button type="button" onClick={() => removeInput(i.id)}>移除</button>
              </li>
            ))}
          </ul>
        )}
        <details data-technical="true">
          <summary>素材引用</summary>
        {inputs.length === 0 ? null : (
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
                    <a
                      href={`/api/projects/${id}/inputs/${i.id}/content`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      查看
                    </a>{" "}
                    <button onClick={() => removeInput(i.id)}>移除</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        </details>
        <form onSubmit={uploadPhoto} className="mt-12">
          <div className="media-actions">
            <button className="primary" type="submit" disabled={!photoFile || photoBusy}>
              {photoBusy ? "上传中…" : "上传并挂接"}
            </button>
            <button type="submit" form="proj-attach-form">挂接素材</button>
          </div>
          <div className="row">
            <div>
              <label htmlFor="proj-photo-upload">上传产品照片(jpeg/png/webp,服务端核验)</label>
              <input
                id="proj-photo-upload"
                type="file"
                accept="image/jpeg,image/png,image/webp"
                onChange={(e) => setPhotoFile(e.target.files?.[0] ?? null)}
              />
            </div>
          </div>
        </form>
        <details data-technical="true">
          <summary>已有素材引用</summary>
        <form id="proj-attach-form" onSubmit={addInput} className="mt-12">
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
          </div>
        </form>
        <p className="muted mb-0">
          上传经本产品 BFF 受限中继:服务端核验格式/大小/用途并做内容幂等(同内容重传不重复登记);
          也可直接粘贴平台素材引用保存(跨应用图片为经授权的版本引用,经平台设施解析,不读其他应用数据库)。
        </p>
        </details>
      </div>
    </>
  );

  return (
    <div className={styles.shell}>
      <SourceImagePanel
        mode="background_plate_lock"
        projectId={id ?? ""}
        returnEnabled={!!binding}
        reloadKey={plateReload}
        onReturned={() => void load()}
        materialsSlot={benchMaterials}
        railSlot={
          <>
            <RevisionRail projectId={id ?? ""} />
            <MotionRefNote projectId={id ?? ""} />
            <RecoveryStrip projectId={id ?? ""} />
            {id ? <SubjectFidelityPanel projectId={id} /> : null}
          </>
        }
        uploadSlot={<PhotoUpload projectId={id ?? ""} onAttached={() => setPlateReload((k) => k + 1)} />}
      />
      <div className="media-follow">
      <BackgroundReplacePanel
        projectId={id ?? ""}
        inputs={inputs.map((item) => ({ id: item.id, snapshot_name: item.snapshot_name }))}
        onInputsChanged={() => {
          void load();
        }}
      />

      <SourceImagePanel projectId={id ?? ""} />
      <TextImagePanel projectId={id ?? ""} historyOnly />

      <ShowcaseVideoPanel
        projectId={id ?? ""}
        images={[
          ...inputs.map((item) => ({
            id: item.id,
            label: item.snapshot_name || "未命名素材",
          })),
          ...outputs.map((item) => ({
            id: item.id,
            label: item.file_name || "未命名成果",
          })),
        ]}
      />

      <LightScenePanel
        projectId={id ?? ""}
        inputs={inputs.map((item) => ({ id: item.id, snapshot_name: item.snapshot_name }))}
      />

      <div className="card">
        <h2>任务与费用事实</h2>
        <div className="media-actions">
          <button className="primary" onClick={submitGeneration}>提交生成</button>
          <button onClick={loadBalance}>读取费用事实</button>
        </div>
        {balance ? <PublicFact raw={balance} tone="plain" /> : null}
        {versions.length === 0 ? (
          <p className="muted">暂无输出版本。</p>
        ) : (
          <ul className={styles.factList} aria-label="输出版本">
            {versions.map((v) => (
              <li key={v.id} className={styles.factCard}>
                <strong>v{v.version_no}</strong>
                <span className="pill">{versionFact(v.status)}</span>
                <span className="muted">{v.created_at || "时间未知"}</span>
              </li>
            ))}
          </ul>
        )}
        <p className="muted mb-0">
          生成能力与计费开关默认关闭;失败时这里只会展示真实原因,不会伪造成功。
        </p>
        <details data-technical="true">
          <summary>版本技术详情</summary>
          {versions.length === 0 ? (
            <p className="muted">暂无输出版本。</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>版本</th>
                  <th>状态</th>
                  <th>创建时间</th>
                </tr>
              </thead>
              <tbody>
                {versions.map((v) => (
                  <tr key={v.id}>
                    <td>v{v.version_no}</td>
                    <td>{v.status || "未采用"}</td>
                    <td className="muted">{v.created_at || "时间未知"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </details>
      </div>

      {binding && bindingAgg ? (
        <div className="card">
          <h2>来源信息(跨应用续接)</h2>
          <div className="media-actions">
            <button onClick={goBackToSource}>返回来源</button>
            {returnURL ? (
              <a className="link" href={returnURL} target="_blank" rel="noreferrer">
                打开来源应用 ↗
              </a>
            ) : null}
          </div>
          <dl className={styles.factDl}>
            <div>
              <dt>来源</dt>
              <dd><span className="pill">{projectSourceLabel(sourceKind) || "来源未知"}</span></dd>
            </div>
            <div>
              <dt>订单</dt>
              <dd>{bindingAgg.source_context?.order_ref || "无"}</dd>
            </div>
            <div>
              <dt>阶段</dt>
              <dd>{bindingAgg.source_context?.stage_ref || "无"}</dd>
            </div>
            <div>
              <dt>活动</dt>
              <dd>{bindingAgg.source_context?.campaign_ref || "无"}</dd>
            </div>
            <div>
              <dt>需求版本</dt>
              <dd>
                {bindingAgg.snapshots?.find((s) => s.id === binding.current_snapshot_id)?.brief_version || "—"}
              </dd>
            </div>
            <div>
              <dt>用途</dt>
              <dd>{binding.purpose || "—"}</dd>
            </div>
            <div>
              <dt>交付</dt>
              <dd>{bindingAgg.source_context?.delivery_spec?.media_type || "—"}</dd>
            </div>
            <div>
              <dt>素材</dt>
              <dd>{(bindingAgg.source_context?.assets ?? []).length} 个</dd>
            </div>
          </dl>
          <p className="muted">来源文本仅作数据展示;续接编辑在本工程进行。</p>
          {pendingSnap ? (
            <div className="banner warn mt-8">
              来源需求已有新版({pendingSnap.brief_version})待确认。
              <button onClick={adoptPending} className="banner-action">
                确认并采用新版
              </button>
              <span className="muted">
                (采用只切换需求版本;人工编辑与已选输出不会被覆盖)
              </span>
            </div>
          ) : null}
          <details data-technical="true">
            <summary>来源技术详情</summary>
            <table>
              <tbody>
                <tr>
                  <th>来源</th>
                  <td>{sourceKind || "—"} · {sourceApp || "—"}</td>
                </tr>
                <tr>
                  <th>付款主体</th>
                  <td>{principalId || "—"}</td>
                </tr>
                <tr>
                  <th>交接</th>
                  <td>{pendingSnap?.handoff_id || "—"}</td>
                </tr>
                <tr>
                  <th>素材引用</th>
                  <td>
                    {(bindingAgg.source_context?.assets ?? []).length === 0
                      ? "—"
                      : (bindingAgg.source_context?.assets ?? []).map((a, i) => (
                          <span className="pill" key={i}>{a.asset_ref || "—"}</span>
                        ))}
                  </td>
                </tr>
                <tr>
                  <th>授权范围</th>
                  <td>{(bindingAgg.source_context?.scopes ?? []).join(" / ") || "—"}</td>
                </tr>
              </tbody>
            </table>
          </details>
        </div>
      ) : (
        <div className="card">
          <h2>来源信息</h2>
          <p className="muted m-0">
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
        <p className="muted mt-4">
          明确选择输出版本后登记(合法 PNG,经平台资产设施);回传来源只发送既有成果的
          不可变引用与任务事实,回执失败不追加生成任务、不扣费。
        </p>
        <form onSubmit={registerOutput}>
          <div className="media-actions">
            <button className="primary" type="submit" disabled={!outputFile}>
              登记成果
            </button>
          </div>
          <div>
            <label htmlFor="output-file">选择成果文件(仅 PNG)</label>
            <input
              id="output-file"
              type="file"
              accept="image/png"
              onChange={(e) => setOutputFile(e.target.files?.[0] ?? null)}
            />
          </div>
        </form>
        {outputs.length === 0 ? (
          <p className="muted">尚未登记成果。</p>
        ) : (
          <ul className={styles.factList} aria-label="已登记成果">
            {outputs.map((o) => {
              const rcpt = receipts.find((r) => r.output_id === o.id);
              return (
                <li key={o.id} className={styles.factCard}>
                  <strong>{o.file_name || "未命名成果"}</strong>
                  <span>需求版本 {o.brief_version || "—"}</span>
                  <span>大小 {o.result_size}</span>
                  <span className="muted">{o.created_at || "时间未知"}</span>
                  {binding ? (
                    rcpt ? (
                      <span>
                        <span className="pill">
                          {receiptFact(rcpt.status)}
                          {rcpt.attempts > 1 ? ` ×${rcpt.attempts}` : ""}
                        </span>{" "}
                        {rcpt.status !== "delivered" ? (
                          <button onClick={() => resendReceipt(rcpt.id)}>重传</button>
                        ) : null}
                      </span>
                    ) : (
                      <button onClick={() => sendReceipt(o.id)}>回传来源</button>
                    )
                  ) : (
                    <span className="muted">无来源,不回传</span>
                  )}
                </li>
              );
            })}
          </ul>
        )}
        <details data-technical="true">
          <summary>成果技术详情</summary>
          {outputs.length === 0 ? (
            <p className="muted">尚未登记成果。</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>文件</th>
                  <th>资产引用</th>
                  <th>需求版本</th>
                  <th>大小</th>
                  <th>登记时间</th>
                  <th>回执</th>
                  <th>失败原因</th>
                </tr>
              </thead>
              <tbody>
                {outputs.map((o) => {
                  const rcpt = receipts.find((r) => r.output_id === o.id);
                  return (
                    <tr key={o.id}>
                      <td>{o.file_name || "—"}</td>
                      <td>{o.platform_asset_id || "—"}</td>
                      <td>{o.brief_version || "—"}</td>
                      <td>{o.result_size}</td>
                      <td className="muted">{o.created_at || "时间未知"}</td>
                      <td>{rcpt ? `${rcpt.status || "状态未知"} ×${rcpt.attempts}` : "无回执"}</td>
                      <td>{rcpt?.last_error || "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </details>
        {srcNotice ? <PublicFact raw={srcNotice} /> : null}
      </div>

      {sizePresets ? (
        <div className="card">
          <h2>尺寸适配(电商规格)</h2>
          <div className="media-actions">
            <button
              className="primary"
              onClick={generateVariants}
              disabled={saBusy || !saFile || !saSource || saSelected.length === 0}
            >
              {saBusy ? "生成中…" : `生成 ${saSelected.length} 个变体`}
            </button>
          </div>
          <p className="muted mt-4">
            纯确定性变换(等比缩放 + 白底填充 / 居中裁切),不调用 AI、不产生任务与费用。
            预设为公开常见规格整理,商家可自定义覆盖;以各平台当时官方要求为准。
            成果登记只存平台引用,生成时请重新提供对应源 PNG,服务端会核对与已登记成果是否为同一文件。
          </p>
          <div className="row">
            <div>
              <label htmlFor="sa-source">源成果</label>
              <select
                id="sa-source"
                value={saSource}
                onChange={(e) => setSaSource(e.target.value)}
              >
                <option value="">— 选择 —</option>
                {outputs.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.file_name || o.id}({o.result_size}B)
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label htmlFor="sa-png">源 PNG 文件</label>
              <input
                id="sa-png"
                type="file"
                accept="image/png"
                onChange={(e) => setSaFile(e.target.files?.[0] ?? null)}
              />
            </div>
            <div>
              <label htmlFor="sa-format">输出格式</label>
              <select
                id="sa-format"
                value={saFormat}
                onChange={(e) => setSaFormat(e.target.value)}
              >
                <option value="png">PNG</option>
                <option value="jpeg">JPEG(quality 90)</option>
              </select>
            </div>
          </div>
          <div className="mt-8">
            <label>目标规格</label>
            <div className="row row-gap-8">
              {sizePresets.map((p) => (
                <label
                  key={p.name}
                  className={`muted flex-none ${styles.check}`}
                >
                  <input
                    type="checkbox"
                    checked={saSelected.includes(p.name)}
                    onChange={() => togglePreset(p.name)}
                  />{" "}
                  {p.label || p.name}({p.width}×
                  {p.height > 0 ? p.height : "等比"} ·{" "}
                  {p.mode === "cover" ? "裁切" : "白底"})
                </label>
              ))}
            </div>
          </div>
          {saNotice ? <PublicFact raw={saNotice} /> : null}
          {variants.length > 0 ? (
            <ul className={styles.factList} aria-label="尺寸变体">
              {variants.map((v) => (
                <li key={v.id} className={styles.factCard}>
                  <strong>{v.file_name || "未命名变体"}</strong>
                  <span className="pill">{v.preset_name}</span>
                  <span>{v.variant_width}×{v.variant_height}</span>
                  <span>{v.variant_mode === "cover" ? "裁切" : "白底"}</span>
                  <span>大小 {v.result_size}</span>
                  <span className="muted">{v.created_at || "时间未知"}</span>
                  <a className="link" href={`/api/projects/${id}/size-adapt/${v.id}/download`}>下载</a>
                </li>
              ))}
            </ul>
          ) : (
            <p className="muted">尚无尺寸变体。</p>
          )}
          <details data-technical="true">
            <summary>尺寸技术详情</summary>
            <p className="muted">服务端按 sha256 与登记成果绑定校验。不一致就不会当成同一张成果。</p>
            {variants.length > 0 ? (
              <table>
                <thead>
                  <tr>
                    <th>缩略名</th>
                    <th>预设</th>
                    <th>尺寸</th>
                    <th>模式</th>
                    <th>大小</th>
                    <th>时间</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {variants.map((v) => (
                    <tr key={v.id}>
                      <td>{v.file_name || v.id}</td>
                      <td><span className="pill">{v.preset_name}</span></td>
                      <td>{v.variant_width}×{v.variant_height}</td>
                      <td>{v.variant_mode === "cover" ? "裁切" : "白底"}</td>
                      <td>{v.result_size}</td>
                      <td className="muted">{v.created_at || "时间未知"}</td>
                      <td>
                        <a className="link" href={`/api/projects/${id}/size-adapt/${v.id}/download`}>下载</a>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="muted">尚无尺寸变体。</p>
            )}
          </details>
        </div>
      ) : null}
      </div>
    </div>
  );
}
