# sizeadapt — 电商尺寸适配(HUI-1703 / FEAT-0204)

纯确定性图像处理包:把项目已有 outputs(合法 PNG ≤ 8 MiB)按预设变换为
各电商平台主图/详情页/广告尺寸。**不调用 AI 模型、不建任务、不扣费、
不触碰生成开关**(import 白名单由 `TestNoForbiddenImports` 钉死)。

## 变换模式

| mode | 语义 | 典型用途 |
|---|---|---|
| `pad` | 等比缩放 + 白底填充到目标画布(居中);透明通道与白底合成 | 商品主图 |
| `cover` | 等比缩放 + 居中裁切填满画布 | 广告位/封面 |

约定:`height=0`(仅限 `pad`)为 **fit-width** 哨兵——宽缩放到精确值,
高按源等比、不纵向填充(「详情页 790 宽等比」类规格)。

输出格式:`png`(默认)或 `jpeg`(quality 固定 90)。同一输入 + 同一预设 +
同一格式输出字节级相同(确定性),变体 sha256 可作幂等/溯源依据。

## 内嵌预设矩阵(presets.json,go:embed)

> **免责**:以下为公开常见规格整理,商家可自定义覆盖,以各平台当时官方要求为准。

| name | 尺寸 | mode |
|---|---|---|
| taobao_main | 800×800 | pad |
| tmall_main | 800×800 | pad |
| tmall_detail_w790 | 宽 790 等比(fit-width) | pad |
| jd_main | 800×800 | pad |
| pdd_main | 750×750 | pad |
| douyin_ad | 1080×1440 | cover |
| xhs_cover | 1080×1440 | cover |
| generic_square_1024 | 1024×1024 | pad |

## 自定义覆盖

预设 schema 固定为三元组 `{"name","width","height","mode"}`(+ 展示名 `label`):

```json
{"presets":[{"name":"my_main","width":800,"height":800,"mode":"pad","label":"我的主图"}]}
```

部署时以 env `PRODUCT_SIZE_PRESETS` 指向自定义 JSON 文件即可整体覆盖内嵌矩阵
(加载即校验:name 限 `[a-z0-9_]{2,64}`、1≤width≤16384、0≤height≤16384 且
height=0 仅限 pad、mode ∈ {pad,cover}、名称去重;非法即 fail-closed 拒绝)。

## 输入门(fail-closed)

- PNG 魔数 + `png.Decode` 成功,字节 ≤ 8 MiB(与 I1 outputs 域同口径);
- 解码后单边 ≤ 16384px 且总像素 ≤ 40M(防解压炸弹)。
