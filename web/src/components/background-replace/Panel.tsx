"use client";

import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  BILLING_PENDING,
  HONESTY,
  PROTECTION_SCOPE,
  REAL_GENERATION_INCOMPLETE,
  billingLine,
  quoteStale,
  modelPlatePreview,
  showCreative,
  subjectLockPreview,
} from "@/lib/bg-replace";

interface InputOpt {
  id: string;
  snapshot_name: string;
}

interface Caps {
  creative_available?: boolean;
  billing_label?: string;
  honesty?: string;
  production_generation_passed?: boolean;
  billing_passed?: boolean;
  real_generation_notice?: string;
}

interface Checks {
  logo?: string;
  packaging_text?: string;
  spec?: string;
  structure?: string;
}

interface Job {
  id: string;
  input_id: string;
  input_version: string;
  mode: string;
  protected_region: string;
  background_intent: string;
  job_status: string;
  quality: string;
  quote_status: string;
  billing_label: string;
  selection: string;
  pending: string[];
  allowed_uses: string[];
  output_asset_id: string;
  output_version: string;
  honesty: string;
  production_generation_passed: boolean;
  billing_passed: boolean;
  platform_task_id: string;
  model_ref?: string;
  fee_ref?: string;
  real_generation_completed?: boolean;
  real_generation_notice?: string;
  product_checks?: Checks;
  export_count?: number;
  origin?: string;
}

interface Sample {
  kind: string;
  label: string;
  quality: string;
  model_usage: string;
  cost_label: string;
}

type AxisKey = "logo" | "packaging_text" | "spec" | "structure";
const modeLabel: Record<string, string> = { fidelity: "保真", creative: "创意" };
const axisLabel: Record<AxisKey, string> = {
  logo: "Logo",
  packaging_text: "包装文字",
  spec: "规格",
  structure: "结构",
};

