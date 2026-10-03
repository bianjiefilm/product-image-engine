// 限定保真换背景的界面文案与纯函数。这里没有任何网络调用，也没有分数。
// 主视图只用人话；内部编号、哈希、阶段码只允许出现在“技术详情”折叠区。
import type { PlateAxis, PlateAxisState, PlateCandidate, SourceCapabilities, SourceRunView } from "@/lib/source-image";

export const PLATE_TITLE = "限定保真换背景";
export const PLATE_SCOPE_NOTE = "已验证范围：冻结合成样本（纸盒 / 金属把手杯 / 玻璃瓶）。其它照片不在范围内。";
export const PLATE_PROMISE = "商品本身的像素一个都不改，只换背景。";
export const PRICING_NOTE = "费用按生成背景图计。生成后的商品检查未通过时，该结果会标记为不可用，不会自动重做，也不会再次扣费。";
export const RETURN_NOTE = "下游收到的回执不含上述限制说明。";
export const NO_USABLE_PHOTO = "还没有在已验证范围内的商品照片。目前只验证了冻结样本（纸盒 / 金属把手杯 / 玻璃瓶），其它照片不能使用这个模式，也不会产生费用。";
export const STEPS = ["选照片", "写背景", "确认费用", "生成中", "查看结果"] as const;

const STATE_LABEL: Record<PlateAxisState, string> = { PASS: "通过", FAIL: "未通过", UNKNOWN: "待核实", "N/A": "不适用" };
export const stateLabel = (s: PlateAxisState): string => STATE_LABEL[s];

export const AXIS_LABELS: Record<string, string> = {
  mask_pixels_identical: "商品像素与原图完全一致",
  coverage_logo: "Logo 区域保持不变",
  coverage_packaging_text: "包装文字区域保持不变",
  coverage_spec: "规格文字区域保持不变",
  coverage_structure: "商品结构保持不变",
  product_count: "商品数量正确",
  output_size: "输出尺寸正确",
  plate_size: "背景图尺寸正确",
  plate_is_new_background: "背景是新生成的",
  background_changed: "背景相比原图有明显变化",
  plate_task_succeeded: "背景生成已成功",
  plate_asset_hash_verified: "背景图文件已核对",
  plate_charged_observed: "费用已按原报价结算",
  background_extra_objects: "背景里没有多余物体",
  background_intent_followed: "背景符合所写方向",
  visual_composite_quality: "整体观感",
  transmission_edge: "玻璃透射与边缘",
};
export const axisLabel = (name: string): string => AXIS_LABELS[name] ?? "其它检查";

export const REASON_TEXT: Record<string, string> = {
  plate_lock_disabled: "限定保真换背景暂未开放。",
  sample_set_unconfigured: "已验证样本还没有就绪，暂不能使用。",
  sample_set_invalid: "已验证样本没有通过校验，暂不能使用。",
  plate_org_not_supported: "这个模式目前只支持个人付款。",
  project_write_forbidden: "当前账号不能修改这个工程。",
  project_archived: "工程已归档，不能再生成。",
  source_unconfigured: "生成服务暂未就绪。",
  runtime_output_recovery_unconfigured: "生成服务暂未就绪。",
  source_download_hosts_unconfigured: "生成服务暂未就绪。",
};
export const reasonText = (reason: string): string => REASON_TEXT[reason] ?? "暂时不能使用。";

export type Candidate = { label: string; tone: "ok" | "warn" | "bad"; detail: string };
export function candidateSummary(c: PlateCandidate): Candidate {
  switch (c) {
    case "limited_candidate": return { label: "通过商品检查（限定范围）", tone: "ok", detail: "商品像素与原图完全一致。结果只在下面列出的范围内有效。" };
    case "not_usable_candidate": return { label: "未通过商品检查", tone: "bad", detail: "这张图不能选用、导出或回传。费用已按生成的背景图结算，不会自动重做。" };
    case "blocked": return { label: "无法使用", tone: "bad", detail: "这张图所依据的验证样本已变更，不能使用。" };
    default: return { label: "商品检查尚未完成", tone: "warn", detail: "检查完成前不能选用、导出或回传。" };
  }
}

export function plate(v: SourceRunView | null) { return v?.mode === "background_plate_lock" ? v.plate ?? null : null; }
export const charged = (v: SourceRunView): boolean => v.payment.status === "charged";
export const canSelectPlate = (v: SourceRunView): boolean => !!plate(v) && plate(v)!.candidate_state === "limited_candidate" && !v.deleted && charged(v) && !v.selected;
export const canExportPlate = (v: SourceRunView): boolean => !!plate(v) && plate(v)!.candidate_state === "limited_candidate" && !v.deleted && charged(v);
export const canReturnPlate = (v: SourceRunView, returnEnabled: boolean): boolean => returnEnabled && canExportPlate(v) && v.selected;
export const failedAxes = (v: SourceRunView): PlateAxis[] => (plate(v)?.axes ?? []).filter(a => a.machine_checked && a.state === "FAIL");

export function stageOf(v: SourceRunView | null, hasPhoto: boolean, quoteReady: boolean): number {
  if (!v) return hasPhoto ? (quoteReady ? 2 : 1) : 0;
  const p = plate(v);
  if (p && p.derivation_state !== "pending") return 4;
  if (v.confirmed) return 3;
  return 2;
}

export function plateStatusLine(v: SourceRunView | null): string {
  if (!v) return "";
  const p = plate(v);
  if (v.deleted) return "结果已隐藏。";
  if (v.payment.status === "refunded") return "这笔费用已有退款记录，结果不再可用。";
  if (p && p.derivation_state === "derived") return candidateSummary(p.candidate_state).label;
  if (p && p.derivation_state === "blocked") return "结果无法使用。";
  if (v.phase === "quoted") return "报价已生成，确认后才会开始。";
  if (v.phase === "quote_expired") return "报价已过期，请重新获取报价。";
  if (v.phase === "provider_unknown" || v.phase === "settlement_unknown") return "正在核实生成状态，请稍候，不会重复扣费。";
  if (v.phase === "not_dispatched" || v.phase === "canceled" || v.phase === "cancelled") return "这次没有开始生成。";
  if (v.confirmed) return "正在生成背景并检查商品……";
  return "处理中……";
}

// Terms that must never appear in the primary (non-technical) view.
export const TECHNICAL_ONLY_TERMS = ["Task", "Bill", "Hold", "hash", "sha256", "provider", "model", "NOT_RUN", "UNKNOWN", "run_id", "asset_id", "qwen", "usage_id", "quote_id", "Upload"] as const;
