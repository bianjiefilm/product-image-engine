import { describe, expect, it } from "vitest";
import { canSendCampaignReceipt } from "../src/lib/campaign-restore";

describe("canSendCampaignReceipt", () => {
  it("订单和独立工程不要求活动选定", () => {
    expect(canSendCampaignReceipt("order", null, "out-1")).toBe(true);
    expect(canSendCampaignReceipt("standalone", undefined, "out-1")).toBe(true);
    expect(canSendCampaignReceipt("", null, "out-1")).toBe(true);
  });

  it("活动工程只有明确选定的那一版可以回传", () => {
    expect(canSendCampaignReceipt("campaign", null, "out-1")).toBe(false);
    expect(canSendCampaignReceipt("campaign", "", "out-1")).toBe(false);
    expect(canSendCampaignReceipt("campaign", "out-1", "")).toBe(false);
    expect(canSendCampaignReceipt("campaign", "out-2", "out-1")).toBe(false);
    expect(canSendCampaignReceipt("campaign", "out-1", "out-1")).toBe(true);
  });
});