export function BackgroundReplacePanel({
  projectId,
  inputs,
  onInputsChanged,
}: {
  projectId: string;
  inputs: InputOpt[];
  onInputsChanged?: () => void;
}) {
  const [hidden, setHidden] = useState(false);
  const [caps, setCaps] = useState<Caps | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [samples, setSamples] = useState<Sample[]>([]);
  const [inputId, setInputId] = useState("");
  const [mode, setMode] = useState("fidelity");
  const [intent, setIntent] = useState("室内棚拍");
  const [active, setActive] = useState<Job | null>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [plateFile, setPlateFile] = useState<File | null>(null);
  const [maskFile, setMaskFile] = useState<File | null>(null);
  const [axes, setAxes] = useState<Record<AxisKey, string>>({
    logo: "unknown",
    packaging_text: "unknown",
    spec: "unknown",
    structure: "unknown",
  });
  const [similarity, setSimilarity] = useState("");
  const [similarityOnly, setSimilarityOnly] = useState(false);

  useEffect(() => {
    let gone = false;
    (async () => {
      const res = await fetch("/api/background-replacements/capabilities");
      if (res.status === 404) {
        if (!gone) setHidden(true);
        return;
      }
      const data = await res.json().catch(() => null);
      if (!gone && res.ok) setCaps(data);
      const acc = await fetch("/api/background-replacements/acceptance");
      if (acc.ok) {
        const body = await acc.json().catch(() => null);
        if (!gone) setSamples(body?.samples ?? []);
      }
      const list = await fetch(`/api/projects/${projectId}/background-replacements`);
      if (list.ok) {
        const body = await list.json().catch(() => null);
        const rows = (body?.jobs ?? []) as Job[];
        if (!gone) {
          setJobs(rows);
          if (rows[0]) {
            setActive(rows[0]);
            setInputId(rows[0].input_id);
            if (rows[0].background_intent) setIntent(rows[0].background_intent);
            if (rows[0].mode === "fidelity") setMode("fidelity");
          }
        }
      }
    })();
    return () => {
      gone = true;
    };
  }, [projectId]);

  useEffect(() => {
    if (!inputId && inputs[0]) setInputId(inputs[0].id);
  }, [inputId, inputs]);

  useEffect(() => {
    if (!showCreative(Boolean(caps?.creative_available)) && mode === "creative") setMode("fidelity");
  }, [caps, mode]);

  if (hidden) return null;
  const creativeOn = showCreative(Boolean(caps?.creative_available));
  const bill = billingLine(caps?.billing_label ?? active?.billing_label);
  const quoted = active
    ? { mode: active.mode, inputId: active.input_id, intent: active.background_intent }
    : null;
  const stale = quoted ? quoteStale(quoted, { mode, inputId, intent }) : false;
  const formBody = { mode, input_id: inputId, background_intent: intent, protected_region: PROTECTION_SCOPE };
  const realNotice = active?.real_generation_notice || caps?.real_generation_notice || REAL_GENERATION_INCOMPLETE;

  async function send(path: string, method: string, body?: unknown) {
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(path, {
        method,
        headers: body ? { "Content-Type": "application/json" } : undefined,
        body: body ? JSON.stringify(body) : undefined,
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setNotice(data?.error?.message ?? "请求失败");
        return null;
      }
      return data;
    } finally {
      setBusy(false);
    }
  }

  async function upload() {
    if (!file) return;
    setBusy(true);
    setNotice("");
    try {
      const bytes = await file.arrayBuffer();
      const qs = new URLSearchParams({ purpose: "project_photo", filename: file.name });
      const up = await fetch(`/api/photos?${qs.toString()}`, { method: "POST", body: bytes });
      const upData = await up.json().catch(() => null);
      if (!up.ok) {
        setNotice(upData?.error?.message ?? "上传失败");
        return;
      }
      const photo = upData?.photo ?? {};
      const attach = await fetch(`/api/projects/${projectId}/inputs`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          platform_asset_id: photo.platform_asset_id ?? "",
          snapshot_name: photo.original_name ?? file.name,
          snapshot_size: photo.size_bytes ?? bytes.byteLength,
          snapshot_content_type: photo.media_type ?? "application/octet-stream",
        }),
      });
      const attachData = await attach.json().catch(() => null);
      if (!attach.ok) {
        setNotice(attachData?.error?.message ?? "上传成功,但挂接工程失败");
        return;
      }
      const created = attachData?.input?.id as string | undefined;
      if (created) setInputId(created);
      setFile(null);
      setNotice(upData?.idempotent ? "同内容已存在,已恢复原挂接" : "上传完成,请选择保真模式");
      onInputsChanged?.();
    } finally {
      setBusy(false);
    }
  }

  async function openQuote() {
    const data = await send(`/api/projects/${projectId}/background-replacements`, "POST", {
      input_id: inputId,
      mode,
      protected_region: PROTECTION_SCOPE,
      background_intent: intent,
      explicit_creative: mode === "creative",
    });
    if (data?.job) setActive(data.job as Job);
  }

  async function act(action: string, body?: unknown) {
    if (!active) return;
    const data = await send(
      `/api/projects/${projectId}/background-replacements/${active.id}/${action}`,
      "POST",
      body
    );
    if (data?.job) setActive(data.job as Job);
  }

  function fileBase64(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const text = String(reader.result ?? "");
        const comma = text.indexOf(",");
        resolve(comma >= 0 ? text.slice(comma + 1) : text);
      };
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });
  }

  async function lockSubject() {
    if (!active || !plateFile || !maskFile) return;
    const plate = await fileBase64(plateFile);
    const mask = await fileBase64(maskFile);
    const data = await send(
      `/api/projects/${projectId}/background-replacements/${active.id}/lock`,
      "POST",
      { plate_png_base64: plate, mask_png_base64: mask }
    );
    if (data?.job) setActive(data.job as Job);
  }

  async function modelPlate() {
    if (!active || !maskFile) return;
    const mask = await fileBase64(maskFile);
    const data = await send(
      `/api/projects/${projectId}/background-replacements/${active.id}/model-plate`,
      "POST",
      { mask_png_base64: mask }
    );
    if (data?.job) setActive(data.job as Job);
  }

  async function inspect() {
    const body: Record<string, unknown> = { ...axes, similarity_only: similarityOnly };
    if (similarity.trim() !== "") {
      const score = Number(similarity);
      if (Number.isFinite(score)) body.similarity = score;
    }
    await act("inspect", body);
  }

  function restore(job: Job) {
    setActive(job);
    setInputId(job.input_id);
    setIntent(job.background_intent || intent);
    if (job.mode === "creative" && creativeOn) setMode("creative");
    else setMode("fidelity");
    setNotice("已恢复原任务,未重新生成");
  }

  return (
    <Card data-testid="bg-replace-panel">
      <CardHeader>
        <CardTitle>AI 背景替换</CardTitle>
        <CardDescription>
          上传后默认保真,只改背景。Logo、包装文字、规格和结构不能被背景生成改写。{caps?.honesty ?? HONESTY}
        </CardDescription>
      </CardHeader>
      <p className="muted">上传 → 确认报价 → 生成，或用背景图和蒙版锁定主体。也可以只上传蒙版，让模型生成背景后再锁回主体像素。</p>
      <p>
        <Badge variant="warn">{bill || BILLING_PENDING}</Badge>{" "}
        <Badge>质量 {active?.quality ?? "unknown"}</Badge>{" "}
        <Badge variant="warn">{realNotice || REAL_GENERATION_INCOMPLETE}</Badge>
      </p>
      <div className="row" data-testid="bg-upload">
        <div>
          <label>上传商品照片</label>
          <input
            type="file"
            accept="image/jpeg,image/png,image/webp"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
        </div>
        <div style={{ flex: 0 }}>
          <Button type="button" disabled={busy || !file} onClick={() => void upload()}>
            上传并挂接
          </Button>
        </div>
      </div>
      <div className="row">
        <div>
          <label>商品输入</label>
          <select value={inputId} onChange={(e) => setInputId(e.target.value)}>
            {inputs.length === 0 ? <option value="">先上传或挂接商品照片</option> : null}
            {inputs.map((item) => (
              <option key={item.id} value={item.id}>
                {item.snapshot_name || item.id}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label>背景方向</label>
          <input value={intent} onChange={(e) => setIntent(e.target.value)} />
        </div>
      </div>
      <div className="row" style={{ marginTop: 8 }} data-testid="bg-mode">
        <label>
          <input type="radio" name="bg-mode" checked={mode === "fidelity"} onChange={() => setMode("fidelity")} /> 保真
        </label>
        {creativeOn ? (
          <label>
            <input type="radio" name="bg-mode" checked={mode === "creative"} onChange={() => setMode("creative")} /> 创意
          </label>
        ) : null}
      </div>
      <p className="muted">主体保护范围 {PROTECTION_SCOPE}。模式、保护范围和输入版本进入任务指纹。</p>
      {stale ? <p className="banner warn">模式、输入或背景方向已变,原报价失效,请重新生成报价。</p> : null}
      {notice ? <p className="banner">{notice}</p> : null}
      <div className="row" style={{ marginTop: 12 }}>
        <Button type="button" disabled={busy || !inputId} onClick={() => void openQuote()}>
          生成报价
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={busy || !active || stale || active.quote_status !== "unconfirmed"}
          onClick={() => void act("confirm", formBody)}
        >
          确认报价
        </Button>
        <Button
          type="button"
          variant="secondary"
          disabled={busy || !active || stale || active.job_status !== "quoted" || active.quote_status !== "confirmed"}
          onClick={() => void act("submit", formBody)}
        >
          生成背景
        </Button>
        {mode === "fidelity" ? (
          <>
            <label>
              背景图 PNG
              <input
                data-testid="bg-plate"
                type="file"
                accept="image/png"
                onChange={(e) => setPlateFile(e.target.files?.[0] ?? null)}
              />
            </label>
            <label>
              主体蒙版 PNG
              <input
                data-testid="bg-mask"
                type="file"
                accept="image/png"
                onChange={(e) => setMaskFile(e.target.files?.[0] ?? null)}
              />
            </label>
            <Button
              type="button"
              variant="secondary"
              disabled={
                busy ||
                !active ||
                stale ||
                active.quote_status !== "confirmed" ||
                (active.job_status !== "quoted" && active.job_status !== "generation_unavailable") ||
                !plateFile ||
                !maskFile
              }
              onClick={() => void lockSubject()}
            >
              锁定主体并换背景
            </Button>
            <Button
              type="button"
              variant="secondary"
              data-testid="bg-model-plate"
              disabled={
                busy ||
                !active ||
                stale ||
                active.quote_status !== "confirmed" ||
                (active.job_status !== "quoted" && active.job_status !== "generation_unavailable") ||
                !maskFile
              }
              onClick={() => void modelPlate()}
            >
              用模型生成背景并锁定主体
            </Button>
          </>
        ) : null}
      </div>
      {active ? (
        <div style={{ marginTop: 16 }}>
          <p className="muted">
            输入版本 {active.input_version} · 模式 {modeLabel[active.mode] ?? active.mode} · 保护范围{" "}
            {active.protected_region} · 状态 {active.job_status}
          </p>
          <p>
            模型 {active.model_ref || "未记录"} · 任务 {active.platform_task_id || "未记录"} · 资产{" "}
            {active.output_asset_id || "未记录"} {active.output_version || ""} · 费用 {active.fee_ref || active.billing_label || BILLING_PENDING}
          </p>
          <p>
            对照:原输入 {active.input_id} / 输出 {active.output_asset_id || "尚无输出"}。允许用途:
            {(active.allowed_uses ?? []).join("、") || "预览"}。
          </p>
          <p>待确认:{(active.pending ?? []).join("；") || "无"}</p>
          <p className="muted">{active.honesty || HONESTY}</p>
          <p className="muted">{realNotice || REAL_GENERATION_INCOMPLETE}</p>
          {active && subjectLockPreview(active.origin) ? (
            <div data-testid="bg-lock-preview">
              <img
                alt="主体锁定后的预览"
                src={`/api/projects/${projectId}/background-replacements/${active.id}/content`}
              />
              <p>主体像素已锁回。这不是模型出图。{REAL_GENERATION_INCOMPLETE}。不能作为可用候选。</p>
            </div>
          ) : null}
          {active && modelPlatePreview(active.origin) ? (
            <div data-testid="bg-model-plate-preview">
              <img
                alt="模型背景锁定后的预览"
                src={`/api/projects/${projectId}/background-replacements/${active.id}/content`}
              />
              <p>模型只提供了背景。主体像素与原图一致。{REAL_GENERATION_INCOMPLETE}。计费未通过。</p>
            </div>
          ) : null}
          <div className="row" data-testid="bg-inspect">
            {(Object.keys(axisLabel) as AxisKey[]).map((key) => (
              <div key={key}>
                <label>{axisLabel[key]}</label>
                <select
                  value={axes[key]}
                  onChange={(e) => setAxes({ ...axes, [key]: e.target.value })}
                >
                  <option value="unknown">未检查</option>
                  <option value="pass">未见改写</option>
                  <option value="fail">已改写</option>
                </select>
              </div>
            ))}
          </div>
          <div className="row">
            <div>
              <label>相似度分(不能单独通过)</label>
              <input value={similarity} onChange={(e) => setSimilarity(e.target.value)} inputMode="decimal" />
            </div>
            <label>
              <input type="checkbox" checked={similarityOnly} onChange={(e) => setSimilarityOnly(e.target.checked)} />{" "}
              只提供相似度
            </label>
          </div>
          <div className="row">
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void inspect()}>
              检查商品
            </Button>
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void act("select")}>
              选定此候选
            </Button>
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void act("export")}>
              导出同一版本
            </Button>
            <Button type="button" size="sm" variant="secondary" disabled={busy} onClick={() => void act("refresh")}>
              刷新并恢复原任务
            </Button>
          </div>
          {active.export_count ? <p className="muted">已导出 {active.export_count} 次,每次都是同一资产版本。</p> : null}
        </div>
      ) : null}
      {samples.length > 0 ? (
        <table style={{ marginTop: 16 }}>
          <thead>
            <tr>
              <th>样本</th>
              <th>质量</th>
              <th>模型用量</th>
              <th>费用</th>
            </tr>
          </thead>
          <tbody>
            {samples.map((sample) => (
              <tr key={sample.kind}>
                <td>{sample.label}</td>
                <td>{sample.quality}</td>
                <td>{sample.model_usage}</td>
                <td>{sample.cost_label}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {jobs.length > 0 ? (
        <div style={{ marginTop: 12 }}>
          <p className="muted">本工程已有 {jobs.length} 条背景替换记录。点恢复不会重新生成。</p>
          <div className="row">
            {jobs.map((job) => (
              <Button key={job.id} type="button" size="sm" variant="outline" disabled={busy} onClick={() => restore(job)}>
                恢复 {modeLabel[job.mode] ?? job.mode} {job.job_status}
              </Button>
            ))}
          </div>
        </div>
      ) : null}
      <p className="muted">生产出图与计费均未标记为通过。{REAL_GENERATION_INCOMPLETE}</p>
    </Card>
  );
}
