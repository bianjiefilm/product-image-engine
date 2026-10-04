# HUI-2596 锁定商品，只改这里

指挥已拍板。本文只记录已定设计，不另起视觉壳，不把 HUI-2232 写成完成。

## 目标

产品图可以锁定商品主体、Logo、包装文字，只改背景或光影；原结果保留；V1/V2 并排；可回退；增量费用是一个可复算的整数分；导出和下游引用绑定明确版本。

## 不做

- 不新增视觉组件，不引入 shadcn。产品壳属于 HUI-2627。
- 不重写生成引擎。主体像素锁回复用已在 main 的 `bgreplace.LockSubject`。
- 没有供应商密钥时不调用真模型。真实供应商局部编辑仍是 UNKNOWN。
- 前端不实现第二套费用加法。

## RevisionIntent

自然语言和可点击动作生成同一个 `RevisionIntent`（`product-revision-intent/v1`）。比较时忽略 `source` 与原文 `text`。

| 说法 | 点击 action | 区域 | 下游 |
|---|---|---|---|
| 背景简单一点 | `simplify_background` | background | |
| 换成户外 | `outdoor` | background | |
| 保留商品，只调亮 | `brighten_keep_product` | light | |
| 回到 Vn | `rollback` + `target_version=Vn` | none | |
| 采用这个版本 | `adopt` | none | |
| 送数字人 / 送AiCut / 送矩阵 | `send_downstream` | none | `digital_human` / `aicut` / `matrix` |

上述修改动作 `preserve_subject=true`，锁 `product`、`logo`、`packaging_text`。认不出的话术是 `unrecognized`，不得执行，也不得整图重画。

## 执行

`preserve_subject=true` 时拒绝静默整图重画。像素修改只做：确定性底板 → `LockSubject` → `SubjectPixelChecks`。供应商全图重画、主体重采样、Logo 重生成、包装文字重生成、视频转码、矩阵发布、新上传都算无关昂贵步骤，局部修改不得进入这些步骤。

本构建没有局部编辑供应商。`Provider.PartialEdit=false` 时，执行前必须返回影响范围说明；未确认则不落版本、不改像素。确认后仍只锁回主体，不调用供应商。`PartialEdit=true` 也只说明“真实供应商仍是 UNKNOWN”，同样不调用供应商。

确定性底板颜色：简化背景 `220,220,220`；户外 `70,140,210`；调亮 `255,244,220`。来源标记 `subject_lock`。这不是模型出图。

## 费用

唯一加法是 `IncrementalCostCents(intent) int64`，单位是分：

- 背景局部：40
- 光影局部：20
- 回退、采用、下游引用：0
- `preserve_subject=false` 或认不出：拒绝，不计价

同一意图两次调用结果相同。前端只展示服务端整数。

## 版本

版本号 V1、V2… 行不可变。采用指针单独保存。

- `failed` / `unknown` 追加记录，不改已采用版本的指针和内容哈希。
- 新 Brief 只产生差异（新增、删除、变更的键），不改已采用结果。
- 并排比较只读两版。回退是把采用指针移回仍在册、且有像素的版本。
- 导出和下游必须点名版本。失败或未知版本不可导出。`requires_reupload=false`，引用形如 `revision-version/V2@<hash前12位>`，不上传文件。

## HTTP

`FEATURE_SELECTIVE_REVISION` 默认关闭。关闭时路由不注册（404）。开启后：

- `POST /api/v1/projects/{id}/revisions/intent` 文本或点击 → 同一意图 + 整数分
- `POST /api/v1/projects/{id}/revisions/plan` 只计划，不执行
- `POST /api/v1/projects/{id}/revisions` 执行局部修改、回退、采用或下游引用
- `POST /api/v1/projects/{id}/revisions/outcome` 记录 failed/unknown
- `GET` 列表与 `compare`，`POST .../{label}/adopt`，`POST .../rollback`，`POST .../briefs`，`POST .../{label}/export`，`POST .../{label}/downstream`，`GET .../{label}/content`

Web 只转发请求，并用 `displayCostCents` 原样返回服务端整数。不新建界面组件。

## 验收对照

- 确定性底板上，蒙版内像素与原图一致。
- 至少一次局部修改的步骤记录不含昂贵步骤，且全图重画钩子调用次数为 0。
- 重启数据库后并排、锁定、回退仍指向同一哈希。
- 费用函数可复算。
- 下游引用带版本且 `requires_reupload=false`。
- 真实供应商：UNKNOWN。HUI-2232 不在本票宣称完成。
