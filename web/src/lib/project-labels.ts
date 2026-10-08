// HUI-2627 fix2:工程状态/来源的中文词汇(2625 gate-r2 盲评 fail 项)。
// 后端枚举(draft/active/archived、standalone/order/campaign)不再裸渲染,
// 统一在这里给出与新建工程表单、批次「部分失败」徽章同族的中文文案。
const STATUS_LABEL: Record<string, string> = {
  draft: "草稿",
  active: "进行中",
  archived: "已归档",
};

const SOURCE_LABEL: Record<string, string> = {
  standalone: "独立制作",
  order: "挂接订单",
  campaign: "挂接活动",
};

export function projectStatusLabel(status: string): string {
  return STATUS_LABEL[status] ?? "状态未知";
}

export function projectSourceLabel(sourceType: string): string {
  return SOURCE_LABEL[sourceType] ?? (sourceType ? "来源未知" : "");
}
