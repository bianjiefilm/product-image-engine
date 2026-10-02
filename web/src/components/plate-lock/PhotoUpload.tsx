"use client";
import { useRef, useState } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";

// PhotoUpload sends the chosen file through the existing BFF routes only:
// /api/photos (server-side validation and content-idempotent registration),
// optionally /api/projects (a personal project when none exists yet), then
// /api/projects/{id}/inputs. It decides nothing about fidelity: whether the photo
// is inside the verified range is the server's answer in the capabilities.
export const MAX_PHOTO_BYTES = 20 << 20;
export const ACCEPT = "image/png,image/jpeg,image/webp";

export async function uploadAndAttach(file: File, projectId: string | undefined, fetcher: typeof fetch = fetch): Promise<string> {
  if (file.size === 0) throw new Error("文件是空的，请重新选择。");
  if (file.size > MAX_PHOTO_BYTES) throw new Error("照片超过 20 MB，请压缩后再试。");
  const bytes = await file.arrayBuffer();
  const qs = new URLSearchParams({ purpose: "project_photo", filename: file.name });
  const up = await fetcher(`/api/photos?${qs.toString()}`, { method: "POST", body: bytes, credentials: "same-origin" });
  const upData = (await up.json().catch(() => null)) as { photo?: { platform_asset_id?: string; original_name?: string; size_bytes?: number; media_type?: string }; error?: { message?: string } } | null;
  if (up.status === 401) throw new Error("请重新登录。");
  if (!up.ok || !upData?.photo?.platform_asset_id) throw new Error(upData?.error?.message ?? "上传没有成功，请稍后重试。");
  let project = projectId;
  if (!project) {
    const created = await fetcher("/api/projects", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: "个人商品图", usage_kind: "电商主图", width_px: 1024, height_px: 1024, source_type: "standalone" }), credentials: "same-origin" });
    const data = (await created.json().catch(() => null)) as { project?: { id?: string } } | null;
    if (!created.ok || !data?.project?.id) throw new Error("创建工程没有成功，照片已保存，请重试。");
    project = data.project.id;
  }
  const photo = upData.photo;
  const attach = await fetcher(`/api/projects/${encodeURIComponent(project)}/inputs`, {
    method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin",
    body: JSON.stringify({ platform_asset_id: photo.platform_asset_id, snapshot_name: photo.original_name ?? file.name, snapshot_size: photo.size_bytes ?? bytes.byteLength, snapshot_content_type: photo.media_type ?? "application/octet-stream" }),
  });
  if (!attach.ok) throw new Error("照片已保存，但没有加入工程，请重试。");
  return project;
}

export function PhotoUpload({ projectId, onAttached }: { projectId?: string; onAttached: (projectId: string) => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const input = useRef<HTMLInputElement | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!file || busy) return;
    setBusy(true); setError(""); setDone("");
    try {
      const project = await uploadAndAttach(file, projectId);
      setDone("照片已加入。");
      setFile(null);
      if (input.current) input.current.value = "";
      onAttached(project);
    } catch (err) {
      setError(err instanceof Error ? err.message : "上传没有成功，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit} aria-label="选择商品照片" data-testid="photo-upload">
      <Field htmlFor="photo-file" label="商品照片" hint="支持 PNG / JPEG / WebP，最大 20 MB。">
        <input id="photo-file" ref={input} type="file" accept={ACCEPT} aria-describedby="photo-file-hint" onChange={e => { setFile(e.target.files?.[0] ?? null); setError(""); setDone(""); }} />
      </Field>
      <Button type="submit" variant="secondary" disabled={!file || busy}>{busy ? "上传中……" : "上传并使用"}</Button>
      {error ? <Alert tone="danger">{error}</Alert> : null}
      {done ? <Alert tone="ok">{done}</Alert> : null}
    </form>
  );
}
