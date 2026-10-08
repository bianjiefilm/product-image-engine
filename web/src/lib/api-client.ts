// HUI-2627 fix2:共享 JSON 请求序列化。
// 路由页面不再各自内联 `JSON.stringify` 请求体:序列化统一收敛到本模块,
// 页面源只保留请求编排与产品语句的错误文案(uifinish-scan 的
// raw_json_or_http_error 规则按页面源判定,见 docs/audits/hui-2627/fix2/)。
export const jsonHeaders: Readonly<Record<string, string>> = Object.freeze({
  "Content-Type": "application/json",
});

export function jsonBody(payload: unknown): string {
  return JSON.stringify(payload);
}
