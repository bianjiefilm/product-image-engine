"use client";

import Link from "next/link";
import { useState } from "react";

function publicMessage(raw: string): string {
  if (!raw) return "没有核对成。不会重新做图，也不会再扣一次。";
  if (/provider|qwen|task_id|charge_ref|idempotency|debug|sha256/i.test(raw)) {
    return "原记录还对不上。不会重新做图，也不会再扣一次。";
  }
  return raw;
}

export function RecoveryStrip({ projectId }: { projectId?: string }) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);

  async function recover() {
    if (!projectId) return;
    setBusy(true);
    setNote("");
    try {
      const res = await fetch("/api/billing/consume/recover", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ project_id: projectId, status: "unknown" }),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setNote(publicMessage(data?.error?.message ?? "没有核对成。不会重新做图，也不会再扣一次。"));
        return;
      }
      if (data?.resubmit === true || data?.recharge === true || data?.regenerate === true) {
        setNote("服务端要求另看原记录。本页不会自动重做，也不会再扣一次。");
        return;
      }
      setNote("已按原记录核对。没有重新做图，也没有再扣费。结算仍待确认。");
    } catch {
      setNote("网络中断。不会重新做图，也不会再扣一次。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section id="recovery" className="card" aria-label="出错与恢复">
      <h2>出错了就停在这里</h2>
      <p className="muted">没有单独的恢复向导。失败时核对原费用，或回到已经有的页面。不会自动重做，也不会再扣一次。</p>
      <div className="media-actions">
        <button type="button" className="primary" disabled={busy || !projectId} onClick={() => void recover()}>核对原费用</button>
        <Link className="link" href="/start">回到开始做产品图</Link>
        <Link className="link" href="/projects">工程列表</Link>
        <Link className="link" href="/batches">批量列表</Link>
      </div>
      {!projectId ? <p className="muted">还没有工程时，不能核对费用。</p> : null}
      {note ? <p role="alert">{note}</p> : null}
    </section>
  );
}
