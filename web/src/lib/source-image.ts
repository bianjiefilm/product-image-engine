export type SourceScope = { userID: string; accountID: string; projectID: string };
export type SourcePendingHint = SourceScope & { requestKey: string };
export type SourceQuote = { quote_id: string; quote_fingerprint: string; usage_id: string; currency: string; quantity: string; unit_price_minor: string; amount_minor: string; quoted_at_unix: number; expires_at_unix: number; pricing_version: string };
export type SourcePayment = { currency: string; payer_source: string; payer_label: string; payer_account_id: string; payer_user_id: string; status: string; charged_minor: string | null; original_charged_minor: string | null; refund_amount_minor: string | null; refresh_status: string; observation_time_known: boolean; last_verified_at: number | null; hold_id?: string; charge_id?: string; release_id?: string; refund_id?: string; original_charge_id?: string };
export type SourceOutput = { asset_id: string; reference_id: string; sha256: string; content_type: string; size_bytes: string; width_px: number; height_px: number; association_verified: boolean };
export type SourceModeName = "text_generate" | "background_plate_lock";
export type PlateAxisState = "PASS" | "FAIL" | "UNKNOWN" | "N/A";
export type PlateAxis = { name: string; state: PlateAxisState; machine_checked: boolean; evidence: string };
export type PlateCandidate = "limited_candidate" | "pending" | "not_usable_candidate" | "blocked";
export type PlateView = {
  result_kind: "derived_composite"; claim: "frozen_synthetic_sample_only"; human_usefulness: "NOT_RUN";
  derivation_state: "pending" | "derived" | "blocked" | "invalid"; candidate_state: PlateCandidate;
  sample_set_id?: string; case_id?: string; background_intent?: string; canvas?: { width_px: number; height_px: number }; blocked_reason?: string;
  plate_task?: { task_id: string; asset_id: string; reference_id: string; sha256: string };
  verdicts?: { fidelity: string; generation: string; billing: string; human_usefulness: "NOT_RUN" };
  axes?: PlateAxis[]; limits?: string[]; report_sha256?: string; composite_sha256?: string;
  result?: { content_type: "image/png"; size_bytes: string; width_px: number; height_px: number; sha256: string };
};
export type SourceRunView = { run_id: string; project_id: string; request_key: string; owner_version: string; mode: SourceModeName; model: string; provider: string; size: string; quantity: string; phase: string; task_id: string; task_phase?: string; task_status?: string; selected: boolean; deleted: boolean; cancel_requested: boolean; confirmed: boolean; payment: SourcePayment; quote: SourceQuote | null; output: SourceOutput | null; snapshot_updated_at: number; fidelity: "not_applicable" | "frozen_sample_only"; visual_quality: string; human_adoption: string; automatic_output_recovery_ready: boolean; content_availability: string; plate?: PlateView };
export type PlateSupportedInput = { input_id: string; kind: string; canvas: { width_px: number; height_px: number } };
export type SourceMode = { mode: string; ready: boolean; disabled: boolean; reason: string; can_quote?: boolean; can_confirm?: boolean; authorization_reason?: string; runtime_reason?: string; model?: string; size?: string; fidelity?: string; visual_quality?: string; claim?: string; supported_input_ids?: string[]; supported_inputs?: PlateSupportedInput[] };
export type SourceCapabilities = { modes: SourceMode[]; historical_read: boolean; automatic_output_recovery_ready: boolean };
export type SourceList = { runs: SourceRunView[]; next_cursor: string };
export type TextCreate = { request_key: string; mode: "text_generate"; prompt: string; size: "1024*1024" };
export type PlateCreate = { request_key: string; mode: "background_plate_lock"; input_id: string; background_intent: string };
export type SourceCreate = TextCreate | PlateCreate;
export type SourceConfirm = { quote_id: string; quote_fingerprint: string };
export interface SourceImageAPI {
  capabilities(project: string): Promise<SourceCapabilities>;
  create(project: string, input: SourceCreate): Promise<SourceRunView>;
  confirm(project: string, run: string, input: SourceConfirm): Promise<SourceRunView>;
  get(project: string, run: string): Promise<SourceRunView>;
  list(project: string, cursor?: string): Promise<SourceList>;
  find(project: string, key: string): Promise<SourceRunView | null>;
  reconcile(project: string, run: string): Promise<SourceRunView>;
  cancel(project: string, run: string): Promise<SourceRunView>;
  select(project: string, run: string): Promise<SourceRunView>;
  deleteOutput(project: string, run: string): Promise<SourceRunView>;
  returnOutput(project: string, run: string): Promise<{ output_id: string }>;
}
export class SourceHTTPError extends Error { constructor(public status: number, public code: string) { super(code); } }
const MAX_MINOR = 9223372036854775807n;
export function isMinor(v: unknown): v is string {
  return typeof v === "string" && /^(0|[1-9][0-9]{0,18})$/.test(v) && BigInt(v) <= MAX_MINOR;
}
export function formatMinor(v: unknown): string {
  if (!isMinor(v)) return "金额待核实";
  const n = BigInt(v); return `¥${n / 100n}.${(n % 100n).toString().padStart(2,"0")}`;
}
export function sourceSegment(v: unknown): v is string { return typeof v === "string" && /^[A-Za-z0-9_-]{1,256}$/.test(v); }
const text = (v: unknown, max = 256): string => {
  if (typeof v !== "string" || v.length > max || /[\u0000-\u001f\u007f]/.test(v)) throw new Error("invalid_source_response");
  return v;
};
const id = (v: unknown, empty = false): string => { if (empty && v === "") return ""; if (!sourceSegment(v)) throw new Error("invalid_source_response"); return v; };
// Platform task-source capture names references "task-source:v1:<task_id>:(input|output)";
// the colons are load-bearing evidence, so reference fields accept that exact shape on top
// of the plain segment alphabet (real-stack contract, HUI-2232 M1).
const TASK_SOURCE_REFERENCE = /^task-source:v1:[A-Za-z0-9_-]+:(input|output)$/;
const referenceId = (v: unknown, empty = false): string => { if (typeof v === "string" && TASK_SOURCE_REFERENCE.test(v)) return v; return id(v, empty); };
const bool = (v: unknown): boolean => { if (typeof v !== "boolean") throw new Error("invalid_source_response"); return v; };
const integer = (v: unknown): number => { if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) throw new Error("invalid_source_response"); return v; };
const object = (v: unknown): Record<string,unknown> => { if (!v || typeof v !== "object" || Array.isArray(v)) throw new Error("invalid_source_response"); return v as Record<string,unknown>; };
const member = (v: unknown, allowed: string[]): string => { const s = text(v); if (!allowed.includes(s)) throw new Error("invalid_source_response"); return s; };
const minor = (v: unknown): string => { if (!isMinor(v)) throw new Error("invalid_source_response"); return v; };
const nullableMinor = (v: unknown): string | null => v === null ? null : minor(v);

