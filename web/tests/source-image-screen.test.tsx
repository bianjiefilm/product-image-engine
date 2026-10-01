import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, it, expect } from "vitest";
import { SourceImageScreen } from "@/components/source-image/Panel";
import { quoted, capabilities } from "./source-image-fixture";
describe("source screen honest quote/result", () => {
  it("known quote exposes separate explicit payment with payer/model/count/size/expiry", () => {
    const html = renderToStaticMarkup(createElement(SourceImageScreen, { view: quoted(), capabilities: capabilities(), nowUnix: 1700000001, projectID: "proj_a" }));
    expect(html).toContain("获取报价"); expect(html).toContain("确认支付并生成"); expect(html).toContain("¥0.25"); expect(html).toContain("个人账户"); expect(html).toContain("qwen-image-2.0"); expect(html).toContain("1024×1024"); expect(html).toContain("1 张"); expect(html).toContain("到期");
    expect(html).toContain("普通概念图，不宣称商品主体保真"); expect(html).toContain("自动结果恢复尚未配置"); expect(html).not.toContain("保真通过");
  });
  it("unknown/expired/denied quote has no payment CTA and no fake free amount", () => {
    const noQuote = { ...quoted(), quote: null }; const expired = quoted(); expired.quote!.expires_at_unix = 1700000001;
    const denied = capabilities(); denied.modes[0].can_confirm = false;
    for (const [view, caps] of [[noQuote, capabilities()], [expired, capabilities()], [quoted(), denied]] as const) {
      const html = renderToStaticMarkup(createElement(SourceImageScreen, { view, capabilities: caps, nowUnix: 1700000001, projectID: "proj_a" }));
      expect(html).not.toContain("确认支付并生成"); expect(html).not.toContain("免费");
    }
  });
  it("refund/deletion keep original expense and unknown cleanup and human facts", () => {
    const r = quoted(); r.deleted = true; r.payment.status = "refunded"; r.payment.original_charged_minor = "25"; r.payment.original_charge_id = "charge_a"; r.payment.refund_id = "refund_a";
    const html = renderToStaticMarkup(createElement(SourceImageScreen, { view: r, capabilities: capabilities(), nowUnix: 1700000001, projectID: "proj_a" }));
    expect(html).toContain("原扣款 ¥0.25"); expect(html).toContain("charge_a"); expect(html).toContain("refund_a"); expect(html).toContain("退款金额待核实"); expect(html).toContain("引用清理待核实"); expect(html).toContain("NOT_RUN"); expect(html).not.toContain("已退款 ¥0.00"); expect(html).not.toContain("/content");
  });
  it("confirmed output links remain protected source paths; quality unknown", () => {
    const r = quoted(); r.confirmed = true; r.phase = "succeeded"; r.payment.status = "charged"; r.payment.charged_minor = "25"; r.output = { asset_id: "asset_a", reference_id: "ref_a", sha256: "b".repeat(64), content_type: "image/png", size_bytes: "1024", width_px: 1024, height_px: 1024, association_verified: true };
    const html = renderToStaticMarkup(createElement(SourceImageScreen, { view: r, capabilities: capabilities(), nowUnix: 1700000001, projectID: "proj_a" }));
    expect(html).toContain("/api/projects/proj_a/source-image-runs/sir_a/content"); expect(html).toContain("/api/projects/proj_a/source-image-runs/sir_a/export"); expect(html).toContain("视觉质量待核实"); expect(html).not.toContain("确认支付并生成");
  });
  it("known refunded output never offers reuse/export or a second payment",()=>{
    const r=quoted();r.phase="succeeded";r.confirmed=true;r.payment.status="refunded";r.payment.refund_id="refund_a";
    r.output={asset_id:"asset_a",reference_id:"ref_a",sha256:"b".repeat(64),content_type:"image/png",size_bytes:"1024",width_px:1024,height_px:1024,association_verified:true};
    const html=renderToStaticMarkup(createElement(SourceImageScreen,{view:r,capabilities:capabilities(),nowUnix:1700000001,projectID:"proj_a"}));
    expect(html).not.toContain("/content");expect(html).not.toContain("/export");expect(html).not.toContain("选择原输出");expect(html).not.toContain("确认支付并生成");expect(html).toContain("退款");
  });
});
