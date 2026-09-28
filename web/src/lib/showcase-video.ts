export const BILLING_PENDING = "计费待确认";

export const CAMERA_MOVES = [
  { id: "360", label: "360 度" },
  { id: "scene", label: "场景运镜" },
] as const;

export function canStartShowcase(imageId: string, cameraMove: string): boolean {
  const image = imageId.trim();
  return image.length > 0 && CAMERA_MOVES.some((item) => item.id === cameraMove);
}

export function presentShowcaseJob(job: {
  id?: string;
  job_status?: string;
  billing_label?: string;
  show_video?: boolean;
  play_still?: boolean;
  video_url?: string;
  billing_passed?: boolean;
  production_authorized?: boolean;
  output_asset_id?: string;
}): {
  showVideo: false;
  playStill: false;
  videoUrl: "";
  billingLabel: typeof BILLING_PENDING;
  billingPassed: false;
  productionAuthorized: false;
  headline: string;
} {
  const failed = (job.job_status ?? "") === "failed";
  return {
    showVideo: false,
    playStill: false,
    videoUrl: "",
    billingLabel: BILLING_PENDING,
    billingPassed: false,
    productionAuthorized: false,
    headline: failed ? "未能生成，没有真实展示视频" : "结果待确认，没有真实展示视频",
  };
}

export function sameRecord(currentId: string, previousId: string): boolean {
  return currentId !== "" && currentId === previousId;
}
