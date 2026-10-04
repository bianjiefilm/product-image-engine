"use client";

import { useCallback, useEffect, useState } from "react";
import { dynamicAdLabel, isMotionDigest, NOT_FINISHED_COPY, type MotionDeclaration } from "@/lib/motion-ref";

type ListBody = { declarations?: MotionDeclaration[]; error?: { message?: string } };
type OneBody = { declaration?: MotionDeclaration; error?: { message?: string } };

export function MotionRefNote({ projectId }: { projectId?: string }) {
  const [closed, setClosed] = useState<boolean | null>(null);
  const [rows, setRows] = useState<MotionDeclaration[]>([]);
  const [revisionID, setRevisionID] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!projectId) {
      setClosed(null);
      setRows([]);
      return;
    }
    const res = await fetch(`/api/projects/${projectId}/motion-refs`);
    const data = (await res.json().catch(() => null)) as ListBody | null;
    if (res.status === 404) {
      setClosed(true);
      setRows([]);
      return;
    }
    if (!res.ok) {
      setNotice(data?.error?.message ?? "引用没有读到");
      setClosed(false);
      return;
    }
    setClosed(false);
    setRows(Array.isArray(data?.declarations) ? data.declarations : []);
  }, [projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function declare() {
    if (!projectId || !revisionID.trim()) return;
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${projectId}/motion-refs`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ revision_id: revisionID.trim() }),
      });
      const data = (await res.json().catch(() => null)) as OneBody | null;
      if (!res.ok || !data?.declaration) {
        setNotice(data?.error?.message ?? NOT_FINISHED_COPY);
        return;
      }
      if (!acceptDeclaration(data.declaration)) {
        setNotice("主体不能重做。还没生成成片。");
        return;
      }
      await load();
    } finally {
      setBusy(false);
    }
  }

  async function record(name: "price" | "color", value: string, id: string) {
    if (!projectId || !value.trim()) return;
    setBusy(true);
    setNotice("");
    try {
      const res = await fetch(`/api/projects/${projectId}/motion-refs/${encodeURIComponent(id)}/parameters`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, value: value.trim() }),
      });
      const data = (await res.json().catch(() => null)) as OneBody | null;
      if (!res.ok || !data?.declaration || !acceptDeclaration(data.declaration)) {
        setNotice(data?.error?.message ?? "只记录参数。还没生成成片。");
        return;
      }
      await load();
    } finally {
      setBusy(false);
    }
  }

  return (
    <section id="motion-ref" className="card" aria-label="Motion 引用">
      <h2>Motion 引用</h2>
      <p className="muted">{NOT_FINISHED_COPY}</p>
      <p className="muted">不生成视频，不调用供应商，这里也没有 Motion 编辑器。</p>
      <p className="muted">HUI-2732 未完成，不能证明其他变体的执行次数为 0，所以不批量重跑。</p>
      {!projectId ? <p>还没有工程。还没生成成片。</p> : null}
      {closed ? <p>这个环境还没有打开 Motion 引用。还没生成成片。</p> : null}
      {projectId && closed === false ? (
        <div>
          <label>
            已采用修订
            <input value={revisionID} onChange={(e) => setRevisionID(e.target.value)} />
          </label>
          <button type="button" disabled={busy} onClick={() => void declare()}>形成引用</button>
          {rows.map((row) => (
            <MotionRefRow key={row.id || row.revision_id} row={row} busy={busy} onRecord={record} />
          ))}
        </div>
      ) : null}
      {notice ? <p role="alert">{notice}</p> : null}
    </section>
  );
}

function MotionRefRow({ row, busy, onRecord }: { row: MotionDeclaration; busy: boolean; onRecord: (name: "price" | "color", value: string, id: string) => Promise<void> }) {
  const [price, setPrice] = useState("");
  const [color, setColor] = useState("");
  const copy = safeCopy(row);
  return (
    <article>
      <p>{copy}</p>
      <p>资产 {row.asset_id}</p>
      <p>内容哈希 {row.content_hash}</p>
      <p>修订 {row.revision_id}</p>
      <p>摘要 {row.digest}</p>
      <p>商品重生成 {row.product_regeneration ? "是" : "否"}，模型调用 {row.model_call_count ?? 0}</p>
      <label>
        价格
        <input value={price} onChange={(e) => setPrice(e.target.value)} />
      </label>
      <label>
        颜色
        <input value={color} onChange={(e) => setColor(e.target.value)} />
      </label>
      <button type="button" disabled={busy || !row.id} onClick={() => void onRecord("price", price, row.id || "")}>只记录价格</button>
      <button type="button" disabled={busy || !row.id} onClick={() => void onRecord("color", color, row.id || "")}>只记录颜色</button>
    </article>
  );
}

function acceptDeclaration(row: MotionDeclaration): boolean {
  if (row.product_regeneration !== false || row.model_call_count !== 0) return false;
  if (row.subject_hash !== row.content_hash) return false;
  if (!row.digest || !isMotionDigest(row.digest)) return false;
  try {
    dynamicAdLabel(row);
  } catch {
    return false;
  }
  return true;
}

function safeCopy(row: MotionDeclaration): string {
  try {
    return dynamicAdLabel(row);
  } catch {
    return NOT_FINISHED_COPY;
  }
}
