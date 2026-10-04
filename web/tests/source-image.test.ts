import { describe, it, expect } from "vitest";
import { SourceImageController, SourceHTTPError, formatMinor, canConfirm, requestState, decodeSourceRun, pendingHintKey } from "@/lib/source-image";
import { quoted, plateDone, sourceAPIFixture, scope, capabilities } from "./source-image-fixture";

function controller(calls: string[]) {
  const api = sourceAPIFixture(calls);
  const c = new SourceImageController(api, () => "request-a", () => 1700000001);
  c.setScope(scope); c.capabilities = capabilities();
  return { c, api };
}
const input = { projectID: "proj_a", prompt: "蓝纸盒", size: "1024*1024" as const };
describe("formal source explicit controller", () => {
  it("quote click never confirms; confirm sends only displayed frozen quote", async () => {
    const calls: string[] = []; const { c } = controller(calls);
    await c.quote(input);
    expect(calls).toEqual(["create:request-a"]); expect(canConfirm(c.view!, 1700000001)).toBe(true);
    await c.confirm();
    expect(calls).toEqual(["create:request-a", "confirm:quote_a:" + "a".repeat(64)]);
    expect(c.view?.task_id).toBe("task_a");
  });
  it("double clicks coalesce the same create and confirm", async () => {
    const calls: string[] = []; const { c } = controller(calls);
    await Promise.all([c.quote(input), c.quote(input)]);
    await Promise.all([c.confirm(), c.confirm()]);
    expect(calls).toEqual(["create:request-a", "confirm:quote_a:" + "a".repeat(64)]);
  });
  it("unknown and expired quotes and tampered displayed quote cannot pay", async () => {
    for (const mutate of [(r: ReturnType<typeof quoted>) => { r.quote = null; }, (r: ReturnType<typeof quoted>) => { r.quote!.expires_at_unix = 1700000001; }, (r: ReturnType<typeof quoted>) => { r.quote!.amount_minor = "9007199254740993"; }]) {
      const calls: string[] = []; const { c } = controller(calls); await c.quote(input); mutate(c.view!);
      await expect(c.confirm()).rejects.toThrow(); expect(calls).toEqual(["create:request-a"]);
    }
  });
  it("capability write denial blocks quote and confirmation without legacy fallback", async () => {
    const calls: string[] = []; const { c } = controller(calls);
    c.capabilities!.modes[0].can_quote = false; await expect(c.quote(input)).rejects.toThrow(); expect(calls).toEqual([]);
    c.capabilities = capabilities(); await c.quote(input); c.capabilities.modes[0].can_confirm = false;
    await expect(c.confirm()).rejects.toThrow(); expect(calls).toEqual(["create:request-a"]);
  });
  it("server-known readonly/archive capability denies explicit recovery/output writes",async()=>{
    for(const reason of ["project_write_forbidden","project_archived"]) {
      const calls:string[]=[];const {c}=controller(calls);await c.restore("proj_a","sir_a");c.capabilities!.modes[0].reason=reason;
      if(reason === "project_write_forbidden") c.capabilities!.modes[0].authorization_reason=reason;
      for(const action of ["reconcile","cancel","select","deleteOutput"] as const) await expect(c.action(action)).rejects.toThrow();expect(calls).toEqual(["get"]);
    }
  });
  it("restore URL and server history are GET only", async () => {
    const calls: string[] = []; const { c } = controller(calls);
    await c.restore("proj_a", "sir_a"); await c.history();
    expect(calls).toEqual(["get", "list"]); expect(c.view?.run_id).toBe("sir_a");
  });
  it("lost create response looks up the original key before any repeat", async () => {
    const calls: string[] = []; const { c, api } = controller(calls);
    api.create = async (_p, body) => { calls.push("create:" + body.request_key); throw new SourceHTTPError(503, "source_unavailable"); };
    await expect(c.quote(input)).rejects.toThrow();
    const hint = c.pendingHint; expect(hint).toEqual({ ...scope, requestKey: "request-a" });
    await c.quote(input); expect(calls).toEqual(["create:request-a", "find:request-a"]);
    expect(c.view?.request_key).toBe("request-a");
  });
  it("recovered missing key retries exact original input; changes conflict until explicit new request", async () => {
    const calls: string[] = []; const { c, api } = controller(calls);
    api.create = async (_p, b) => { calls.push("create:" + b.request_key); throw new SourceHTTPError(503, "source_unavailable"); };
    api.find = async (_p,k) => { calls.push("find:" + k); return null; };
    await expect(c.quote(input)).rejects.toThrow();
    await expect(c.quote({ ...input, prompt: "other" })).rejects.toThrow();
    await expect(c.quote(input)).rejects.toThrow();
    expect(calls).toEqual(["create:request-a", "find:request-a", "create:request-a"]);
  });
  it("new generation gets a new user-requested key", async () => {
    const calls: string[] = []; let n = 0; const c = new SourceImageController(sourceAPIFixture(calls), () => `request-${++n}`, () => 1700000001);
    c.setScope(scope); c.capabilities = capabilities(); await c.quote(input); c.newRequest(); await c.quote(input);
    expect(calls).toEqual(["create:request-1", "create:request-2"]);
  });
  it("account change hides facts and drops in-flight old-scope completion", async () => {
    const calls: string[] = []; const { c, api } = controller(calls);
    let finish!: (r: ReturnType<typeof quoted>) => void; api.get = () => new Promise(resolve => { finish = resolve; });
    const restoring = c.restore("proj_a", "sir_a");
    c.setScope({ ...scope, userID: "usr_b", accountID: "acct_b" }); finish(quoted());
    await expect(restoring).rejects.toThrow(); expect(c.view).toBeNull(); expect(c.pendingHint).toBeNull(); expect(c.capabilities).toBeNull();
  });
  it("lookup/detail/action cannot replace the requested original run or key",async()=>{
    const calls:string[]=[];const {c,api}=controller(calls);
    api.get=async()=>({...quoted(),run_id:"sir_other"});await expect(c.restore("proj_a","sir_a")).rejects.toThrow();expect(c.view).toBeNull();
    api.create=async()=>{throw new SourceHTTPError(503,"source_unavailable");};api.find=async()=>({...quoted(),request_key:"other-key"});
    await expect(c.quote(input)).rejects.toThrow();await expect(c.quote(input)).rejects.toThrow();expect(c.view).toBeNull();
    c.newRequest();api.create=async()=>quoted();await c.quote(input);api.confirm=async()=>({...quoted(),run_id:"sir_other",confirmed:true});
    await expect(c.confirm()).rejects.toThrow();expect(c.view?.run_id).toBe("sir_a");
  });
  it("saved hints are scope-specific and verify by GET; stale owner hint does nothing", async () => {
    const calls: string[] = []; const { c } = controller(calls);
    expect(pendingHintKey(scope)).not.toBe(pendingHintKey({ ...scope, userID: "usr_b" }));
    await c.restoreHint({ ...scope, userID: "usr_b", requestKey: "request-a" }); expect(calls).toEqual([]);
    await c.restoreHint({ ...scope, requestKey: "request-a" }); expect(calls).toEqual(["find:request-a"]);
  });
  it("decimal minor strings preserve int64 precision and unknown is never zero", () => {
    expect(formatMinor("25")).toBe("¥0.25"); expect(formatMinor("9007199254740993")).toBe("¥90071992547409.93");
    expect(formatMinor("9223372036854775807")).toBe("¥92233720368547758.07");
    for (const bad of ["", "00", "-1", "2.5", "1e2", "9223372036854775808"]) expect(formatMinor(bad)).toBe("金额待核实");
    expect(formatMinor(null)).toBe("金额待核实");
  });
  it("wire projection closes private fields and rejects numeric money and contradictory output", () => {
    const raw = { ...quoted(), signed_url: "https://private.invalid/secret", token: "secret" };
    expect(JSON.stringify(decodeSourceRun(raw))).not.toContain("secret");
    expect(() => decodeSourceRun({ ...quoted(), quote: { ...quoted().quote!, amount_minor: 25 } })).toThrow();
    expect(() => decodeSourceRun({ ...quoted(), deleted: true, output: { asset_id: "asset_a" } })).toThrow();
  });
  it("formal ingested/reconciling money facts remain readable and unknown", () => {
    for(const status of ["ingested","reconciling"]) {
      const r=quoted(); r.payment.status=status; r.quote=null;
      expect(decodeSourceRun(r).payment.status).toBe(status);expect(canConfirm(r,1700000001)).toBe(false);expect(formatMinor(r.payment.charged_minor)).toBe("金额待核实");
    }
  });
  it("actual output_deleted phase remains readable after the explicit tombstone action",()=>{
    const r=quoted();r.deleted=true;r.phase="output_deleted";expect(decodeSourceRun(r).deleted).toBe(true);expect(requestState(r)).toContain("引用清理待核实");
  });
  it("platform task-source reference ids (with colons) decode in plate_task and output", () => {
    const ref = "task-source:v1:tsk_723bc44347877c91f8c26fcc:output";
    const plate = plateDone();
    plate.plate!.plate_task = { task_id: "task_a", asset_id: "asset_a", reference_id: ref, sha256: "a".repeat(64) };
    expect(decodeSourceRun(plate).plate!.plate_task?.reference_id).toBe(ref);
    const text = quoted();
    text.output = { asset_id: "asset_a", reference_id: ref, sha256: "a".repeat(64), content_type: "image/png", size_bytes: "52341", width_px: 1024, height_px: 1024, association_verified: true };
    expect(decodeSourceRun(text).output?.reference_id).toBe(ref);
  });
  it("a reloaded key hint cannot reconstruct an absent original tuple into a POST", async () => {
    const calls:string[]=[]; const {c,api}=controller(calls);
    api.find=async(_p,k)=>{calls.push("find:"+k);return null;};
    await c.restoreHint({...scope,requestKey:"request-a"});
    await expect(c.quote(input)).rejects.toThrow();expect(calls).toEqual(["find:request-a","find:request-a"]);
  });
  it("funds unknown does not become free and refund retains original charge", () => {
    expect(requestState({ ...quoted(), phase: "asset_pending", payment: { ...quoted().payment, status: "unknown" } })).toContain("素材保存中");
    const r = quoted(); r.phase = "succeeded"; r.payment.status = "refunded"; r.payment.original_charged_minor = "25"; r.payment.original_charge_id = "charge_a"; r.payment.refund_id = "refund_a";
    expect(requestState(r)).toContain("退款事实"); expect(formatMinor(r.payment.refund_amount_minor)).toBe("金额待核实");
  });
});
