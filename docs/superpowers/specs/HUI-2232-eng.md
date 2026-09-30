# HUI-2232 工程走查：对着 HTTP 提供者核验出图门

日期：2026-09-30

总指挥已裁定。本规格只新增 `server/internal/engwalk`。它不修改 `qualitygate`、`textimage`、`bgreplace`、`fidelity`、`lightscene`、`config`。可以导入 `fidelity`、`textimage`、`config`。不新增图像模型 SDK，不改 `go.mod`。

合同路径必须真的发起 HTTP。把一张 PNG 写死在包里、不经过网络，不算这条路径。模拟图和未知结果都不是商业上的真实出图通过。人不采纳，不挡合同走查。

## 不做

- 不编辑既有包，不接入 `httpapi`，不写数据库，不打开浏览器，不购买任何东西。
- 不绑定 8080、18084、18101–18106。测试服务器只用 `httptest` 的端口 0。
- 不打印凭证或任何其他秘密。结果里不出现凭证原文。
- 不把 `TaskBaseURL`、`UploadBaseURL`、`BillingBaseURL` 当成图像模型地址。它们不是这三枚模型凭证的成对 URL。
- 不把合同通过写成 `真实出图完成`。`production_generation_passed` 与 `billing_passed` 保持 false。
- 不循环重试供应商，不对同一个提交打第二次模型请求。

## 六层

走查结果始终带这六层，顺序固定：

1. `task` — 受支持模式发出一次可核验的任务
2. `asset` — 存下的字节能按 PNG 打开
3. `hash` — SHA-256 与字节长度属于这批存下的字节
4. `export` — 导出引用同一摘要，并且标记可打开
5. `fidelity` — Logo / 包装文字 / 规格 / 结构的失败不能被相似度写成已验证回执
6. `charge` — 失败、未知、刷新或重复提交不产生第二条记账；供应商成本、用户价、补贴分开

## 提交

`Gate.Submit(ctx, Submit)` 处理一次提交。`NewGate` 返回空账本。

`Submit` 字段：`Fingerprint`、`Mode`、`Endpoint`、`Similarity`。

三者去空白后有空值（指纹、模式、端点）时返回错误 `提交不完整`，不发 HTTP，不记账。

受支持模式只有 `text_image`。其他模式不发 HTTP，结果 `outcome=unknown`，`generation=false`，资产为空，记账数为 0，`contract_passed=false`。

同一指纹的第二次提交（刷新或重复）返回第一次的副本，不再发 HTTP，记账数不增加。调用方改返回的资产切片不能改库内字节。

## 合同路径

客户端对端点发 HTTP POST，`Content-Type` 是 `application/json`，正文只有 `mode` 与 `fingerprint`。相似度不发给提供者。

只接受状态 200 且正文是 PNG 的响应：

- 先核对 PNG 签名 `89 50 4E 47 0D 0A 1A 0A`
- 再用 `image/png` 解码。解码失败则不存储
- 存下的是响应原字节，不是重新编码的图
- 返回这些字节的 SHA-256 小写十六进制和字节长度

其他情况不存储，`generation=false`，记账数不变：

| 响应 | outcome | failure class |
| --- | --- | --- |
| 状态不是 200（含 503） | `failed` | `http_` 加状态码，503 即 `http_503` |
| 200 且正文为空 | `failed` | `empty_body` |
| 200 但不是可解码 PNG | `unknown` | `unknown` |

未知不是成功。`outcome=unknown` 时 `generation=false`，`contract_passed=false`。

`contract_passed` 仅当存下的字节非空、摘要与长度对得上、并且破损 Logo 案例被拒绝。资产为空时它必须为 false。它不是商业完成。

## 四个轴

轴从像素读出，不接受调用方的通过旗标。合同图由测试用 `image/png` 编码。生产代码按下面的颜色判定，颜色值是契约：

| 轴 | 像素 | 通过颜色 |
| --- | --- | --- |
| logo（标记像素） | `(0,0)` | `R=0x11 G=0x22 B=0x33 A=0xFF` |
| packaging_text | `(1,0)` | `R=0x44 G=0x55 B=0x66 A=0xFF` |
| spec | `(2,0)` | `R=0x10 G=0x20 B=0x30 A=0xFF` |
| structure | `(3,0)` | `R=0x01 G=0x02 B=0x03 A=0xFF` |

像素越界或颜色不符就是 `fail`，否则 `pass`。

