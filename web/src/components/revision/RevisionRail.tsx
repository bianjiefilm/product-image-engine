"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { displayCostCents } from "@/lib/revision";

type VersionView = {
  label?: string;
  outcome?: string;
  action?: string;
  region?: string;
  incremental_cost_cents?: number;
  brief_version?: string;
};

type PlanView = {
  requires_acknowledgement?: boolean;
  incremental_cost_cents?: number;
  calls_supplier?: boolean;
};

const CLICKS = [
  { click: "simplify_background", label: "背景简单一点" },
  { click: "outdoor", label: "换成户外" },
  { click: "brighten_keep_product", label: "保留商品，只调亮" },
] as const;

const ACTION_LABEL: Record<string, string> = {
  simplify_background: "背景简单一点",
  outdoor: "换成户外",
  brighten_keep_product: "保留商品，只调亮",
  rollback: "回到这一版",
  adopt: "采用",
  send_downstream: "送到下游",
};

const REGION_LABEL: Record<string, string> = {
  background: "背景",
  light: "光影",
  none: "不改画面",
};

function publicMessage(raw: string): string {
  if (!raw) return "没有完成。可以稍后再试，不会自动重做。";
  if (/provider|qwen|preserve_|partialedit|sha256|supplier|供应商|modelxing|debug|subject_lock/i.test(raw)) {
    return "这一步没有完成。商品会保持原样。本页不会整张重画，也不会另编一笔费用。";
  }
  return raw;
}

function costLabel(cents: unknown): string {
  if (typeof cents !== "number") return "费用待确认";
  try {
    const n = displayCostCents(cents);
    return `增加 ¥${Math.floor(n / 100)}.${String(n % 100).padStart(2, "0")}`;
  } catch {
    return "费用待确认";
  }
}

function fileToB64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const raw = String(reader.result || "");
      const comma = raw.indexOf(",");
      resolve(comma >= 0 ? raw.slice(comma + 1) : raw);
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

