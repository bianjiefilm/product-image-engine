// 费用只展示服务端返回的整数分。这里不做加法。
export function displayCostCents(serverCents: number): number {
  if (!Number.isInteger(serverCents) || serverCents < 0) {
    throw new Error("费用必须使用服务端返回的整数分");
  }
  return serverCents;
}