// Parse before projection: duplicate/case-alias keys and noncanonical numeric
// shapes must not be silently normalized by JSON.parse. No money uses Number.
export function parseSourceJSON(raw: string): unknown {
  let at = 0;
  const space = () => { while (/[\t\r\n ]/.test(raw[at] ?? "x")) at++; };
  const str = (): string => {
    const m = /^"(?:[^"\\\u0000-\u001f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"/.exec(raw.slice(at));
    if (!m) throw new Error("invalid_source_json"); at += m[0].length;
    const s = JSON.parse(m[0]) as string;
    for (const c of s) { const n = c.codePointAt(0)!; if (n >= 0xd800 && n <= 0xdfff) throw new Error("invalid_source_json"); }
    return s;
  };
  const value = (depth: number): unknown => {
    if (depth > 8) throw new Error("invalid_source_json"); space();
    if (raw[at] === '"') return str();
    if (raw[at] === "{") {
      at++; space(); const out: Record<string,unknown> = Object.create(null); const keys = new Set<string>();
      if (raw[at] === "}") { at++; return out; }
      while (true) { space(); const k = str(); const folded = k.toLowerCase(); if (keys.has(folded)) throw new Error("invalid_source_json"); keys.add(folded); space(); if (raw[at++] !== ":") throw new Error("invalid_source_json"); out[k] = value(depth+1); space(); const c=raw[at++]; if (c === "}") return out; if (c !== ",") throw new Error("invalid_source_json"); }
    }
    if (raw[at] === "[") { at++; space(); const out: unknown[] = []; if (raw[at] === "]") { at++; return out; } while (true) { out.push(value(depth+1)); space(); const c=raw[at++]; if(c === "]") return out; if(c !== ",") throw new Error("invalid_source_json"); } }
    for (const [literal, v] of [["true",true],["false",false],["null",null]] as const) if (raw.startsWith(literal,at)) { at+=literal.length; return v; }
    const m = /^-?(?:0|[1-9][0-9]*)/.exec(raw.slice(at)); if (!m) throw new Error("invalid_source_json"); at+=m[0].length;
    const n=Number(m[0]); if (!Number.isSafeInteger(n) || m[0] === "-0") throw new Error("invalid_source_json"); return n;
  };
  const result = value(0); space(); if(at !== raw.length) throw new Error("invalid_source_json"); return result;
}
function decodeQuote(v: unknown): SourceQuote {
  const q=object(v); const hash=text(q.quote_fingerprint); if(!/^[0-9a-f]{64}$/.test(hash)) throw new Error("invalid_source_response");
  const result: SourceQuote = { quote_id:id(q.quote_id), quote_fingerprint:hash, usage_id:id(q.usage_id), currency:member(q.currency,["CNY"]), quantity:member(q.quantity,["1"]), unit_price_minor:minor(q.unit_price_minor), amount_minor:minor(q.amount_minor), quoted_at_unix:integer(q.quoted_at_unix), expires_at_unix:integer(q.expires_at_unix), pricing_version:id(q.pricing_version) };
  if(result.amount_minor !== result.unit_price_minor || result.amount_minor === "0" || result.expires_at_unix-result.quoted_at_unix !== 300) throw new Error("invalid_source_response");
  return result;
}
const arrayOf = <T,>(v: unknown, max: number, each: (x: unknown) => T): T[] => { if (!Array.isArray(v) || v.length > max) throw new Error("invalid_source_response"); return v.map(each); };
const AXIS_STATES: PlateAxisState[] = ["PASS", "FAIL", "UNKNOWN", "N/A"];
const CANDIDATES: PlateCandidate[] = ["limited_candidate", "pending", "not_usable_candidate", "blocked"];
export function decodePlate(v: unknown): PlateView {
  const p = object(v);
  const out: PlateView = {
    result_kind: member(p.result_kind, ["derived_composite"]) as "derived_composite", claim: member(p.claim, ["frozen_synthetic_sample_only"]) as "frozen_synthetic_sample_only",
    human_usefulness: member(p.human_usefulness, ["NOT_RUN"]) as "NOT_RUN", derivation_state: member(p.derivation_state, ["pending", "derived", "blocked", "invalid"]) as PlateView["derivation_state"],
    candidate_state: member(p.candidate_state, CANDIDATES) as PlateCandidate,
  };
  if (p.sample_set_id !== undefined) out.sample_set_id = id(p.sample_set_id);
  if (p.case_id !== undefined) out.case_id = text(p.case_id, 64);
  if (p.background_intent !== undefined) out.background_intent = text(p.background_intent, 800);
  if (p.blocked_reason !== undefined) out.blocked_reason = id(p.blocked_reason);
  if (p.canvas !== undefined) { const c = object(p.canvas); out.canvas = { width_px: integer(c.width_px), height_px: integer(c.height_px) }; }
  if (p.plate_task !== undefined) { const t = object(p.plate_task); out.plate_task = { task_id: id(t.task_id, true), asset_id: id(t.asset_id), reference_id: referenceId(t.reference_id), sha256: text(t.sha256, 64) }; }
  if (p.verdicts !== undefined) { const d = object(p.verdicts); out.verdicts = { fidelity: member(d.fidelity, ["PASS", "FAIL", "UNKNOWN"]), generation: member(d.generation, ["PASS", "FAIL", "UNKNOWN"]), billing: member(d.billing, ["PASS", "FAIL", "UNKNOWN"]), human_usefulness: member(d.human_usefulness, ["NOT_RUN"]) as "NOT_RUN" }; }
  if (p.axes !== undefined) out.axes = arrayOf(p.axes, 24, x => { const a = object(x); return { name: id(a.name), state: member(a.state, AXIS_STATES) as PlateAxisState, machine_checked: bool(a.machine_checked), evidence: text(a.evidence, 512) }; });
  if (p.limits !== undefined) out.limits = arrayOf(p.limits, 12, x => text(x, 512));
  if (p.report_sha256 !== undefined) out.report_sha256 = text(p.report_sha256, 64);
  if (p.composite_sha256 !== undefined) out.composite_sha256 = text(p.composite_sha256, 64);
  if (p.result !== undefined) { const r = object(p.result); out.result = { content_type: member(r.content_type, ["image/png"]) as "image/png", size_bytes: minor(r.size_bytes), width_px: integer(r.width_px), height_px: integer(r.height_px), sha256: text(r.sha256, 64) }; }
  if (out.candidate_state === "limited_candidate" && (out.derivation_state !== "derived" || !out.verdicts || !out.axes || !out.limits || out.limits.length === 0 || !out.result)) throw new Error("invalid_source_response");
  return out;
}
export function decodeSourceRun(v: unknown): SourceRunView {
  const r=object(v), p=object(r.payment);
  const payment: SourcePayment = { currency:member(p.currency,["CNY"]), payer_source:member(p.payer_source,["personal","delegation"]), payer_label:member(p.payer_label,["个人账户","组织委托账户"]), payer_account_id:id(p.payer_account_id), payer_user_id:id(p.payer_user_id), status:member(p.status,["unknown","ingested","quoted","held","charged","released","refunded","reconciling"]), charged_minor:nullableMinor(p.charged_minor), original_charged_minor:nullableMinor(p.original_charged_minor), refund_amount_minor:nullableMinor(p.refund_amount_minor), refresh_status:member(p.refresh_status,["unknown","not_refreshed"]), observation_time_known:bool(p.observation_time_known), last_verified_at:p.last_verified_at === null ? null : integer(p.last_verified_at) };
  for (const k of ["hold_id","charge_id","release_id","refund_id","original_charge_id"] as const) if(p[k] !== undefined) payment[k]=id(p[k],true);
  const modeName=member(r.mode,["text_generate","background_plate_lock"]) as SourceModeName;
  let output: SourceOutput | null=null;
  if(modeName === "background_plate_lock" && r.output !== null) throw new Error("invalid_source_response");
  if(r.output !== null) { const o=object(r.output); output={ asset_id:id(o.asset_id), reference_id:referenceId(o.reference_id), sha256:text(o.sha256), content_type:member(o.content_type,["image/png"]), size_bytes:minor(o.size_bytes), width_px:integer(o.width_px), height_px:integer(o.height_px), association_verified:bool(o.association_verified) }; if(!/^[0-9a-f]{64}$/.test(output.sha256) || output.width_px !== 1024 || output.height_px !== 1024 || !output.association_verified || BigInt(output.size_bytes) < 1n || BigInt(output.size_bytes)>16777216n) throw new Error("invalid_source_response"); }
  const result: SourceRunView={ run_id:id(r.run_id), project_id:id(r.project_id), request_key:text(r.request_key), owner_version:member(r.owner_version,["product_source_v1"]), mode:modeName, model:member(r.model,["qwen-image-2.0"]), provider:member(r.provider,["modelxing-qwen-image-2.0-v1"]), size:member(r.size,["1024*1024"]), quantity:member(r.quantity,["1"]), phase:member(r.phase,["intent","usage_pending","quote_pending","quoted","quote_expired","confirmed","task_submit_pending","task_linked","output_ready","settlement_pending","succeeded","not_dispatched","cancel_pending","provider_unknown","asset_pending","settlement_unknown","review_required","canceled","cancelled","failed","output_deleted"]), task_id:id(r.task_id,true), selected:bool(r.selected), deleted:bool(r.deleted), cancel_requested:bool(r.cancel_requested), confirmed:bool(r.confirmed), payment, quote:r.quote === null ? null : decodeQuote(r.quote), output, snapshot_updated_at:integer(r.snapshot_updated_at), fidelity:member(r.fidelity,modeName === "text_generate" ? ["not_applicable"] : ["frozen_sample_only"]) as SourceRunView["fidelity"], visual_quality:member(r.visual_quality,["unknown"]), human_adoption:member(r.human_adoption,["NOT_RUN"]), automatic_output_recovery_ready:bool(r.automatic_output_recovery_ready), content_availability:member(r.content_availability,["not_checked"]) };
  if (!result.run_id.startsWith("sir_") || result.deleted && (result.output !== null || result.selected)) throw new Error("invalid_source_response");
  if(modeName === "background_plate_lock") result.plate=decodePlate(r.plate); else if(r.plate !== undefined) throw new Error("invalid_source_response");
  if(r.task_phase !== undefined) result.task_phase=id(r.task_phase,true); if(r.task_status !== undefined) result.task_status=id(r.task_status,true);
  return result;
}
export function decodeSourceCapabilities(v: unknown): SourceCapabilities {
  const c=object(v); if(!Array.isArray(c.modes) || c.modes.length !== 3) throw new Error("invalid_source_response");
  const modes = c.modes.map(v => { const m=object(v); const out: SourceMode={ mode:member(m.mode,["text_generate","background_plate_lock","reference_edit"]), ready:bool(m.ready), disabled:bool(m.disabled), reason:id(m.reason,true) };
    for(const k of ["can_quote","can_confirm"] as const) if(m[k] !== undefined) out[k]=bool(m[k]);
    for(const k of ["authorization_reason","runtime_reason"] as const) if(m[k] !== undefined) out[k]=id(m[k],true);
    if(out.mode === "text_generate") { out.model=member(m.model,["qwen-image-2.0"]); out.size=member(m.size,["1024*1024"]); out.fidelity=member(m.fidelity,["not_applicable"]); out.visual_quality=member(m.visual_quality,["unknown"]); }
    if(out.mode === "background_plate_lock") {
      out.model=member(m.model,["qwen-image-2.0"]); out.size=member(m.size,["1024*1024"]); out.fidelity=member(m.fidelity,["frozen_sample_only"]); out.visual_quality=member(m.visual_quality,["unknown"]); out.claim=member(m.claim,["frozen_synthetic_sample_only"]);
      out.supported_input_ids=arrayOf(m.supported_input_ids,100,x=>id(x));
      out.supported_inputs=arrayOf(m.supported_inputs,100,x=>{ const i=object(x),c=object(i.canvas); return { input_id:id(i.input_id), kind:member(i.kind,["carton","handled_metal","glass_bottle"]), canvas:{width_px:integer(c.width_px),height_px:integer(c.height_px)} }; });
    }
    return out;
  });
  if(new Set(modes.map(m=>m.mode)).size !== 3) throw new Error("invalid_source_response");
  return { modes, historical_read:bool(c.historical_read), automatic_output_recovery_ready:bool(c.automatic_output_recovery_ready) };
}
function validateCursor(v: string): void {
  if(!/^[A-Za-z0-9_-]{1,1024}$/.test(v)) throw new Error("invalid_source_response");
  const base=v.replace(/-/g,"+").replace(/_/g,"/"); const binary=atob(base);
  if(btoa(binary).replace(/=/g,"").replace(/\+/g,"-").replace(/\//g,"_") !== v) throw new Error("invalid_source_response");
  const c=object(parseSourceJSON(new TextDecoder("utf-8",{fatal:true}).decode(Uint8Array.from(binary,ch=>ch.charCodeAt(0)))));
  if(Object.keys(c).sort().join(",") !== "created_at,id,v" || c.v !== 1 || !isMinor(c.created_at) || c.created_at === "0" || !sourceSegment(c.id) || !c.id.startsWith("sir_")) throw new Error("invalid_source_response");
}
export function decodeSourceList(v: unknown): SourceList { const d=object(v); if(!Array.isArray(d.runs) || d.runs.length>100) throw new Error("invalid_source_response"); const next=text(d.next_cursor,1024); if(next) validateCursor(next); return { runs:d.runs.map(decodeSourceRun), next_cursor:next }; }
export function canConfirm(v: SourceRunView, nowUnix: number): boolean {
  try { const q=decodeQuote(v.quote); return v.phase === "quoted" && !v.confirmed && !v.deleted && !v.cancel_requested && (v.mode === "text_generate" || v.mode === "background_plate_lock") && v.size === "1024*1024" && v.payment.status === "quoted" && q.quoted_at_unix <= nowUnix && nowUnix < q.expires_at_unix; } catch { return false; }
}
export function requestState(v: SourceRunView): string {
  if(v.deleted) return "输出已隐藏，引用清理待核实（不代表退款）";
  if(v.payment.status === "refunded") return "原账单已有退款事实，退款金额待核实";
  if(v.phase === "asset_pending") return "素材保存中";
  if(v.phase === "provider_unknown") return "供应商状态待核实，保留原请求";
  if(v.phase === "settlement_unknown" || v.phase === "settlement_pending" || v.output && v.payment.status !== "charged") return "已有结果，费用待核实";
  if(v.phase === "succeeded" && v.payment.status === "charged") return v.payment.refresh_status === "unknown" ? "原账单已结算，当前费用待核实" : "原账单已结算，当前费用未刷新";
  if(v.output) return "结果已收到";
  if(v.phase === "quoted") return "报价已取得，等待明确确认";
  if(v.phase === "quote_expired") return "原报价已过期";
  if(v.phase === "not_dispatched") return "已阻止提交，原费用仍按账单事实展示";
  if(v.phase === "cancel_pending") return "取消待核实，不宣称已释放费用";
  return "等待结果，保留原请求";
}
export function sourceWritesDenied(c: SourceCapabilities | null, name: SourceModeName="text_generate"): boolean {
  const mode=c?.modes.find(m=>m.mode === name);
  return !mode || mode.authorization_reason === "project_write_forbidden" || mode.reason === "project_archived";
}
export function validBackground(v: string): boolean { return v.length>0 && v === v.trim() && Array.from(v).length<=200 && !/[\u0000-\u001f\u007f]/.test(v); }
export const pendingHintKey = (s: SourceScope, mode: SourceModeName="text_generate"): string => (mode === "text_generate" ? "source-image-pending:" : "plate-lock-pending:") + JSON.stringify([s.userID,s.accountID,s.projectID]);
export function sourcePath(project: string, run?: string, action?: "content" | "export" | "return"): string {
  if(!sourceSegment(project) || run !== undefined && !sourceSegment(run)) throw new Error("invalid_source_path");
  return `/api/projects/${encodeURIComponent(project)}/source-image-runs${run ? "/"+encodeURIComponent(run) : ""}${action ? "/"+action : ""}`;
}
async function sourceFetch(path: string, method="GET", body?: unknown): Promise<unknown> {
  const abort=new AbortController(); const timeout=setTimeout(()=>abort.abort(),22000);
  try { const r=await fetch(path,{method,body:body === undefined ? undefined : JSON.stringify(body),headers:body === undefined ? undefined : {"Content-Type":"application/json"},cache:"no-store",credentials:"same-origin",redirect:"error",signal:abort.signal});
    const raw=await r.text(); if(new TextEncoder().encode(raw).length>1048576) throw new SourceHTTPError(503,"source_unavailable");
    let data: unknown; try { data=parseSourceJSON(raw); } catch { throw new SourceHTTPError(503,"source_unavailable"); }
    if(!r.ok) { const d=object(data); const e=object(d.error); throw new SourceHTTPError(r.status,typeof e.code === "string" ? e.code : "source_unavailable"); } return data;
  } catch(e) { if(e instanceof SourceHTTPError) throw e; throw new SourceHTTPError(503,"source_unavailable"); } finally { clearTimeout(timeout); }
}
export const sourceImageAPI: SourceImageAPI = {
  capabilities:async p=>decodeSourceCapabilities(await sourceFetch(`/api/projects/${encodeURIComponent(p)}/source-image-capabilities`)),
  create:async(p,b)=>decodeSourceRun(await sourceFetch(sourcePath(p),"POST",b)),
  confirm:async(p,r,b)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/confirm","POST",b)),
  get:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r))),
  list:async(p,c)=>decodeSourceList(await sourceFetch(sourcePath(p)+(c ? "?cursor="+encodeURIComponent(c) : ""))),
  find:async(p,k)=>{ try {return decodeSourceRun(await sourceFetch(sourcePath(p)+"/by-request?request_key="+encodeURIComponent(k)));} catch(e) {if(e instanceof SourceHTTPError && e.status === 404) return null; throw e;} },
  reconcile:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/reconcile","POST",{})),
  cancel:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/cancel","POST",{})),
  select:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/select","POST",{})),
  deleteOutput:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/output","DELETE",{})),
  returnOutput:async(p,r)=>{ const d=object(await sourceFetch(sourcePath(p,r)+"/return","POST",{})); const o=object(d.output); return { output_id:id(o.id) }; },
};
export class SourceImageController {
  view: SourceRunView | null=null;
  capabilities: SourceCapabilities | null=null;
  private scope: SourceScope | null=null;
  private epoch=0;
  private pending: { hint: SourcePendingHint; input: SourceCreate | null; attempted: boolean } | null=null;
  private active: Promise<void> | null=null;
  private shownQuote="";
  constructor(private api: SourceImageAPI, private uuid: () => string=()=>crypto.randomUUID(), private now: () => number=()=>Math.floor(Date.now()/1000), private mode: SourceModeName="text_generate") {}
  get pendingHint(): SourcePendingHint | null { return this.pending ? { ...this.pending.hint } : null; }
  setScope(s: SourceScope | null): void {
    if(s && (!sourceSegment(s.userID) || !sourceSegment(s.accountID) || !sourceSegment(s.projectID))) throw new Error("invalid_source_scope");
    if(JSON.stringify(s) !== JSON.stringify(this.scope)) { this.epoch++; this.view=null; this.capabilities=null; this.pending=null; this.active=null; this.shownQuote=""; this.scope=s ? {...s} : null; }
  }
  private current(): SourceScope { if(!this.scope) throw new Error("未验证当前登录身份与工程"); return this.scope; }
  private remember(v: SourceRunView, epoch: number): void { const s=this.current(); if(v.mode !== this.mode) throw new Error("请求类型不一致，请重新读取"); if(epoch !== this.epoch || v.project_id !== s.projectID || v.payment.payer_user_id !== s.userID || v.payment.payer_source === "personal" && v.payment.payer_account_id !== s.accountID) throw new Error("登录范围已改变，请重新读取"); this.view=decodeSourceRun(v); this.shownQuote=JSON.stringify(this.view.quote); }
  private rememberOriginal(next: SourceRunView, previous: SourceRunView, epoch: number): void {
    if(next.run_id !== previous.run_id || next.request_key !== previous.request_key || previous.quote !== null && JSON.stringify(next.quote) !== JSON.stringify(previous.quote) || previous.task_id && next.task_id !== previous.task_id) throw new Error("原请求事实不一致");
    this.remember(next,epoch);
  }
  private coalesce(work: () => Promise<void>): Promise<void> { if(this.active) return this.active; const result=work(); this.active=result; void result.finally(()=>{if(this.active === result) this.active=null;}).catch(()=>{}); return result; }
  quote(input: { projectID: string; prompt: string; size: "1024*1024" }): Promise<void> {
    return this.begin(input.projectID, "text_generate", () => {
      const prompt=input.prompt.trim(); if(!prompt || new TextEncoder().encode(prompt).length>16384 || input.size !== "1024*1024") throw new Error("文字描述不合法");
      return (requestKey: string): SourceCreate => ({ request_key:requestKey, mode:"text_generate", prompt, size:"1024*1024" });
    });
  }
  // The only things a browser contributes to a limited-fidelity run are which of
  // its own registered photos and the words describing the background.
  quotePlate(input: { projectID: string; inputID: string; background: string }): Promise<void> {
    return this.begin(input.projectID, "background_plate_lock", () => {
      const background=input.background.trim(), plate=this.capabilities?.modes.find(m=>m.mode === "background_plate_lock");
      if(!validBackground(background)) throw new Error("背景描述需为 1 到 200 个字，且不含换行");
      if(!sourceSegment(input.inputID) || !plate?.supported_input_ids?.includes(input.inputID)) throw new Error("这张照片不在已验证范围内");
      return (requestKey: string): SourceCreate => ({ request_key:requestKey, mode:"background_plate_lock", input_id:input.inputID, background_intent:background });
    });
  }
  private begin(projectID: string, modeName: SourceModeName, prepare: () => (requestKey: string) => SourceCreate): Promise<void> { return this.coalesce(async()=>{
    const s=this.current(), epoch=this.epoch; if(projectID !== s.projectID || this.capabilities?.modes.find(m=>m.mode === modeName)?.can_quote !== true) throw new Error("当前工程不允许获取正式报价");
    const make=prepare();
    if(this.view && !this.pending) throw new Error("已有原请求，请显式重新生成");
    const body: SourceCreate=make(this.pending?.hint.requestKey ?? this.uuid());
    if(!sourceSegment(body.request_key)) throw new Error("请求键不合法");
    if(this.pending?.input && JSON.stringify(body) !== JSON.stringify(this.pending.input)) throw new Error("原请求内容不一致，请保留原请求或显式重新生成");
    if(!this.pending) this.pending={ hint:{...s,requestKey:body.request_key},input:body,attempted:false };
    if(this.pending.attempted) { const found=await this.api.find(s.projectID,body.request_key); if(epoch !== this.epoch) throw new Error("登录范围已改变"); if(found) { if(found.request_key !== body.request_key) throw new Error("原请求键不一致"); this.remember(found,epoch); return; } if(this.pending.input === null) throw new Error("原请求内容未知，只能读取原请求；新生成须另行明确操作"); }
    this.pending.input=body; this.pending.attempted=true;
    const v=await this.api.create(s.projectID,body); if(v.request_key !== body.request_key) throw new Error("原请求事实不一致"); this.remember(v,epoch);
  }); }
  confirm(): Promise<void> { return this.coalesce(async()=>{ const s=this.current(), v=this.view, epoch=this.epoch;
    if(!v || this.capabilities?.modes.find(m=>m.mode === v.mode)?.can_confirm !== true || !canConfirm(v,this.now()) || this.shownQuote !== JSON.stringify(v.quote)) throw new Error("原报价未验证、已过期或已改变");
    this.rememberOriginal(await this.api.confirm(s.projectID,v.run_id,{quote_id:v.quote!.quote_id,quote_fingerprint:v.quote!.quote_fingerprint}),v,epoch);
  }); }
  async loadCapabilities(): Promise<void> { const s=this.current(), epoch=this.epoch; const c=await this.api.capabilities(s.projectID); if(epoch !== this.epoch) throw new Error("登录范围已改变"); this.capabilities=c; }
  async restore(project: string, run: string): Promise<void> { const s=this.current(), epoch=this.epoch; if(project !== s.projectID || !sourceSegment(run)) throw new Error("工程/请求不合法"); const found=await this.api.get(project,run);if(found.run_id !== run) throw new Error("原请求编号不一致");if(this.view?.run_id === run) this.rememberOriginal(found,this.view,epoch);else this.remember(found,epoch); }
  async history(cursor?: string): Promise<SourceList> { const s=this.current(), epoch=this.epoch; const result=await this.api.list(s.projectID,cursor); if(epoch !== this.epoch) throw new Error("登录范围已改变"); for(const v of result.runs) if(v.project_id !== s.projectID || v.payment.payer_user_id !== s.userID) throw new Error("历史范围不一致"); return result; }
  async restoreHint(h: unknown): Promise<void> { const s=this.current(), epoch=this.epoch; if(!h || typeof h !== "object") return; const v=h as SourcePendingHint;
    if(v.userID !== s.userID || v.accountID !== s.accountID || v.projectID !== s.projectID || !sourceSegment(v.requestKey)) return;
    this.pending={hint:{...s,requestKey:v.requestKey},input:null,attempted:true}; const r=await this.api.find(s.projectID,v.requestKey); if(epoch !== this.epoch) throw new Error("登录范围已改变"); if(r) {if(r.request_key !== v.requestKey) throw new Error("原请求键不一致");this.remember(r,epoch);}
  }
  action(name: "reconcile" | "cancel" | "select" | "deleteOutput"): Promise<void> { return this.coalesce(async()=>{ const s=this.current(), epoch=this.epoch, v=this.view; if(!v || sourceWritesDenied(this.capabilities,v.mode)) throw new Error("当前工程不允许写操作"); this.rememberOriginal(await this.api[name](s.projectID,v.run_id),v,epoch); }); }
  // Registers the selected, limited-candidate composite for return to an order or
  // campaign. The server re-checks quality, selection and the original charge.
  async returnToSource(): Promise<string> { let outputID=""; await this.coalesce(async()=>{ const s=this.current(), v=this.view; if(!v || v.mode !== "background_plate_lock" || sourceWritesDenied(this.capabilities,v.mode) || !v.selected || v.plate?.candidate_state !== "limited_candidate") throw new Error("只有已选定的限定候选才能回传"); outputID=(await this.api.returnOutput(s.projectID,v.run_id)).output_id; }); return outputID; }
  newRequest(): void { if(this.active) throw new Error("原动作尚未返回"); this.view=null; this.pending=null; this.shownQuote=""; }
}
