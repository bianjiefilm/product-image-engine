"use client";

import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { BILLING_PENDING, canStartWithText, presentTextJob, sameRecord } from "@/lib/text-image";

type Job = {
  id: string;
  prompt?: string;
  job_status?: string;
  status_label?: string;
  quote_status?: string;
  billing_label?: string;
  show_image?: boolean;
  honesty?: string;
  pending?: string[];
};

export function TextImagePanel({ projectId }: { projectId?: string }) {
  const [prompt, setPrompt] = useState("");
  const [job, setJob] = useState<Job | null>(null);
  const [previousId, setPreviousId] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [boundProject, setBoundProject] = useState(projectId ?? "");

  useEffect(() => {
    setBoundProject(projectId ?? "");
  }, [projectId]);

  useEffect(() => {
    if (!boundProject) return;
    let gone = false;
    (async () => {
      const list = await fetch(`/api/projects/${boundProject}/text-images`);
      if (!list.ok) return;
      const body = (await list.json().catch(() => null)) as { jobs?: Job[] } | null;
      const current = body?.jobs?.[0];
      if (!gone && current?.id) remember(current);
    })();
    return () => {
      gone = true;
    };
  }, [boundProject]);

  function remember(next: Job) {
    setJob((prev) => {
      if (prev?.id && prev.id !== next.id) setPreviousId(prev.id);
      return next;
    });
  }

  const view = presentTextJob(job ?? {});
  const same = job?.id ? (previousId ? sameRecord(job.id, previousId) : true) : true;

  async function send(path: string, body?: unknown) {
    const res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    const data = (await res.json().catch(() => null)) as { job?: Job; project?: { id?: string }; error?: { message?: string } } | null;
    if (!res.ok) {
      setNotice(data?.error?.message ?? "请求失败");
      return null;
    }
    return data;
  }

  async function begin() {
    if (!canStartWithText(prompt)) return;
    setBusy(true);
    setNotice("");
    try {
      let id = boundProject;
      if (!id) {
        const started = await send("/api/entry/start", {
          name: "文字描述产品图",
          usage_kind: "文生图",
          width_px: 800,
          height_px: 800,
          background_direction: "",
          photo_asset_id: "",
        });
        id = started?.project?.id ?? "";
        if (!id) return;
        setBoundProject(id);
      }
      const created = await send(`/api/projects/${id}/text-images`, { prompt: prompt.trim() });
      const opened = created?.job;
      if (!opened?.id) return;
      remember(opened);
      if (opened.job_status === "quoted" && opened.quote_status !== "confirmed") {
        const confirmed = await send(`/api/projects/${id}/text-images/${opened.id}/confirm`);
        if (confirmed?.job) remember(confirmed.job);
      }
      if ((opened.job_status ?? "quoted") === "quoted") {
        const submitted = await send(`/api/projects/${id}/text-images/${opened.id}/submit`);
        if (submitted?.job) remember(submitted.job);
      }
    } finally {
      setBusy(false);
    }
  }

  async function refresh() {
    if (!boundProject || !job?.id) return;
    setBusy(true);
    setNotice("");
    try {
      const data = await send(`/api/projects/${boundProject}/text-images/${job.id}/refresh`);
      if (data?.job) remember(data.job);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card data-text-entry="true" data-show-image="false" data-photo-required="false" data-billing-passed="false" data-production-authorized="false">
      <CardHeader>
        <CardTitle>只用文字描述</CardTitle>
        <CardDescription>
          不上传实物照片也可以开始。没有真实生成供应商时，这里只记录失败或待确认，不会显示产品图。
        </CardDescription>
      </CardHeader>
      <p>
        <Badge variant="warn">{view.billingLabel || BILLING_PENDING}</Badge>{" "}
        <Badge>主体保护待确认</Badge>{" "}
        <Badge>{job?.status_label || (job?.job_status === "failed" ? "失败" : "待确认")}</Badge>
      </p>
      <label>
        文字描述
        <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} rows={3} placeholder="例如：白色陶瓷杯，浅灰棚拍" />
      </label>
      <div className="row" style={{ marginTop: 12 }}>
        <Button type="button" disabled={busy || !canStartWithText(prompt)} onClick={() => void begin()}>
          {busy ? "处理中…" : "只用文字描述开始"}
        </Button>
        <Button type="button" variant="outline" disabled={busy || !job?.id} onClick={() => void refresh()}>
          刷新同一条记录
        </Button>
      </div>
      <p>{view.headline}</p>
      <p>生成完成不等于已结算。计费待确认。Billing 未通过，Production 未授权。不扣真实费用。</p>
      {job?.id ? <p>记录 {job.id}</p> : null}
      {!same ? <p>刷新后的记录编号变了，请不要把它当成同一次请求。</p> : null}
      {notice ? <p>{notice}</p> : null}
      <p className="muted">{job?.honesty || "没有真实文生图结果，不能当作已经完成的产品图"}</p>
      <p className="muted">{(job?.pending ?? []).join("；")}</p>
    </Card>
  );
}
