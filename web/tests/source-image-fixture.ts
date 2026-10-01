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
    { mode: "background_plate_lock", ready: false, disabled: true, reason: "formal_contract_unconfigured" },
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
  };
}
