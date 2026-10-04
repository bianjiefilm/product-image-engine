export const NOT_FINISHED_COPY = "还没生成成片。这条只是已采用商品的 Motion 引用声明，不是动态广告。";

const DIGEST_RE = /^sha256:[0-9a-f]{64}$/;

export type MotionDeclaration = {
  id?: string;
  asset_id?: string;
  content_hash?: string;
  revision_id?: string;
  digest?: string;
  subject_hash?: string;
  product_regeneration?: boolean;
  model_call_count?: number;
  video_generated?: boolean;
  verified?: boolean;
  dynamic_ad?: boolean;
  finished_copy?: string;
};

// dynamicAdLabel 拒绝把未核验引用说成已经做出的动态广告。
export function dynamicAdLabel(declaration: MotionDeclaration): string {
  if (declaration.verified === true || declaration.dynamic_ad === true || declaration.video_generated === true) {
    throw new Error("未核验的引用不能当成动态广告");
  }
  if (!declaration.finished_copy?.includes("还没生成成片")) {
    throw new Error("还没生成成片");
  }
  return declaration.finished_copy;
}

export function isMotionDigest(value: string): boolean {
  return DIGEST_RE.test(value);
}
