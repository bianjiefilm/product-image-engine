// 活动回执门：只有明确选定的那一版可以回传。
// 订单和独立工程不走这道闸门。打开页面、下载和登记成果都不是选定。

export function canSendCampaignReceipt(
  sourceKind: string | null | undefined,
  selectedOutputId: string | null | undefined,
  outputId: string,
): boolean {
  if (sourceKind !== "campaign") {
    return true;
  }
  return selectedOutputId != null && selectedOutputId !== "" && selectedOutputId === outputId;
}
