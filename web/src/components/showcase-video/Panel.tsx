"use client";

import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { BILLING_PENDING, CAMERA_MOVES, canStartShowcase, presentShowcaseJob, sameRecord } from "@/lib/showcase-video";
import { maySubmitShowcase } from "@/components/showcase-video/submit-guard";

type ImageOpt = { id: string; label: string };

type Job = {
  id: string;
  product_image_id?: string;
  camera_move?: string;
  job_status?: string;
  status_label?: string;
  quote_status?: string;
  billing_label?: string;
  show_video?: boolean;
  play_still?: boolean;
  video_url?: string;
  honesty?: string;
  pending?: string[];
};

export function ShowcaseVideoPanel({ projectId, images }: { projectId: string; images: ImageOpt[] }) {
  const [imageId, setImageId] = useState("");
  const [cameraMove, setCameraMove] = useState("");
  const [job, setJob] = useState<Job | null>(null);
  const [previousId, setPreviousId] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!projectId) return;
    let gone = false;
    (async () => {
      const list = await fetch(`/api/projects/${projectId}/showcase-videos`);
      if (!list.ok) return;
      const body = (await list.json().catch(() => null)) as { jobs?: Job[] } | null;
      const current = body?.jobs?.[0];
      if (!gone && current?.id) remember(current);
    })();
    return () => {
      gone = true;
    };
  }, [projectId]);

  function remember(next: Job) {
    setJob((prev) => {
      if (prev?.id && prev.id !== next.id) setPreviousId(prev.id);
      return next;
    });
    if (next.product_image_id) setImageId(next.product_image_id);
    if (next.camera_move === "360" || next.camera_move === "scene") setCameraMove(next.camera_move);
  }

  const view = presentShowcaseJob(job ?? {});
  const ready = images.some((item) => item.id === imageId) && canStartShowcase(imageId, cameraMove);
  const same = job?.id ? (previousId ? sameRecord(job.id, previousId) : true) : true;

  async function send(path: string, body?: unknown) {
    const res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    const data = (await res.json().catch(() => null)) as { job?: Job; error?: { message?: string } } | null;
    if (!res.ok) {
      setNotice(data?.error?.message ?? "请求失败");
      return null;
    }
    return data;
  }

  async function begin() {
    if (!projectId || !ready) return;
    setBusy(true);
    setNotice("");
    try {
      const created = await send(`/api/projects/${projectId}/showcase-videos`, {
        product_image_id: imageId,
        camera_move: cameraMove,
      });
      let opened = created?.job;
      if (!opened?.id) return;
      remember(opened);
      if (opened.job_status === "quoted" && opened.quote_status !== "confirmed") {
        const confirmed = await send(`/api/projects/${projectId}/showcase-videos/${opened.id}/confirm`);
        if (!confirmed?.job) return;
        opened = confirmed.job;
        remember(opened);
      }
      if (!maySubmitShowcase(opened)) return;
      const submitted = await send(`/api/projects/${projectId}/showcase-videos/${opened.id}/submit`);
      if (submitted?.job) remember(submitted.job);
    } finally {
      setBusy(false);
    }
  }

  async function refresh() {
    if (!projectId || !job?.id) return;
    setBusy(true);
    setNotice("");
    try {
      const data = await send(`/api/projects/${projectId}/showcase-videos/${job.id}/refresh`);
      if (data?.job) remember(data.job);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card
      data-showcase-entry="true"
      data-show-video="false"
      data-play-still="false"
      data-image-required="true"
      data-billing-passed="false"
      data-production-authorized="false"
    >
      <CardHeader>
        <CardTitle>展示视频</CardTitle>
        <CardDescription>
          用已有产品图发起。运镜只有 360 度或场景运镜。没有真实视频供应商时，这里只记录失败或待确认，不会播放画面。
        </CardDescription>
      </CardHeader>
      <p>
        <Badge variant="warn">{BILLING_PENDING}</Badge>{" "}
        <Badge>{job?.status_label || (job?.job_status === "failed" ? "失败" : "待确认")}</Badge>
      </p>
      {images.length === 0 ? <p>没有产品图就不能发起展示视频。</p> : null}
      <label>
        产品图
        <select value={imageId} onChange={(e) => setImageId(e.target.value)} disabled={images.length === 0}>
          <option value="">选择已有产品图</option>
          {images.map((item) => (
            <option key={item.id} value={item.id}>
              {item.label || item.id}
            </option>
          ))}
        </select>
      </label>
      <fieldset>
        <legend>运镜</legend>
        {CAMERA_MOVES.map((item) => (
          <label key={item.id}>
            <input
              type="radio"
              name="showcase-camera-move"
              value={item.id}
              checked={cameraMove === item.id}
              onChange={() => setCameraMove(item.id)}
            />
            {item.label}
          </label>
        ))}
      </fieldset>
      <div className="row" style={{ marginTop: 12 }}>
        <Button type="button" disabled={busy || !ready} onClick={() => void begin()}>
          {busy ? "处理中…" : "用这张产品图做展示视频"}
        </Button>
        <Button type="button" variant="outline" disabled={busy || !job?.id} onClick={() => void refresh()}>
          刷新同一条记录
        </Button>
      </div>
      <p>{view.headline}</p>
      <p>计费待确认。Billing 未通过，Production 未授权。不扣真实费用。</p>
      {job?.id ? <p>记录 {job.id}</p> : null}
      {!same ? <p>刷新后的记录编号变了，请不要把它当成同一次请求。</p> : null}
      {notice ? <p>{notice}</p> : null}
      <p className="muted">{job?.honesty || "没有真实展示视频，不能播放静图或假视频"}</p>
      <p className="muted">{(job?.pending ?? []).join("；")}</p>
    </Card>
  );
}