从同一批字节派生两个案例：

- 完好：解码存下的字节。标记像素在，四个轴就是实测值。
- 破损 Logo：在内存里把 `(0,0)` 清成透明黑后再测。标记不在。包装文字、规格、结构保持原测量。存下的资产仍是原字节。

`fidelity.Evaluate` 的检查由这两个测量填充，而不是由测试传入的通过旗标填充：

- `product_count`：图像宽高都大于 0 则 `pass`
- `logo_text`：logo 与 packaging_text 都 `pass` 才 `pass`，否则 `fail`
- `shape`：structure 轴
- `declared_color`：spec 轴
- `key_texture`：采样到多于一种不透明颜色则 `pass`，否则 `fail`

其余输入固定为保真模式、`subject-protect/v1`、受支持样本类 `logo_text`、保护对象齐全、`SupplierAuthorized=false`、非空 mask 与范围。`Similarity > 0` 时设 `SimilarityOnly=true`，但不得改任何一个轴。

破损案例必须让 `fidelity.BlocksVerifiedReceipt` 拒绝已验证回执（`fidelity_failed`）。报告自身也不得是通过：`verdict` 不是 `pass`，`exact_product` 与 `deliverable` 为 false。调用方传入高相似度（测试用 `0.99`）也不能把 Logo 失败翻成通过。

完好的合同图未获供应商授权，不得变成已验证回执（`exact_product` 为 false）。

导出使用完好报告的 `ExportDocument()`，并写入 `sha256`、`byte_len`、`openable=true`。

## 记账

只有第一次成功存下图像时记一条。同一指纹不会有第二条。503、空正文、未知、不受支持的模式都不增加记账。

这一条的三个金额字段分开：

| 字段 | label | amount |
| --- | --- | --- |
| 供应商成本 | `fixture` | `1.00` |
| 用户价 | `user_price` | 空 |
| 补贴 | `subsidy` | 空 |

`1.00` 是夹具内部成本，不是用户价，也不是补贴。`billing_passed` 仍为 false。

## 商业状态

`textimage.RealGenerationCompleted` 保持原样（恒为 false）。走查读取它，但不用它把合同图写成完成。

`production_generation_passed` 与 `billing_passed` 恒为 false，包括 `LivePassed` 为 true 时。

`status` 在 `LivePassed` 为 false 时逐字是 `真实出图未完成`。只有 `LivePassed` 为 true 时改为 `供应商图片已解码`。合同通过不改变这句话。

## 真实供应商尝试

每次提交最多尝试一次，且只在下面三个环境变量至少有一个去空白后非空时才进入尝试：

- `PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL`（`Config.TextImageModelCredential`）
- `PRODUCT_BG_MODEL_CREDENTIAL`（`Config.BgModelCredential`）
- `PRODUCT_LIGHT_MODEL_CREDENTIAL`（`Config.LightModelCredential`）

`config.go` 里没有与这三枚凭证成对的 URL 字段。没有 URL 就不发明地址，也不拨号。尝试结果只记录状态码、字节长度、SHA-256 和错误类：

- 三个变量都空：`LivePassed=false`，错误类 `配置缺失`，状态码 0，长度 0，摘要空
- 有凭证但没有 URL：`LivePassed=false`，错误类 `端点缺失`，不拨号，结果和错误都不含凭证

`LivePassed` 只有在非回环主机返回可解码图像时才为 true。`127.0.0.1`、`localhost`、`::1` 都不是。`httptest` 是回环，不能把 `LivePassed` 设为 true。回环上的真实 HTTP 响应仍可记录状态码、长度和摘要，但通过旗标为 false，错误类是 `loopback`。

没有 URL 时这条尝试不发起网络请求。有 URL 时也只请求一次，不重试。

## 测试

命令：

```
cd server && GOWORK=off go test -count=1 ./internal/engwalk/
```

不跑整个模块。测试必须：

- 自己用 `image/png` 编码 PNG，由端口 0 的 `httptest` 返回；客户端走 POST；测试解码存下的字节，并把摘要与服务器原字节比较
- 在 `127.0.0.1` 上 `LivePassed` 为 true 时失败
- 在存下的字节为空而 `ContractPassed` 为 true 时失败
- 在相似度把 Logo 失败翻成通过时失败
- 凭证出现在结果或错误文本里时失败
- 有凭证、无 URL 时若发生拨号则失败
