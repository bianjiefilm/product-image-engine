# HUI-2627 finish-r1 证据包(2026-10-08,接棒段收口)

Ticket: HUI-2627【UI-R5 I｜Product Image Product Finish P0】。分支 `codex/20261008-hui2627-finish-r1`(基线 origin/main 8b231ac)。
**证据层级:本目录全部为 `fixture` 层**(e2e/fixture-api.mjs 替身 Go API + `next start` + 真实 Chrome,零新增扣费,不冒充真实栈验收)。

## 做了(与票面六项收口范围对应)

1. **回归复核**:前任静态精读(d2e0180/a0e9368/c798692)经接棒抽验沿用;动态断言由 `web/e2e/hui-2627-finish-r1.mjs` 在 fixture 栈执行兑现。四条 PASS 保持、零回归 → `regression-review.md`(含接棒增补段)。
2. **成品红线四条**:渲染层逐面断言通过(八面 × `redline-*-1440.json` + `walkthrough-record.json` 0 fail):
   - 无 table/form 主视觉(start/runview 主视觉为 figure/大图;start 页 0 table);
   - primary action 不藏表单底部(`login-primary-action.json`:登录主按钮位于首个 input 之上);
   - 默认视图无 provider/qwen/run_id/debug/FEATURE_/fixture 字样、`details[data-technical]` 全部闭合;
   - 不支持局部编辑时降级文案为成品化表述(`flag-off-copy.txt`「…还没有打开。不会假装已经改好」,无内部 flag 名)。
3. **状态覆盖**(八代表页适用态,全部真实触发+截图):
   loading(`state-loading`)/ empty(`state-empty-projects|batches`,有「新建工程/开始做产品图」恢复入口)/ partial(`state-partial-batches`,部分失败+计数)/ success(`runview-success` + figcaption 三 chip)/ error(`state-error-recovery.json`:「这个工程没有打开」+ 返回工程列表/开始做产品图;`state-not-usable`:未通过商品检查诚实文案)/ disabled(`state-disabled`)/ offline(`state-offline`:「网络不可用…请稍后重试」+ 重试按钮)/ revision_off(`flag-off-copy.txt`,fixture 404=真实栈 flag 关闭同形)。
4. **事实诚实性**:实点「送数字人」,状态句原文「只返回了 V1 的本地引用,没有上传文件。数字人、AiCut 和矩阵都还没收到。」(`downstream-copy.txt`);fixture 契约测试断言 downstream 永无「已送出/已上传/uploaded/delivered/sent_at」(`web/tests/fixture-api.test.ts`)。
5. **HUI-2625 门证据包**:40 张矩阵(八代表页 × 1920/1440/1024/430/390,`finish-matrix-*.png`,逐张无横向溢出)+ 390 并排比较单栏断言(`compare-390.json`)+ axe-core 八面@1440 + 七面@390 全部 0 serious/critical(`axe-results.jsonl`,15 行;login 无 390 腿)+ 去 Logo 品牌族材料 15 张(`finish-blind-*`,390 腿为真实 390 视口;选择器 `[data-eco-topnav] [class*="appName"]` visibility:hidden)。
6. **HUI-2596 交互不动**:采用 V2 → 回到 V1 往返实测通过(`revision-rail.json`、`adopted-v2` 截图);仅补 label htmlFor 关联(axe label critical 修复),不改交互。

## 清零扫描

`hygiene-scan.json` + `web/tests/finish-hygiene.test.ts`:裸 hex 全 src = **0**;inline style(72)/raw button(45)/raw input(53)/raw table(14)为 2026-10-08 记账基线,测试断言**不得增长**(整面重写会触碰 HUI-2596 交互,超出本轮 P0;「无 table/form 主视觉」红线由渲染层驱动实测把关)。page-census markerBook 记账口径保持稳定。

## 测试账

- `npx vitest run --pool=forks`:41 文件 / 333 用例全过(基线 37/316 → 前任 38/320 → 接棒 +fixture revision_off、+ui-error-banner 2、+revision label 1、+finish-hygiene 2)。
- 驱动(含视口修复后)两连跑均 exit 0 / 0 fail / 25 ok(可复跑);本目录证据取终版 run。修复明细:axe/blind 的 390 腿曾误用 1440 视口(截图尺寸实锤),已在驱动内切视口修复并复跑。

## 证据四层

| 层 | 内容 |
|---|---|
| engineering | vitest 41/334 全绿;hygiene 清零;fixture 契约测试(诚实字段) |
| browser | 本目录 68 PNG + 16 axe 行 + walkthrough-record.json(fixture 层,真实 Chrome) |
| real-provider | **NOT_RUN**(零扣费红线,不配置真实供应商;复用 10-04 真实栈结论,见下) |
| cost | 新增开支 0;全部走 fixture/冻结样本 |

10-04 真实栈验收(readyz 8f3dd4c,四条 PASS,零新增扣费复用已付费 run)按「复用」引用,不冒充本次复走。

## 没做 / NOT_RUN 账目

- 真实栈(QA 120.26.69.202)复走 **NOT_RUN**:该栈 SSH 客户端配置在本机不存在,历史存储态亦无(前任 D1,接棒核实沿用)。
- 真实供应商(modelxing 等)**NOT_RUN**:零扣费红线,一律 fixture/冻结样本。
- HUI-2625 成品门 **未过、未裁决**:本包为输入件;**不标 Done**。
- 送数字人/AiCut/矩阵:只返回本地引用,对方**还没收到文件**(诚实账目,非本轮可解)。
- raw control/inline style 整面清零 **未做**(见上,基线防回潮)。
- HUI-2232 未关;未部署。

## 复跑方式

```
cd web && npx next build
E2E_OUT=/tmp/out FIXTURE_SAMPLES_DIR=../fixtures/frozen-samples/v1 \
  node e2e/hui-2627-finish-r1.mjs
```

端口默认 web 32321 / fixture-api 32324(可 E2E_WEB_PORT/FIXTURE_API_PORT 覆盖)。
