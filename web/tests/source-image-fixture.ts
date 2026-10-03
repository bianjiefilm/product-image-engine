import type { SourceRunView, SourceCapabilities, SourceImageAPI } from "@/lib/source-image";

export const scope = { userID: "usr_a", accountID: "acct_a", projectID: "proj_a" };
export function quoted(): SourceRunView {
  return {
    run_id: "sir_a", project_id: "proj_a", request_key: "request-a", owner_version: "product_source_v1",
    mode: "text_generate", model: "qwen-image-2.0", provider: "modelxing-qwen-image-2.0-v1", size: "1024*1024", quantity: "1",
    phase: "quoted", task_id: "", selected: false, deleted: false, cancel_requested: false, confirmed: false,
    payment: { currency: "CNY", payer_source: "personal", payer_label: "个人账户", payer_account_id: "acct_a", payer_user_id: "usr_a", status: "quoted", charged_minor: null, original_charged_minor: null, refund_amount_minor: null, refresh_status: "not_refreshed", observation_time_known: false, last_verified_at: null },
    quote: { quote_id: "quote_a", quote_fingerprint: "a".repeat(64), usage_id: "usage_a", currency: "CNY", quantity: "1", unit_price_minor: "25", amount_minor: "25", quoted_at_unix: 1700000000, expires_at_unix: 1700000300, pricing_version: "test-price-v1" },
    output: null, snapshot_updated_at: 1700000000, fidelity: "not_applicable", visual_quality: "unknown", human_adoption: "NOT_RUN", automatic_output_recovery_ready: false, content_availability: "not_checked",
  };
}
export function capabilities(): SourceCapabilities {
  return { historical_read: true, automatic_output_recovery_ready: false, modes: [
    { mode: "text_generate", ready: false, disabled: true, can_quote: true, can_confirm: true, reason: "runtime_output_recovery_unconfigured", runtime_reason: "runtime_output_recovery_unconfigured", authorization_reason: "", size: "1024*1024", model: "qwen-image-2.0", fidelity: "not_applicable", visual_quality: "unknown" },
    { mode: "reference_edit", ready: false, disabled: true, reason: "formal_contract_unconfigured" },
    { mode: "background_plate_lock", ready: false, disabled: true, can_quote: false, can_confirm: false, reason: "plate_lock_disabled", size: "1024*1024", model: "qwen-image-2.0", claim: "frozen_synthetic_sample_only", fidelity: "frozen_sample_only", visual_quality: "unknown", supported_input_ids: [], supported_inputs: [] },
  ] };
}
export function sourceAPIFixture(calls: string[]): SourceImageAPI {
  return {
    capabilities: async () => { calls.push("capabilities"); return capabilities(); },
    create: async (_project, input) => { calls.push("create:" + input.request_key); return { ...quoted(), request_key: input.request_key }; },
    confirm: async (_project, _run, input) => { calls.push("confirm:" + input.quote_id + ":" + input.quote_fingerprint); return { ...quoted(), phase: "task_linked", confirmed: true, task_id: "task_a" }; },
    get: async () => { calls.push("get"); return quoted(); },
    list: async () => { calls.push("list"); return { runs: [quoted()], next_cursor: "" }; },
    find: async (_project, key) => { calls.push("find:" + key); return quoted(); },
    reconcile: async () => { calls.push("reconcile"); return quoted(); },
    cancel: async () => { calls.push("cancel"); return { ...quoted(), phase: "not_dispatched", cancel_requested: true }; },
    select: async () => { calls.push("select"); return quoted(); },
    deleteOutput: async () => { calls.push("deleteOutput"); return { ...quoted(), deleted: true, output: null }; },
    returnOutput: async () => { calls.push("returnOutput"); return { output_id: "out_a" }; },
  };
}

export function plateCapabilities(ready = true): SourceCapabilities {
  const caps = capabilities();
  caps.automatic_output_recovery_ready = ready;
  caps.modes = caps.modes.map(m => m.mode === "background_plate_lock" ? { ...m, ready, disabled: !ready, can_quote: ready, can_confirm: ready, reason: ready ? "" : "plate_lock_disabled", supported_input_ids: ready ? ["pin_a"] : [], supported_inputs: ready ? [{ input_id: "pin_a", kind: "carton", canvas: { width_px: 1024, height_px: 1280 } }] : [] } : m);
  return caps;
}
export function plateQuoted(): SourceRunView {
  return { ...quoted(), run_id: "sir_plate", request_key: "request-plate", mode: "background_plate_lock", fidelity: "frozen_sample_only", plate: { result_kind: "derived_composite", claim: "frozen_synthetic_sample_only", human_usefulness: "NOT_RUN", derivation_state: "pending", candidate_state: "pending", case_id: "carton-1024x1280", sample_set_id: "frozen-sample-set", background_intent: "深蓝色渐变摄影棚背景", canvas: { width_px: 1024, height_px: 1280 } } };
}
const PASS_AXES = ["mask_pixels_identical", "coverage_logo", "coverage_packaging_text", "coverage_spec", "coverage_structure", "product_count", "output_size", "plate_size", "plate_is_new_background", "background_changed", "plate_task_succeeded", "plate_asset_hash_verified", "plate_charged_observed"];
export function plateDone(candidate: "limited_candidate" | "not_usable_candidate" = "limited_candidate"): SourceRunView {
  const base = plateQuoted();
  const axes = [...PASS_AXES.map(name => ({ name, state: (candidate === "not_usable_candidate" && name === "background_changed" ? "FAIL" : "PASS") as "PASS" | "FAIL", machine_checked: true, evidence: "ok" })),
    ...["background_extra_objects", "background_intent_followed", "visual_composite_quality"].map(name => ({ name, state: "UNKNOWN" as const, machine_checked: false, evidence: "not machine checkable" })),
    { name: "transmission_edge", state: "N/A" as const, machine_checked: false, evidence: "not machine checkable" }];
  const ok = candidate === "limited_candidate";
  return { ...base, phase: "succeeded", confirmed: true, task_id: "task_a", payment: { ...base.payment, status: "charged", charged_minor: "25", original_charged_minor: "25", charge_id: "charge_a", original_charge_id: "charge_a" },
    plate: { ...base.plate!, derivation_state: "derived", candidate_state: candidate, verdicts: { fidelity: ok ? "PASS" : "PASS", generation: "PASS", billing: "PASS", human_usefulness: "NOT_RUN" }, axes,
      limits: ok ? ["已验证范围仅限冻结合成样本（纸盒/金属把手杯/玻璃瓶），不代表任意商品照片", "背景内容未经机器检查：可能出现多余物体，也可能与所写方向不符"] : [], report_sha256: "b".repeat(64), composite_sha256: "c".repeat(64),
      result: { content_type: "image/png", size_bytes: "52341", width_px: 1024, height_px: 1280, sha256: "c".repeat(64) } } };
}