export function RevisionRail({ projectId }: { projectId?: string }) {
  const [closed, setClosed] = useState<boolean | null>(null);
  const [versions, setVersions] = useState<VersionView[]>([]);
  const [adopted, setAdopted] = useState("");
  const [notice, setNotice] = useState("");
  const [cost, setCost] = useState<number | null>(null);
  const [needsAck, setNeedsAck] = useState(false);
  const [ack, setAck] = useState(false);
  const [click, setClick] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [original, setOriginal] = useState<File | null>(null);
  const [mask, setMask] = useState<File | null>(null);
  const [left, setLeft] = useState("");
  const [right, setRight] = useState("");
  const [compared, setCompared] = useState(false);
  const [downNote, setDownNote] = useState("");

  const applyState = useCallback((data: { versions?: VersionView[]; adopted_label?: string } | null) => {
    const list = Array.isArray(data?.versions) ? data.versions : [];
    setVersions(list);
    setAdopted(typeof data?.adopted_label === "string" ? data.adopted_label : "");
    setLeft((cur) => cur || list[0]?.label || "");
    setRight((cur) => cur || list[1]?.label || list[0]?.label || "");
  }, []);

  const load = useCallback(async () => {
    if (!projectId) {
      setClosed(null);
      setVersions([]);
      return;
    }
    const res = await fetch(`/api/projects/${projectId}/revisions`);
    const data = await res.json().catch(() => null);
    if (res.status === 404) {
      setClosed(true);
      setVersions([]);
      return;
    }
    if (!res.ok) {
      setNotice(publicMessage(data?.error?.message ?? "版本没有读到"));
      setClosed(false);
      return;
    }
    setClosed(false);
    applyState(data);
  }, [applyState, projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function plan(body: { click?: string; text?: string }) {
    if (!projectId) return;
    setBusy(true);
    setNotice("");
    setDownNote("");
    setCost(null);
    try {
      const intent = await fetch(`/api/projects/${projectId}/revisions/intent`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const intentBody = await intent.json().catch(() => null);
      if (intent.status === 404) {
        setClosed(true);
        return;
      }
      if (!intent.ok) {
        setNotice(publicMessage(intentBody?.error?.message ?? "这句话还不能执行，不会整张重画。"));
        setClick("");
        return;
      }
      const planned = await fetch(`/api/projects/${projectId}/revisions/plan`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const planBody = (await planned.json().catch(() => null)) as { plan?: PlanView; error?: { message?: string }; incremental_cost_cents?: number } | null;
      if (!planned.ok) {
        setNotice(publicMessage(planBody?.error?.message ?? "还不能确认影响范围"));
        return;
      }
      const view = planBody?.plan;
      const cents = view?.incremental_cost_cents ?? intentBody?.incremental_cost_cents;
      setCost(typeof cents === "number" ? cents : null);
      setNeedsAck(view?.requires_acknowledgement === true);
      setAck(false);
      setNotice("商品、标志和包装文字会保持原样。确认前不会改图。现在不能把一小块交给外部去改。");
    } finally {
      setBusy(false);
    }
  }

  async function execute() {
    if (!projectId || !click || !original || !mask) return;
    if (needsAck && !ack) return;
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${projectId}/revisions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          click,
          acknowledged: true,
          brief_version: "current",
          original_b64: await fileToB64(original),
          mask_b64: await fileToB64(mask),
        }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setNotice(publicMessage(data?.error?.message ?? data?.impact_notice ?? "没有改成"));
        return;
      }
      setNotice("已留下新版本。商品主体按锁定范围保持。这不是外部重画。");
      setClick("");
      await load();
    } finally {
      setBusy(false);
    }
  }

  async function adopt(label: string) {
    if (!projectId) return;
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${projectId}/revisions/${encodeURIComponent(label)}/adopt`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setNotice(publicMessage(data?.error?.message ?? "没有采用"));
        return;
      }
      applyState(data);
      setNotice(`已采用 ${label}。之前的版本还在。`);
    } finally {
      setBusy(false);
    }
  }

  async function rollback(label: string) {
    if (!projectId) return;
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${projectId}/revisions/rollback`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text: `回到${label}` }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setNotice(publicMessage(data?.error?.message ?? "没有回到那一版"));
        return;
      }
      applyState(data);
      setNotice(`已回到 ${label}。`);
    } finally {
      setBusy(false);
    }
  }

  async function compare() {
    if (!projectId || !left || !right) return;
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${projectId}/revisions/compare?left=${encodeURIComponent(left)}&right=${encodeURIComponent(right)}`);
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setCompared(false);
        setNotice(publicMessage(data?.error?.message ?? "这两版还不能并排"));
        return;
      }
      setCompared(true);
    } finally {
      setBusy(false);
    }
  }

  async function downstream(label: string, target: string) {
    if (!projectId) return;
    setBusy(true);
    setDownNote("");
    try {
      const res = await fetch(`/api/projects/${projectId}/revisions/${encodeURIComponent(label)}/downstream`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ target }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setDownNote(publicMessage(data?.error?.message ?? "没有送出"));
        return;
      }
      const again = data?.downstream?.requires_reupload === true;
      setDownNote(again ? "这一版还不能直接沿用，需要重新准备文件。" : `只返回了 ${label} 的本地引用，没有上传文件。数字人、AiCut 和矩阵都还没收到。`);
    } finally {
      setBusy(false);
    }
  }

  const labels = versions.map((v) => v.label).filter((v): v is string => !!v);

  return (
    <section id="revision" className="card" aria-label="只改这里" data-revision={closed ? "closed" : projectId ? "open" : "empty"}>
      <h2>只改这里</h2>
      <p className="muted">锁定商品，只改背景或光影。已有版本可以比较、采用或回到上一版。</p>
      {!projectId ? (
        <div className="stage-empty" style={{ minHeight: 120 }}>
          <p>还没有工程，所以还不能改某一处。</p>
          <p><Link className="link" href="/start">去开始做产品图</Link></p>
        </div>
      ) : closed ? (
        <div className="stage-empty" style={{ minHeight: 140 }}>
          <p>「只改这里」在这个环境还没有打开。不会假装已经改好。</p>
          <p>
            <Link className="link" href="/start">开始做产品图</Link>
            {" · "}
            <Link className="link" href="/projects">已有工程</Link>
          </p>
        </div>
      ) : (
        <>
          <div className="media-actions">
            {CLICKS.map((item) => (
              <button
                key={item.click}
                type="button"
                className={click === item.click ? "primary" : ""}
                disabled={busy}
                onClick={() => {
                  setClick(item.click);
                  setText("");
                  void plan({ click: item.click });
                }}
              >
                {item.label}
              </button>
            ))}
          </div>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              setClick("");
              void plan({ text: text.trim() });
            }}
          >
            <label htmlFor="revision-text">或者用一句话</label>
            <input id="revision-text" value={text} onChange={(e) => setText(e.target.value)} placeholder="例如：背景简单一点" />
            <div className="media-actions">
              <button className="primary" type="submit" disabled={busy || !text.trim()}>看会影响哪里</button>
            </div>
          </form>
          {cost !== null ? <p>这一处的费用 {costLabel(cost)}。金额来自服务端，这里不加、不改。</p> : null}
          {click ? (
            <>
              <p>{notice || "确认前不会改图。"}</p>
              <label>商品原图（PNG）</label>
              <input type="file" accept="image/png" onChange={(e) => setOriginal(e.target.files?.[0] ?? null)} />
              <label>要锁住的商品范围（PNG）</label>
              <input type="file" accept="image/png" onChange={(e) => setMask(e.target.files?.[0] ?? null)} />
              <p className="muted">页面上还不能直接圈选。没有原图和要锁住的范围时不会执行，也不会用别的图冒充结果。</p>
              {needsAck ? (
                <label>
                  <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} /> 我知道商品会锁住，确认后才改这一处以外
                </label>
              ) : null}
              <div className="media-actions">
                <button className="primary" type="button" disabled={busy || !original || !mask || (needsAck && !ack)} onClick={() => void execute()}>
                  确认只改这里
                </button>
              </div>
            </>
          ) : notice ? <p role="status">{notice}</p> : null}
          <h3>版本</h3>
          {versions.length === 0 ? <p className="muted">还没有版本。做出结果后，采用状态会标在这里。</p> : (
            <ul className="asset-strip">
              {versions.map((v) => (
                <li key={v.label}>
                  <img alt={`${v.label} 的结果`} src={`/api/projects/${projectId}/revisions/${encodeURIComponent(v.label || "")}/content`} />
                  <strong>{v.label}</strong>
                  <div>{adopted === v.label ? <span className="fidelity-chip">已采用</span> : <span className="fidelity-chip wait">未采用</span>}</div>
                  <div className="muted">{ACTION_LABEL[v.action || ""] ?? "这一版"} · {REGION_LABEL[v.region || ""] ?? "画面"}</div>
                  <div className="muted">{costLabel(v.incremental_cost_cents)}</div>
                  <div className="media-actions">
                    <button type="button" disabled={busy || adopted === v.label} onClick={() => void adopt(v.label || "")}>采用这个版本</button>
                    <button type="button" disabled={busy || adopted === v.label} onClick={() => void rollback(v.label || "")}>回到 {v.label}</button>
                  </div>
                  <div className="media-actions">
                    <button type="button" disabled={busy} onClick={() => void downstream(v.label || "", "digital_human")}>送数字人</button>
                    <button type="button" disabled={busy} onClick={() => void downstream(v.label || "", "aicut")}>送 AiCut</button>
                    <button type="button" disabled={busy} onClick={() => void downstream(v.label || "", "matrix")}>送矩阵</button>
                  </div>
                </li>
              ))}
            </ul>
          )}
          {downNote ? <p role="status">{downNote}</p> : null}
          <div id="compare">
            <h3>并排比较</h3>
            {labels.length < 2 ? (
              <p className="muted">还没有两版可以并排看。第二版出现后，可以在这里对照，不会改已采用的图。</p>
            ) : (
              <>
                <div className="row">
                  <div>
                    <label htmlFor="compare-left">左</label>
                    <select id="compare-left" value={left} onChange={(e) => { setLeft(e.target.value); setCompared(false); }}>
                      {labels.map((label) => <option key={label} value={label}>{label}{label === adopted ? " · 已采用" : ""}</option>)}
                    </select>
                  </div>
                  <div>
                    <label htmlFor="compare-right">右</label>
                    <select id="compare-right" value={right} onChange={(e) => { setRight(e.target.value); setCompared(false); }}>
                      {labels.map((label) => <option key={label} value={label}>{label}{label === adopted ? " · 已采用" : ""}</option>)}
                    </select>
                  </div>
                </div>
                <div className="media-actions">
                  <button className="primary" type="button" disabled={busy || !left || !right || left === right} onClick={() => void compare()}>并排比较</button>
                </div>
                {compared ? (
                  <div className="compare-pair">
                    <figure>
                      <img alt={`${left}，左`} src={`/api/projects/${projectId}/revisions/${encodeURIComponent(left)}/content`} />
                      <figcaption>{left}{left === adopted ? " · 已采用" : " · 未采用"}</figcaption>
                    </figure>
                    <figure>
                      <img alt={`${right}，右`} src={`/api/projects/${projectId}/revisions/${encodeURIComponent(right)}/content`} />
                      <figcaption>{right}{right === adopted ? " · 已采用" : " · 未采用"}</figcaption>
                    </figure>
                  </div>
                ) : null}
              </>
            )}
          </div>
        </>
      )}
    </section>
  );
}
