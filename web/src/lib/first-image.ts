export const PERSONAL_SCOPE = "personal/default";

export function loggedInHome(): "/start" {
  return "/start";
}

export function firstScreen(): { title: string; showsCatalog: false; steps: string[] } {
  return {
    title: "开始做产品图",
    showsCatalog: false,
    steps: ["上传或选择商品照片", "选择使用场景和尺寸", "背景方向", "费用确认", "生成"],
  };
}

const STATUS_LABEL: Record<string, string> = {
  queued: "排队中",
  running: "生成中",
  unknown: "结果未知",
  failed: "未能生成",
  completed: "已返回候选",
};

export function statusLabel(status: string): string {
  return STATUS_LABEL[status] ?? "尚未生成";
}

export function presentResult(input: {
  status: string;
  outputAssetId?: string;
  billingConnected: boolean;
  quoteLabel?: string;
}): {
  showImage: boolean;
  quoteLabel: string;
  charged: false;
  usefulProven: false;
  fidelityLabel: "待确认";
  headline: string;
} {
  const asset = input.outputAssetId?.trim() ?? "";
  const showImage = input.status === "completed" && asset !== "";
  const quoteLabel = input.billingConnected && input.quoteLabel?.trim() ? input.quoteLabel.trim() : "待确认";
  return {
    showImage,
    quoteLabel,
    charged: false,
    usefulProven: false,
    fidelityLabel: "待确认",
    headline: showImage ? "候选已返回，真实成片质量仍未证明" : "真实成片仍未生成",
  };
}

export function resumeTask(currentId: string, previousId: string): boolean {
  return currentId !== "" && currentId === previousId;
}
