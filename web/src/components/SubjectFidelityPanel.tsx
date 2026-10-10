"use client";

import { useCallback, useEffect, useState } from "react";

const CHECKS = [
  ["product_count", "商品数量"],
  ["logo_text", "包装文字 / Logo"],
  ["shape", "形状 / 结构"],
  ["key_texture", "关键纹理"],
  ["declared_color", "声明颜色"],
] as const;

const SAMPLES = [
  ["logo_text", "包装文字 / Logo"],
  ["transparent_reflective", "透明或反光"],
  ["fine_edge", "细小边缘"],
  ["occlusion", "遮挡"],
  ["small_product", "小商品"],
  ["similar_sku", "相近 SKU"],
] as const;

interface FidelityReport {
  id: string;
  mode: string;
  verdict: string;
  exact_product: boolean;
  deliverable: boolean;
  real_generation: string;
  reasons?: string[];
  limits?: string[];
  sample_class: string;
  input_ref: string;
  input_version: string;
  output_ref: string;
  holding_wear_verified: boolean;
}

function verdictLabel(verdict: string): string {
  if (verdict === "fail") return "未通过";
  if (verdict === "pass") return "保真通过";
  return "待确认";
}

function verdictClass(verdict: string): string {
  if (verdict === "fail") return "pill bad";
  if (verdict === "pass") return "pill";
  return "pill wait";
}

