"use client";

import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  BILLING_PENDING,
  HONESTY,
  billingLine,
  creativeChoice,
  quoteStale,
  subjectProtectionLabel,
} from "@/lib/light-scene";

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
}

interface Job {
  id: string;
  input_id: string;
  input_version: string;
  mode: string;
  protected_region: string;
  lighting_intent: string;
  job_status: string;
  quality: string;
  quote_status: string;
  billing_label: string;
  selection: string;
  pending: string[];
  allowed_uses: string[];
  output_asset_id: string;
  output_version: string;
  verified_product: boolean;
  honesty: string;
}

interface Sample {
  kind: string;
  label: string;
  quality: string;
  model_usage: string;
  cost_label: string;
}

const modeLabel: Record<string, string> = { fidelity: "保真", creative: "创意" };
const statusLabel: Record<string, string> = {
  quoted: "待确认报价",
  submitting: "提交中",
  queued: "已排队",
  running: "生成中",
  unknown: "待核对",
  failed: "失败",
};

export function LightScenePanel({ projectId, inputs }: { projectId: string; inputs: InputOpt[] }) {
  const [hidden, setHidden] = useState(false);
  const [caps, setCaps] = useState<Caps | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [samples, setSamples] = useState<Sample[]>([]);
  const [inputId, setInputId] = useState("");
  const [mode, setMode] = useState("fidelity");
  const [intent, setIntent] = useState("侧光棚拍");
  const [active, setActive] = useState<Job | null>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let gone = false;
    (async () => {
      const res = await fetch("/api/light-scenes/capabilities");
      if (res.status === 404) {
        if (!gone) setHidden(true);
        return;
      }
      const data = await res.json().catch(() => null);
      if (!gone && res.ok) setCaps(data);
      const acc = await fetch("/api/light-scenes/acceptance");
      if (acc.ok) {
        const body = await acc.json().catch(() => null);
        if (!gone) setSamples(body?.samples ?? []);
      }
      const list = await fetch(`/api/projects/${projectId}/light-scenes`);
      if (list.ok) {
        const body = await list.json().catch(() => null);
        if (!gone) setJobs(body?.jobs ?? []);
      }
    })();
    return () => {
      gone = true;
    };
  }, [projectId]);

  useEffect(() => {
    if (!inputId && inputs[0]) setInputId(inputs[0].id);
  }, [inputId, inputs]);

  if (hidden) return null;
  const creative = creativeChoice(Boolean(caps?.creative_available));
  const bill = billingLine(caps?.billing_label ?? active?.billing_label);
  const quoted = active
    ? { mode: active.mode, inputId: active.input_id, intent: active.lighting_intent }
    : null;
  const stale = quoted ? quoteStale(quoted, { mode, inputId, intent }) : false;
  const formBody = { mode, input_id: inputId, lighting_intent: intent };

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

  async function openQuote() {
    const data = await send(`/api/projects/${projectId}/light-scenes`, "POST", {
      input_id: inputId,
      mode,
      lighting_intent: intent,
      explicit_creative: mode === "creative",
    });
    if (data?.job) setActive(data.job as Job);
  }

  async function act(action: string, body?: unknown) {
    if (!active) return;
    const data = await send(
      `/api/projects/${projectId}/light-scenes/${active.id}/${action}`,
      "POST",
      body
    );
    if (data?.job) setActive(data.job as Job);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>光影场景</CardTitle>
        <CardDescription>
          默认保真模式,只改光影和氛围,不能绕过主体保护。{caps?.honesty ?? HONESTY}
        </CardDescription>
      </CardHeader>
      <p>
        <Badge variant="warn">{bill || BILLING_PENDING}</Badge>{" "}
        <Badge>质量 {active?.quality ?? "unknown"}</Badge>{" "}
        <Badge>{subjectProtectionLabel(active?.quality)}</Badge>
      </p>
      <div className="row">
        <div>
          <label>商品输入</label>
          <select value={inputId} onChange={(e) => setInputId(e.target.value)}>
            {inputs.length === 0 ? <option value="">先挂接商品照片</option> : null}
            {inputs.map((item) => (
              <option key={item.id} value={item.id}>
                {item.snapshot_name || item.id}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label>光影意图</label>
          <input value={intent} onChange={(e) => setIntent(e.target.value)} />
        </div>
      </div>
      <div className="row" style={{ marginTop: 8 }}>
        <label>
          <input type="radio" checked={mode === "fidelity"} onChange={() => setMode("fidelity")} /> 保真
        </label>
        <label title={creative.reason}>
          <input
            type="radio"
            checked={mode === "creative"}
            disabled={!creative.enabled}
            onChange={() => setMode("creative")}
          />{" "}
          创意{creative.enabled ? "" : "（未接通）"}
        </label>
      </div>
      {stale ? <p className="banner warn">模式、输入或光影意图已变,原报价失效,请重新生成报价。</p> : null}
      {notice ? <p className="banner">{notice}</p> : null}
      <div className="row" style={{ marginTop: 12 }}>
        <Button type="button" disabled={busy || !inputId} onClick={openQuote}>
          生成报价
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={busy || !active || stale || active.quote_status !== "unconfirmed"}
          onClick={() => act("confirm", formBody)}
        >
          确认报价
        </Button>
        <Button
          type="button"
          variant="secondary"
          disabled={busy || !active || stale || active.job_status !== "quoted" || active.quote_status !== "confirmed"}
          onClick={() => act("submit", formBody)}
        >
          提交光影场景
        </Button>
      </div>
      {active ? (
        <div style={{ marginTop: 16 }}>
          <p className="muted">
            输入版本 {active.input_version} · 模式 {modeLabel[active.mode] ?? active.mode} · 保护区域{" "}
            {active.protected_region} · 状态 {statusLabel[active.job_status] ?? active.job_status}
          </p>
          <p>
            对照:原输入 {active.input_id} / 输出 {active.output_asset_id || "尚无输出"}{" "}
            {active.output_version || ""}
          </p>
          <p>
            主体保护:{subjectProtectionLabel(active.quality)}。允许用途:
            {(active.allowed_uses ?? []).join("、") || "预览"}。
          </p>
          <p>待确认:{(active.pending ?? []).join("；") || "无"}</p>
          <p className="muted">{active.honesty || HONESTY}</p>
          <div className="row">
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => act("select")}>
              选定此候选
            </Button>
            <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => act("export")}>
              导出同一版本
            </Button>
            <Button type="button" size="sm" variant="secondary" disabled={busy} onClick={() => act("refresh")}>
              核对任务
            </Button>
          </div>
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
      {jobs.length > 0 ? <p className="muted">本工程已有 {jobs.length} 条光影场景记录。</p> : null}
      <p className="muted">生产出图与计费均未标记为通过。顶栏不代表光影生成已通过。</p>
    </Card>
  );
}
