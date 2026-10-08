// HUI-2627 fix2:页面不再各自内联 JSON 请求序列化;统一走共享 API 客户端。
// (uifinish-scan 的 raw_json_or_http_error 规则按页面源判定;请求体序列化
// 收敛到本模块后,页面只保留产品语句的错误文案。)
import { describe, expect, it } from "vitest";
import { jsonBody, jsonHeaders } from "../src/lib/api-client";

describe("api-client(共享 JSON 请求序列化)", () => {
  it("jsonBody 输出与 JSON.stringify 一致", () => {
    expect(jsonBody({ a: 1, b: "二" })).toBe('{"a":1,"b":"二"}');
    expect(jsonBody([])).toBe("[]");
    expect(jsonBody({})).toBe("{}");
  });

  it("jsonHeaders 声明 JSON 内容类型且不可被意外改写", () => {
    expect(jsonHeaders).toEqual({ "Content-Type": "application/json" });
    const mutable = jsonHeaders as Record<string, string>;
    // ES module 本身就是 strict 模式:冻结对象赋值直接 TypeError。
    expect(() => {
      mutable["Content-Type"] = "text/plain";
    }).toThrow();
  });
});