export function SubjectFidelityPanel({ projectId }: { projectId: string }) {
  const [reports, setReports] = useState<FidelityReport[] | null>(null);
  const [closed, setClosed] = useState(false);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [mode, setMode] = useState("fidelity");
  const [sampleClass, setSampleClass] = useState("logo_text");
  const [inputRef, setInputRef] = useState("");
  const [inputVersion, setInputVersion] = useState("");
  const [outputRef, setOutputRef] = useState("");
  const [maskRef, setMaskRef] = useState("");
  const [subjectEdit, setSubjectEdit] = useState(false);
  const [subjectEditConfirmed, setSubjectEditConfirmed] = useState(false);
  const [checks, setChecks] = useState<Record<string, string>>({
    product_count: "unknown",
    logo_text: "unknown",
    shape: "unknown",
    key_texture: "unknown",
    declared_color: "unknown",
  });

  const load = useCallback(async () => {
    const res = await fetch(`/api/projects/${projectId}/fidelity-reports`);
    if (res.status === 404) {
      setClosed(true);
      setReports([]);
      return;
    }
    const data = await res.json().catch(() => null);
    if (!res.ok) {
      setNotice(data?.error?.message ?? "主体保真记录读取失败");
      setReports([]);
      return;
    }
    setClosed(false);
    setReports((data?.reports ?? []) as FidelityReport[]);
  }, [projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setNotice("");
    const res = await fetch(`/api/projects/${projectId}/fidelity-reports`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        mode,
        sample_class: sampleClass,
        input_ref: inputRef,
        input_version: inputVersion,
        output_ref: outputRef,
        mask_ref: maskRef,
        subject_edit: subjectEdit,
        subject_edit_confirmed: subjectEditConfirmed,
        machine_scope: "边缘与文字像素差",
        human_scope: "包装文字与 Logo 目视",
        protected: CHECKS.map(([name]) => name),
        allowed_regions: ["background"],
        checks: CHECKS.map(([name]) => ({ name, result: checks[name] })),
      }),
    });
    const data = await res.json().catch(() => null);
    setBusy(false);
    if (!res.ok) {
      setNotice(data?.error?.message ?? "记录失败");
      return;
    }
    const verdict = data?.report?.verdict as string | undefined;
    setNotice(
      verdict === "fail"
        ? "已留下未通过候选，不会当作可用商品图。"
        : "已记录。真实生成保真仍为待确认，没有扣费。"
    );
    await load();
  }

  return (
    <div className="card" data-testid="subject-fidelity">
      <h2>主体保真</h2>
      <div className="banner warn">
        真实生成保真待确认。这里只记录保护范围和检查，不调用生成模型，也不扣费。
        计费没连通时，费用仍显示「计费待确认」。顶栏完成不代表主体保真或背景生成已经通过。
      </div>
      <p className="muted">
        未通过的结果留在候选里，不能改标签变成已通过。创意结果不是精确商品图。
        持物或穿戴不在这项里验证。输出 ID 填了之后，未通过或待确认的结果不能回执成已验证商品图。
      </p>
      {closed ? (
        <p className="muted" style={{ marginBottom: 0 }}>
          主体保真记录未开启。真实生成保真仍为待确认。
        </p>
      ) : (
        <>
          {reports && reports.length > 0 ? (
            <div className="media-actions" aria-label="主体保真结论">
              {reports.map((r) => (
                <span key={r.id} className={r.verdict === "fail" ? "fidelity-chip bad" : r.verdict === "pass" ? "fidelity-chip" : "fidelity-chip wait"}>
                  主体保真 {verdictLabel(r.verdict)} · {r.input_version || "未标版本"}
                  {r.exact_product ? "" : " · 不能当作已验证商品图"}
                </span>
              ))}
            </div>
          ) : (
            <p className="muted">还没有主体保真记录。结果上的商品检查仍然单独显示。</p>
          )}
          <form onSubmit={onSubmit} style={{ marginTop: 12 }}>
            <div className="row">
              <div>
                <label>模式</label>
                <select value={mode} onChange={(e) => setMode(e.target.value)}>
                  <option value="fidelity">保真（默认，只改已确认区域）</option>
                  <option value="creative">创意（不是精确商品图）</option>
                </select>
              </div>
              <div>
                <label>样本类</label>
                <select
                  value={sampleClass}
                  onChange={(e) => setSampleClass(e.target.value)}
                >
                  {SAMPLES.map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <div className="row">
              <div>
                <label>原图引用</label>
                <input value={inputRef} onChange={(e) => setInputRef(e.target.value)} />
              </div>
              <div>
                <label>输入版本</label>
                <input
                  value={inputVersion}
                  onChange={(e) => setInputVersion(e.target.value)}
                />
              </div>
              <div>
                <label>主体 mask</label>
                <input value={maskRef} onChange={(e) => setMaskRef(e.target.value)} />
              </div>
              <div>
                <label>输出 ID（可空，用于拦住未验证回执）</label>
                <input value={outputRef} onChange={(e) => setOutputRef(e.target.value)} />
              </div>
            </div>
            {CHECKS.map(([name, label]) => (
              <div className="row" key={name}>
                <div>
                  <label>{label}</label>
                  <select
                    value={checks[name]}
                    onChange={(e) =>
                      setChecks({ ...checks, [name]: e.target.value })
                    }
                  >
                    <option value="unknown">待确认</option>
                    <option value="pass">未发现漂移</option>
                    <option value="fail">有漂移</option>
                  </select>
                </div>
              </div>
            ))}
            <div className="row">
              <div>
                <label>
                  <input
                    type="checkbox"
                    checked={subjectEdit}
                    onChange={(e) => setSubjectEdit(e.target.checked)}
                  />{" "}
                  这次改到了商品主体
                </label>
              </div>
              <div>
                <label>
                  <input
                    type="checkbox"
                    checked={subjectEditConfirmed}
                    onChange={(e) => setSubjectEditConfirmed(e.target.checked)}
                  />{" "}
                  主体修改已经单独确认
                </label>
              </div>
            </div>
            <button className="primary" type="submit" disabled={busy}>
              {busy ? "记录中…" : "记录主体保护"}
            </button>
          </form>
          <details data-technical="true">
            <summary>记录检查（不生成、不扣费）</summary>
          {reports && reports.length > 0 ? (
            <table>
              <thead>
                <tr>
                  <th>结论</th>
                  <th>模式</th>
                  <th>样本</th>
                  <th>输入版本</th>
                  <th>限制</th>
                </tr>
              </thead>
              <tbody>
                {reports.map((r) => (
                  <tr key={r.id}>
                    <td>
                      <span className={verdictClass(r.verdict)}>{verdictLabel(r.verdict)}</span>
                    </td>
                    <td>{r.mode === "creative" ? "创意" : "保真"}</td>
                    <td>{r.sample_class}</td>
                    <td>
                      {r.input_ref} · {r.input_version}
                    </td>
                    <td className="muted">
                      {(r.reasons ?? []).concat(r.limits ?? []).join("；") || "—"}
                      {r.exact_product ? "" : " 不能当作已验证商品图。"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p className="muted">还没有主体保真记录。</p>
          )}
          </details>
        </>
      )}
      {notice ? <p className="muted">{notice}</p> : null}
    </div>
  );
}
