import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PlateLockScreen } from "@/components/plate-lock/PlateLockScreen";
import type { PlateScreenProps } from "@/components/plate-lock/PlateLockScreen";
import { TECHNICAL_ONLY_TERMS } from "@/lib/plate-lock";
import { plateCapabilities, plateDone, plateQuoted } from "./source-image-fixture";

const base: PlateScreenProps = { view: null, capabilities: plateCapabilities(true), nowUnix: 1700000100, projectID: "proj_a", background: "" };
const html = (p: Partial<PlateScreenProps>) => renderToStaticMarkup(createElement(PlateLockScreen, { ...base, ...p }));
// The technical details fold is the only place internal words may appear.
const primary = (markup: string) => markup.replace(/<details data-technical="true">[\s\S]*?<\/details>/g, "");
// What a person reads: tags and attributes (class names, image URLs) are not text.
const visible = (markup: string) => primary(markup).replace(/<[^>]*>/g, " ");

describe("limited-fidelity screen states", () => {
  it("loading state shows a polite status and skeletons, no form", () => {
    const m = html({ loading: true, capabilities: null });
    expect(m).toContain('aria-busy="true"');
    expect(m).toContain("正在读取");
    expect(m).not.toContain("<form");
  });

  it("disabled state names the reason in plain words and offers no paid action", () => {
    for (const [reason, text] of [["plate_lock_disabled", "暂未开放"], ["sample_set_invalid", "没有通过校验"], ["plate_org_not_supported", "只支持个人付款"], ["project_write_forbidden", "不能修改这个工程"]] as const) {
      const caps = plateCapabilities(false);
      caps.modes = caps.modes.map(m => (m.mode === "background_plate_lock" ? { ...m, reason } : m));
      const m = html({ capabilities: caps });
      expect(m).toContain(text);
      expect(m).toContain('data-ready="false"');
      expect(m).not.toContain("获取报价");
      expect(m).not.toContain("确认并生成");
    }
  });

  it("ready but no photo inside the verified range explains the limit and creates no form", () => {
    const caps = plateCapabilities(true);
    caps.modes = caps.modes.map(m => (m.mode === "background_plate_lock" ? { ...m, supported_input_ids: [], supported_inputs: [] } : m));
    const m = html({ capabilities: caps });
    expect(m).toContain("冻结样本");
    expect(m).toContain("不会产生费用");
    expect(m).not.toContain("获取报价");
  });

  it("empty ready state is a labelled form with the pricing promise and a disabled quote button", () => {
    const m = html({});
    expect(m).toContain('for="plate-background"');
    expect(m).toContain('id="plate-background"');
    expect(m).toContain("不会自动重做，也不会再次扣费");
    expect(m).toMatch(/<button[^>]*disabled=""[^>]*>获取报价/);
    expect(html({ background: "深蓝色背景" })).not.toMatch(/<button[^>]*disabled=""[^>]*>获取报价/);
  });

  it("quoted state shows the amount, payer and one confirm button, with the honest cost sentence", () => {
    const m = html({ view: plateQuoted() });
    expect(m).toContain("¥0.25");
    expect(m).toContain("个人账户");
    expect(m).toContain("确认并生成 ¥0.25");
    expect(m).toContain("不会自动重做，也不会再次扣费");
    expect(m).not.toContain("plate-select");
  });

  it("an expired quote cannot be confirmed", () => {
    expect(html({ view: plateQuoted(), nowUnix: 1700000300 })).not.toContain("确认并生成");
  });

  it("pending derivation keeps the user informed and offers no selection", () => {
    const view = { ...plateQuoted(), confirmed: true, phase: "task_linked" };
    const m = html({ view });
    expect(m).toContain("正在生成背景并检查商品");
    expect(m).not.toContain("plate-select");
  });

  it("a limited candidate shows per-axis results in four plain states, the limits, and enables select and export", () => {
    const m = primary(html({ view: plateDone() }));
    expect(m).toContain('data-candidate="limited_candidate"');
    expect(m).toContain("通过商品检查（限定范围）");
    expect(m).toContain("商品像素与原图完全一致");
    expect(m).toContain(">通过<");
    expect(m).toContain(">待核实<");
    expect(m).toContain(">不适用<");
    expect(m).toContain("使用限制");
    expect(m).toContain("尚未经过真人评审");
    expect(m).toMatch(/<button[^>]*data-testid="plate-select"[^>]*>选定这张/);
    expect(m).not.toMatch(/<button[^>]*disabled=""[^>]*data-testid="plate-select"/);
    expect(m).toContain('data-testid="plate-export"');
    expect(m).toContain('src="/api/projects/proj_a/source-image-runs/sir_plate/content"');
  });

  it("an unusable candidate is an alert naming the failed check, and cannot be selected, exported or returned", () => {
    const m = primary(html({ view: plateDone("not_usable_candidate"), returnEnabled: true, returnAck: true }));
    expect(m).toContain('role="alert"');
    expect(m).toContain("未通过商品检查");
    expect(m).toContain("背景相比原图有明显变化");
    expect(m).toContain("未通过");
    expect(m).toMatch(/<button[^>]*disabled=""[^>]*data-testid="plate-select"/);
    expect(m).not.toContain('data-testid="plate-export"');
    expect(m).not.toContain('data-testid="plate-return"');
    expect(m).toContain("不可使用");
  });

  it("return needs an explicit acknowledgement that the receipt omits the limits", () => {
    const selected = { ...plateDone(), selected: true };
    const off = primary(html({ view: selected, returnEnabled: true, returnAck: false }));
    expect(off).toContain("下游收到的回执不含上述限制说明");
    expect(off).toMatch(/<button[^>]*disabled=""[^>]*>回传到来源/);
    const on = primary(html({ view: selected, returnEnabled: true, returnAck: true }));
    expect(on).not.toMatch(/<button[^>]*disabled=""[^>]*>回传到来源/);
    expect(primary(html({ view: plateDone(), returnEnabled: false }))).not.toContain("回传到来源");
  });

  it("the primary view never shows an internal word; the technical fold carries them", () => {
    for (const view of [plateQuoted(), plateDone(), plateDone("not_usable_candidate")]) {
      const m = visible(html({ view, returnEnabled: true }));
      for (const term of TECHNICAL_ONLY_TERMS) expect(m, `${term} leaked into the primary view`).not.toContain(term);
      expect(m).not.toContain("sir_plate");
      expect(m).not.toContain("task_a");
      expect(m).not.toMatch(/[0-9a-f]{40,}/);
    }
    const full = html({ view: plateDone() });
    expect(full).toContain('data-technical="true"');
    expect(full).toContain("sir_plate");
    expect(full).toContain("合成结果 hash");
    expect(full).toContain("c".repeat(64));
  });

  it("the capability matrix tells the truth about every mode, and the closed ones are not clickable", () => {
    const m = html({});
    expect(m).toContain('data-mode="background_plate_lock" data-state="available"');
    for (const id of ["creative_background", "light_scene", "showcase_video", "reference_edit", "first_image"]) expect(m).toContain(`data-mode="${id}" data-state="not_opened"`);
    expect(m).toContain("未开放：还没有真实成功证据");
    expect(m).toContain('data-mode="text_generate" data-state="unavailable"');
    const caps = plateCapabilities(true);
    caps.modes = caps.modes.map(x => (x.mode === "text_generate" ? { ...x, ready: true, disabled: false } : x));
    expect(html({ capabilities: caps })).toContain("不保证商品一致");
    const section = m.slice(m.indexOf('data-testid="capability-matrix"'));
    expect(section).not.toContain("<button");
  });

  it("before any project exists the matrix does not pretend to know", () => {
    const m = html({ capabilities: null, projectID: "" });
    expect(m).toContain('data-mode="background_plate_lock" data-state="unknown"');
    expect(m).toContain("选好照片后显示");
  });

  it("every control has a visible label or text, and a polite live region exists", () => {
    const m = html({ view: plateDone(), returnEnabled: true });
    expect(m).toContain('aria-live="polite"');
    expect(m).toContain('aria-label="制作步骤"');
    expect(m).toContain('aria-current="step"');
    expect(m).not.toMatch(/<img(?![^>]*alt=)/);
  });

  it("a notice is announced as an alert", () => {
    expect(html({ notice: "这张照片不在已验证范围内" })).toMatch(/role="alert"[^>]*data-testid="plate-notice"|data-testid="plate-notice"[^>]*role="alert"/);
  });
});
