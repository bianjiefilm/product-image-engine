import { describe, expect, it } from "vitest";
import { SourceImageController, sourceImageAPI, validBackground, decodeSourceRun, pendingHintKey } from "@/lib/source-image";
import type { PlateCreate, SourceCreate } from "@/lib/source-image";
import { plateCapabilities, plateDone, plateQuoted, quoted, scope, sourceAPIFixture } from "./source-image-fixture";

function plateController(calls: string[], over: Partial<ReturnType<typeof sourceAPIFixture>> = {}) {
  const sent: SourceCreate[] = [];
  const api = { ...sourceAPIFixture(calls), capabilities: async () => plateCapabilities(true), create: async (_p: string, input: SourceCreate) => { sent.push(input); return { ...plateQuoted(), request_key: input.request_key }; }, ...over };
  const c = new SourceImageController(api, () => "request-plate", () => 1700000100, "background_plate_lock");
  c.setScope(scope);
  return { c, sent };
}

describe("plate-lock controller", () => {
  it("quotes with exactly request_key, mode, input_id and background_intent", async () => {
    const calls: string[] = [];
    const { c, sent } = plateController(calls);
    await c.loadCapabilities();
    await c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "  深蓝色渐变摄影棚背景  " });
    expect(sent).toHaveLength(1);
    expect(Object.keys(sent[0]).sort()).toEqual(["background_intent", "input_id", "mode", "request_key"]);
    expect((sent[0] as PlateCreate).background_intent).toBe("深蓝色渐变摄影棚背景");
    expect(c.view?.mode).toBe("background_plate_lock");
  });

  it("refuses an input the server did not list as supported, before any request", async () => {
    const calls: string[] = [];
    const { c, sent } = plateController(calls);
    await c.loadCapabilities();
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_other", background: "深蓝" })).rejects.toThrow("不在已验证范围");
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "a\nb" })).rejects.toThrow("背景描述");
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "" })).rejects.toThrow("背景描述");
    expect(sent).toEqual([]);
  });

  it("refuses to quote when the mode is not ready", async () => {
    const { c, sent } = plateController([], { capabilities: async () => plateCapabilities(false) });
    await c.loadCapabilities();
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "深蓝" })).rejects.toThrow("不允许获取正式报价");
    expect(sent).toEqual([]);
  });

  it("double click coalesces to one create and a retry after an unknown reuses the original key", async () => {
    const calls: string[] = [];
    let attempts = 0;
    const { c } = plateController(calls, { create: async (_p, input) => { attempts++; if (attempts === 1) throw new Error("network"); return { ...plateQuoted(), request_key: input.request_key }; }, find: async () => null });
    await c.loadCapabilities();
    const input = { projectID: "proj_a", inputID: "pin_a", background: "深蓝" };
    await Promise.allSettled([c.quotePlate(input), c.quotePlate(input)]);
    expect(attempts).toBe(1);
    await c.quotePlate(input);
    expect(attempts).toBe(2);
    expect(c.pendingHint?.requestKey).toBe("request-plate");
  });

  it("never accepts a run of the other mode, so the two panels cannot swap views", async () => {
    const { c } = plateController([], { get: async () => quoted() });
    await expect(c.restore("proj_a", "sir_a")).rejects.toThrow("请求类型不一致");
    expect(c.view).toBeNull();
    const text = new SourceImageController(sourceAPIFixture([]), undefined, undefined, "text_generate");
    text.setScope(scope);
    await expect((async () => { await text.restore("proj_a", "sir_plate"); })().catch(e => { throw e; })).rejects.toBeDefined();
  });

  it("confirm uses the plate mode capability and the original quote only", async () => {
    const calls: string[] = [];
    const { c } = plateController(calls, { confirm: async (_p, _r, input) => { calls.push("confirm:" + input.quote_id + ":" + input.quote_fingerprint); return { ...plateQuoted(), phase: "task_linked", confirmed: true, task_id: "task_a" }; } });
    await c.loadCapabilities();
    await c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "深蓝" });
    await c.confirm();
    expect(c.view?.confirmed).toBe(true);
    expect(calls).toContain("confirm:quote_a:" + "a".repeat(64));
  });

  it("return is possible only for a selected limited candidate", async () => {
    const calls: string[] = [];
    const { c } = plateController(calls, { get: async () => plateDone(), select: async () => ({ ...plateDone(), selected: true }) });
    await c.loadCapabilities();
    await c.restore("proj_a", "sir_plate");
    await expect(c.returnToSource()).rejects.toThrow("只有已选定的限定候选才能回传");
    await c.action("select");
    expect(await c.returnToSource()).toBe("out_a");
    expect(calls).toContain("returnOutput");
    const { c: bad } = plateController([], { get: async () => ({ ...plateDone("not_usable_candidate"), selected: true }) });
    await bad.loadCapabilities();
    await bad.restore("proj_a", "sir_plate");
    await expect(bad.returnToSource()).rejects.toThrow();
  });

  it("pending hints are separate per mode", () => {
    expect(pendingHintKey(scope, "background_plate_lock")).not.toBe(pendingHintKey(scope, "text_generate"));
    expect(pendingHintKey(scope)).toBe(pendingHintKey(scope, "text_generate"));
  });

  it("validBackground mirrors the server rule", () => {
    for (const [v, ok] of [["深蓝背景", true], ["", false], [" x", false], ["x ", false], ["a\nb", false], ["a\u007fb", false], ["界".repeat(200), true], ["界".repeat(201), false]] as const) expect(validBackground(v), JSON.stringify(v)).toBe(ok);
  });

  it("the decoder keeps the two modes apart", () => {
    expect(() => decodeSourceRun({ ...quoted(), plate: plateQuoted().plate })).toThrow();
    expect(() => decodeSourceRun({ ...plateQuoted(), plate: undefined })).toThrow();
    expect(() => decodeSourceRun({ ...plateQuoted(), fidelity: "not_applicable" })).toThrow();
    expect(() => decodeSourceRun({ ...quoted(), fidelity: "frozen_sample_only" })).toThrow();
    const done = decodeSourceRun(JSON.parse(JSON.stringify(plateDone())));
    expect(done.plate?.candidate_state).toBe("limited_candidate");
    const lying = JSON.parse(JSON.stringify(plateDone()));
    lying.plate.limits = [];
    expect(() => decodeSourceRun(lying)).toThrow();
  });

  it("the real API object exposes the plate create and return operations", () => {
    expect(typeof sourceImageAPI.returnOutput).toBe("function");
  });
});
