# HUI-2232 产品图工程质量 Gate（C2 轮）：限定保真 `background_plate_lock` 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让“限定保真换背景”在**冻结授权样本**上，从正常 UI 经正式报价 → 确认 → 真实模型底板 → 服务端锁定主体合成 → 逐项质量裁决 → 选定 / 导出 / 受限回流 全链路可复验，并让其它模式如实收口（不夸大、不假通过）。

**Architecture:** 不重建底座。底板走**已存在**的 `text_generate` source Task（同一 Bill 报价 / hold / charge、同一 durable Upload、同一恢复协调器），只给 source run 增加 `background_plate_lock` 模式与一个不可变的“冻结输入”侧记录；合成与质量裁决在产品侧用纯函数完成，产出一条**写一次**的派生记录（合成字节 + `product-source-quality/v2` 报告）。浏览器只提交 `input_id` 与背景文字，样本、蒙版、覆盖区、阈值、价格全部来自服务端冻结文件。旧的直连模型路由永久关闭（410）。

**Tech Stack:** Go 1.26（`net/http`、`modernc.org/sqlite`、`golang.org/x/image`，均为 `server/go.mod` 既有依赖）；Next.js 15 / React 19 / vitest；Playwright 仅作 devDependency（`playwright-core@1.63.0` 精确固定，复用本机已安装 Chrome 二进制 + 独立 profile）。

**Spec:** `docs/superpowers/specs/2026-10-02-hui-2232-quality-gate-design.md`（执行者必须与本计划一起阅读；§4.x 是各任务的行为依据，§13 是 Root 自答记录）。

**范围:** 只覆盖 spec §11 的 A1–A12（即票剩余范围 12 条）。组织付款 E7、`reference_edit`、公共“复合输出”合同、Painuo Token、SDK 升级、真人验收、生产发布均**不在**本计划内。

## Global Constraints

以下逐条来自 spec / 章程，**每个任务都隐含遵守**：

- 公共合同不动：`order-receipt/v1`、platform SDK（`github.com/bianjiefilm/public-ai/sdk/go v0.1.1`，不写 `replace`、不升级）、public-ai 一律只读。
- 默认全关：`FEATURE_BG_PLATE_LOCK=0`、`PRODUCT_FIDELITY_SAMPLES_DIR=` 为空；仅当 source runtime 就绪、付款人为个人、样本集装载并校验通过时模式才 `ready`；`FEATURE_SOURCE_ORG_IMAGES=1` 也**不**开放本模式。
- 样本集：仓库内确定性合成，**3 类（`carton` / `handled_metal` / `glass_bottle`）× 3 画布（`1024x1024`、`1024x1280`、`768x1024`）= 9 个 case**，`claim = "frozen_synthetic_sample_only"`；阈值版本 `bg-lock-thresholds/v1`，**只许收紧、不许放宽**。
- 底板固定 `1024*1024`、`qwen-image-2.0`、`n=1`（既有价格画像，不改 SKU）；画布适配 `fit-cover-center-catmullrom/v1`（等比覆盖 + 居中裁切），**禁止拉伸**。
- 报告 `product-source-quality/v2`；`text_generate` 的 v1 报告字节不变；轴状态四值 `PASS/FAIL/UNKNOWN/N/A` 永不互相折叠；输入与报告里**没有任何分数 / 相似度 / 浏览器布尔 / `supplier_authorized` 字段**。
- 候选状态只有 `limited_candidate | pending | not_usable_candidate`（派生被阻断时 `blocked`）；措辞不得出现“已验证商品图 / 完全保真”；`human_usefulness` 恒为 `NOT_RUN`。
- 迁移只加不改：`0020_plate_lock.sql`（冻结输入 + 派生）、`0021_plate_output_quality.sql`（回流质量绑定）；回退 = 关开关，**一旦存在 plate 行就不得降级二进制**。
- 资金安全：任何代码路径都不得在 `Task` 之外调用图像模型；不产生第二个 usage / quote / Task / charge；GET 为纯读。
- 章程硬边界：不 SSH 生产、不读取 / 打印 / 复制任何密钥（`.env*`、`渠道凭证-密钥.txt`、`qa-private-nine-projects-20261001/` 下任何值）、不 push / 不开 PR / 不合并远端 main、不用用户 Chrome 与内置 Browser pane、不写 Linear；只在本 worktree `product-image-engine/.worktrees/hui-2232-c2`（分支 `claude/hui-2232-quality-gate`）提交。
- 端口只用 `32320–32329`：`32320` Go server、`32321` Next、`32323` 故障代理、`32324` fixture API，其余备用。
- **所有 Go 命令必须 `GOWORK=off`**（`/Volumes/MobileHD/Work/派诺生态/go.work` 会让模块外的 `go build/test` 直接报错）。重任务（全量 `go test ./...`、`-race`、`next build`、浏览器取证）一律 `"$RUN/bin/heavy" -- <cmd>`。
- 提交：英文 conventional commit 标题，正文可中文，**结尾必须**是 `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`；不 `--no-verify`；不提交 `node_modules`、`.next`、`next-env.d.ts`、`.env*`、本地库。
- 真人环节一律标 `SIMULATED-HUMAN` 且**不进入任何裁决**；需要真人才能关闭的部分如实标 `NEEDS_REAL_HUMAN`。

## Review Focus

spec 没有逐条写出、但最可能咬到真实使用者的输入 / 条件（每条都已在所属任务里有一条钉死它的测试）：

1. **用户上传的是普通照片而不是冻结样本**：期望 = 能力端点不列它、创建返回 422 `sample_not_frozen`、**零 Billing / Task 调用**、界面用人话说明范围。→ Task 4 `TestPlateCreateRefusesBeforeAnyPaidCall`、Task 5 `photo-not-in-range`。
2. **模型返回的“新背景”几乎等于原图**（纯白背景落在原背景色附近、返回原图、±3 噪声）：期望 = `background_changed` FAIL → `not_usable_candidate`，已扣费事实保留，**不自动重做、不再扣费**，界面说清失败项。→ Task 2 `TestThresholdCalibration`、Task 3 `TestF07F06UnusablePlatesAreRecordedNotRegenerated`、Task 4 `TestPlateUnusableCandidate…`、Task 5 `not-usable-candidate`。
3. **生成过程中双击 / 刷新 / 退出重登 / 响应丢失**：期望 = 沿原键恢复同一 run，usage / quote / Task / charge 各恒为 1。→ Task 3 `TestF09…`、`TestReconcileAndRecoveryLoopDriveDerivation`，Task 5 `refresh-and-relogin-restore-same-run`，Task 6 M3 探针。
4. **背景文字的边角输入**（空、仅空白、换行 / 控制字符、201 字、前后空格、浏览器夹带 `mask` / `passed` / `prompt` / `amount` 字段）：期望 = 400，**拒绝发生在任何付费调用之前**。→ Task 4 同一张表驱动测试、Task 5 `plate-lock-bff.test.ts`。
5. **回滚 / 关开关发生在生成中途，或原图已从 Upload 清理**：期望 = 关开关只禁止新建与新确认，历史读取 / 导出 / 选定 / 删除 / 恢复照常；派生用冻结集合里的原图字节，**不依赖 Upload 里的原图还在**；样本集在计价后被换掉 → `blocked / sample_set_changed`，不下载、不扣费。→ Task 3 `TestF17…`、`TestFlagOffRefusesAnUnconfirmedQuote…`，Task 4 `TestPlateFlagOffClosesNewWorkButKeepsHistoryAndRecovery`。

## 计划阶段的 Root 自答（对 spec 的精确化 / 偏离，均已在对应任务实现并有测试）

Ruling: **P1 派生用冻结集合里的原图字节，不再经 Upload 读原图** — 因为“用户登记的原图 SHA = 冻结原图 SHA”已在创建时经 Upload 字节核对；之后再依赖 Upload 保留期只会制造 `original_unavailable`（旧 handled-metal 已出现过）。spec §4.4 第 4 点与 F17 因此改为：派生永不读 Upload 原图；若集合在计价后被换（案例摘要不一致）→ 终态 `blocked / sample_set_changed`，不下载、不扣费、底板引用保留 — 如果错了的代价：集合热替换后旧 run 变 `blocked`，需要重新报价；换来的是没有对 Upload 保留期的隐性依赖。

Ruling: **P2 派生记录写一次、不可变** — spec §4.4 里的 `selected / returned_output_id / revision` 列不进派生表：选定仍是 source run 的 `selected`，回流绑定落在 `0021` 的 `project_output_quality` — 因为可变列会让“结果与报告被事后改写”成为可能；写一次 + 读时校验 SHA 是更强的不变量 — 如果错了的代价：多一张表（0021），无其它。

Ruling: **P3 迁移拆为 0020 / 0021** — `0020` 只含冻结输入与派生（Task 3），`0021` 只含回流质量绑定（Task 4），各自随所属任务可独立提交与验证 — 如果错了的代价：无。

Ruling: **P4 `capabilities` 对 `supported_input_ids` 只读本地照片登记（SHA）** — 创建时才经 Upload 取字节核对 — 因为 GET 不得访问远端、不得写库（沿用 #38 约束） — 如果错了的代价：本地登记与 Upload 真实内容不一致时，能力端点会列出而创建时才 422（零费用，仅体验问题）。

Ruling: **P5 构建身份显式注入** — Go 在 git worktree 里会把 `vcs.revision` 盖成主仓 HEAD（已实测：盖成了 `6219124…` 且 `modified=true`），所以验收构建用 `-buildvcs=false -ldflags "-X …buildinfo.revision=… -X …buildinfo.modified=…"`；未注入时回退到工具链戳，且验收预检会因不一致而 BLOCKED，不会放行 — 如果错了的代价：忘记注入 → 预检 BLOCKED（安全方向）。

Ruling: **P6 浏览器一律用 `http://localhost:<port>` 访问** — source BFF 的同源校验拿 `Origin` 对比 Next 重建的 URL（host 恒为 `localhost`），用 `127.0.0.1` 会得到 403。

Ruling: **P7 画布适配的确定性按“同构建同架构”承诺** — 1024×1280 需要 CatmullRom 放大（浮点核，可能因 FMA 在 arm64 / amd64 间产生末位差异）；已存的派生记录才是权威，测试只断言同进程 / 同机重复派生字节一致 — 如果错了的代价：跨架构重算得到不同字节，但不影响已存记录与其 SHA 绑定。

Ruling: **P8 `/start` 个人路径收敛为一条付费主路径** — 旧 first-image 表单与 ConsumePanel 仅保留给订单 / 活动深链；个人路径换成“真实上传 → 限定保真换背景”，文字生成折叠到 `<details>`；个人工程靠 `?project=<id>` 在刷新 / 重登后找回 — 如果错了的代价：旧个人 first-image 入口消失（它本就没有真实成功证据，spec §4.8 / §4.10 要求关闭）。

## 公共环境（每个任务的 shell 都先执行）

```bash
export RUN=/Volumes/MobileHD/Work/派诺生态/.claude-orchestration/20261002
export WT=/Volumes/MobileHD/Work/派诺生态/product-image-engine/.worktrees/hui-2232-c2
export EV=$RUN/evidence/HUI-2232
export GOWORK=off GOCACHE=$RUN/caches/HUI-2232/go-build npm_config_cache=$RUN/caches/HUI-2232/npm NEXT_TELEMETRY_DISABLED=1
mkdir -p "$GOCACHE" "$npm_config_cache" "$EV"
cd "$WT" && git status --short && git branch --show-current   # 期望：分支 claude/hui-2232-quality-gate；开工前工作区干净
```

## 文件结构总览

| 任务 | 新增（Create） | 修改（Modify） |
|---|---|---|
| 1 样本集 / 装载器 / 配置 | `server/internal/fidelitysamples/{manifest.go,load.go,fidelitysamples_test.go,sampletest/sampletest.go}`、`server/internal/samplegen/{draw.go,samplegen.go,samplegen_test.go}`、`server/cmd/samplegen/main.go`、`server/internal/buildinfo/{buildinfo.go,buildinfo_test.go}`、`fixtures/frozen-samples/v1/**`（生成） | `server/internal/config/{config.go,config_test.go}`、`deploy/product-image.env.example` |
| 2 platelock 引擎 | `server/internal/platelock/{fit.go,report.go,platelock_test.go}` | — |
| 3 run 流水线 | `server/internal/sourceimage/{plate.go,plate_test.go}`、`server/internal/store/migrations/0020_plate_lock.sql`、`server/internal/store/{plate.go,plate_test.go}`、`server/internal/sourceflow/{plate.go,plate_test.go,plate_io_test.go}` | `sourceimage/{types.go,validate.go,quality.go}`、`store/{source_runs.go,source_http.go}`、`sourceflow/{types.go,quote.go,recover.go,task.go,output.go}` |
| 4 HTTP / 回流 / 接线 | `store/migrations/0021_plate_output_quality.sql`、`store/{plate_output.go,plate_output_test.go}`、`httpapi/{source_plate.go,source_plate_test.go}` | `httpapi/{server.go,source_image.go,source_image_content.go,source_image_view.go,handoff.go,bgreplace.go,bgreplace_test.go,source_image_test.go}`、`cmd/server/{source_runtime.go,source_runtime_test.go}` |
| 5 Web | `web/src/lib/plate-lock.ts`、`web/src/components/plate-lock/{PlateLockScreen.tsx,PhotoUpload.tsx}`、`web/src/components/ui/{alert,field,skeleton,stepper}.tsx`、`web/tests/plate-lock-*.test.ts(x)`、`web/e2e/{fixture-api.mjs,fixture-run.mjs,zip.mjs}` | `web/src/lib/source-image.ts`、`web/src/app/api/projects/[id]/source-image-runs/proxy.ts`、`…/background-replacements/[jobId]/[action]/route.ts`、`web/src/components/{source-image/Panel.tsx,first-image/StartScreen.tsx,background-replace/Panel.tsx}`、`web/src/app/{start/ui.tsx,projects/[id]/page.tsx,globals.css}`、`web/src/components/ui/ui.module.css`、`web/tests/{source-image-fixture.ts,source-image-mounts.test.tsx,bg-replace.test.ts}`、`web/package.json` |
| 6 真实验收 / 收口 | `web/e2e/{ledger.mjs,fault-proxy.mjs,review-validate.mjs,verdict.mjs,real-run.mjs}`、`web/tests/e2e-helpers.test.ts`、`docs/runbooks/hui-2232-plate-lock.md` | `web/package.json`（scripts） |

任务依赖：1 → 2 → 3 → 4 → 5 → 6（2 依赖 1 的样本与类型；3 依赖 1、2；4 依赖 3；5 依赖 4 的 HTTP 契约；6 依赖 5）。

---

## Task 1：冻结授权样本集、确定性生成器、运行时装载器与默认关闭的配置

**交付物（可独立提交 / 独立验证）：** 仓库内 9 个冻结合成样本（PNG 原图 + 蒙版 + `manifest.json`）、可复验的生成器 `samplegen -check`、失败即整体关闭的装载器 `fidelitysamples.Load`、`FEATURE_BG_PLATE_LOCK` / `PRODUCT_FIDELITY_SAMPLES_DIR` 两个默认关闭的配置、构建身份包 `buildinfo`。对应 spec §4.2、§4.3（装载与配置部分）、§4.5 的 `build` 来源；反例 F01–F03、F05（装载层）、F16。

**Files:**
- Create: `server/internal/fidelitysamples/manifest.go`、`load.go`、`fidelitysamples_test.go`、`sampletest/sampletest.go`
- Create: `server/internal/samplegen/draw.go`、`samplegen.go`、`samplegen_test.go`、`server/cmd/samplegen/main.go`
- Create: `server/internal/buildinfo/buildinfo.go`、`buildinfo_test.go`
- Create（由生成器产出，不手写）: `fixtures/frozen-samples/v1/manifest.json`、`README.md`、`originals/*.png`（9）、`masks/*.png`（9）
- Modify: `server/internal/config/config.go`、`server/internal/config/config_test.go`、`deploy/product-image.env.example`

**Interfaces:**
- Consumes: 既有 `bgreplace.CoverageSample` / `bgreplace.VerifyCoverage` / `bgreplace.Axis*` 常量（`server/internal/bgreplace/{coverage.go,close.go}`）。
- Produces（后续任务依赖的精确签名）：
  - `package fidelitysamples`：常量 `SetID="frozen-sample-set"`、`SetVersion="v1"`、`ThresholdsVersion="bg-lock-thresholds/v1"`、`Scope="frozen_synthetic_sample_only"`、`KindCarton/KindHandledMetal/KindGlassBottle`、`MaterialOpaque/MaterialReflective/MaterialTransparent`；`var ErrInvalid`；类型 `Rect = bgreplace.CoverageRegion`、`Canvas{W,H int}`、`File{Path,SHA256,PixelSHA256}`、`Coverage{Logo,PackagingText,Spec,Structure []Rect}`、`Case{CaseID,Kind,MaterialClass string; Canvas; Original,Mask File; Coverage; ProductCount int; ExpectedKeypoints Keypoints; Intents,HumanAxes []string}`、`Manifest`、`LoadedCase{Case; OriginalBytes,MaskBytes []byte; Sample bgreplace.CoverageSample; Evidence bgreplace.CoverageEvidence}`、`Set`；函数 `Load(dir string) (*Set, error)`、`(*Set).Cases() []*LoadedCase`、`(*Set).BySHA(sha string) (*LoadedCase, bool)`、`(*Set).ID() string`、`(*Set).SHA256() string`、`ComputeSetSHA256([]Case) string`、`PixelSHA256(image.Image) string`、`FileSHA256([]byte) string`、`ValidIntent(string) bool`、`(Case).CoverageSample() bgreplace.CoverageSample`。
  - `package sampletest`（仅测试用）：`Dir(t) string`、`Copy(t) string`、`Resign(t, dir string, fn func(*fidelitysamples.Manifest))`。
  - `package samplegen`：`Build() []Case`、`Write(dir) error`、`Check(dir) error`、`Canvases`。
  - `package buildinfo`：`Read() Info{VCSRevision string; VCSModified bool}`。
  - `config.Config`：`BgPlateLockEnabled bool`（`FEATURE_BG_PLATE_LOCK`，默认 false）、`FidelitySamplesDir string`（`PRODUCT_FIDELITY_SAMPLES_DIR`）。

- [ ] **Step 1: 先写失败的测试**

装载器测试（覆盖：已提交集合 9 case 全部通过 `VerifyCoverage`；F01 文字区被挖掉、F02 全透明蒙版、F03 全不透明蒙版、F05 改一个像素不重签、产品连通域数不符、未知字段 / 重复键 / 尾随数据、路径逃逸 / 符号链接、画布不符、背景方向不足 2 条、阈值版本不符、`set_sha256` 被改、目录缺失）：

`server/internal/fidelitysamples/fidelitysamples_test.go`

````go
package fidelitysamples_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
)

func TestCommittedSetLoadsWithNineVerifiedCases(t *testing.T) {
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases()) != 9 {
		t.Fatalf("cases = %d, want 9", len(set.Cases()))
	}
	kinds := map[string]int{}
	canvases := map[fs.Canvas]int{}
	for _, c := range set.Cases() {
		kinds[c.Kind]++
		canvases[c.Canvas]++
		if len(c.Intents) < 2 || c.ProductCount != 1 || c.Evidence.CoverageSHA256 == "" || c.Evidence.Scope != fs.Scope {
			t.Fatalf("case %s incomplete: %+v", c.CaseID, c.Evidence)
		}
		got, ok := set.BySHA(c.Original.SHA256)
		if !ok || got.CaseID != c.CaseID {
			t.Fatalf("BySHA(%s) lost %s", c.Original.SHA256, c.CaseID)
		}
		if _, ok := set.BySHA(c.Mask.SHA256); ok {
			t.Fatalf("a mask digest must never resolve as an original")
		}
		// The CoverageSample reuse is real: an independent VerifyCoverage agrees.
		if _, err := bgreplace.VerifyCoverage(c.OriginalBytes, c.MaskBytes, c.Case.CoverageSample()); err != nil {
			t.Fatalf("%s: %v", c.CaseID, err)
		}
	}
	for _, k := range []string{fs.KindCarton, fs.KindHandledMetal, fs.KindGlassBottle} {
		if kinds[k] != 3 {
			t.Fatalf("kind %s has %d cases", k, kinds[k])
		}
	}
	for _, cv := range []fs.Canvas{{W: 1024, H: 1024}, {W: 1024, H: 1280}, {W: 768, H: 1024}} {
		if canvases[cv] != 3 {
			t.Fatalf("canvas %+v has %d cases", cv, canvases[cv])
		}
	}
}

func TestLoadRejectsAnyBrokenSetAsAUnit(t *testing.T) {
	setPixel := func(t *testing.T, path string, a func(*image.NRGBA)) {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		n := image.NewNRGBA(img.Bounds())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		a(n)
		var buf bytes.Buffer
		if err := png.Encode(&buf, n); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cartonMask := "masks/carton-1024x1024.png"
	cases := []struct {
		name, want string
		mutate     func(t *testing.T, dir string)
	}{
		{"F01 text region cut out of the mask", "packaging_text", func(t *testing.T, dir string) {
			set, _ := fs.Load(sampletest.Dir(t))
			r := set.Cases()[0].Coverage.PackagingText[0]
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) { m.SetNRGBA(r[0]+3, r[1]+3, color.NRGBA{}) })
			sampletest.Resign(t, dir, nil)
		}},
		{"F02 fully transparent mask", "没有保护任何像素", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) { m.Pix = make([]uint8, len(m.Pix)) })
			sampletest.Resign(t, dir, nil)
		}},
		{"F03 fully opaque mask", "蒙版盖住了整张图", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) {
				for i := range m.Pix {
					m.Pix[i] = 255
				}
			})
			sampletest.Resign(t, dir, nil)
		}},
		{"F05 one original pixel changed, manifest not re-signed", "sha256 differs", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, "originals/carton-1024x1024.png"), func(m *image.NRGBA) { m.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 255}) })
		}},
		{"two product components but manifest says one", "product components", func(t *testing.T, dir string) {
			setPixel(t, filepath.Join(dir, cartonMask), func(m *image.NRGBA) {
				for y := 10; y < 30; y++ {
					for x := 10; x < 30; x++ {
						m.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
					}
				}
			})
			sampletest.Resign(t, dir, nil)
		}},
		{"unknown manifest field", "unknown field", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, bytes.Replace(raw, []byte(`"set_id"`), []byte(`"passed": true, "set_id"`), 1), 0o644)
		}},
		{"duplicate manifest key", "duplicate key", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, bytes.Replace(raw, []byte(`"version": "v1",`), []byte(`"version": "v1", "version": "v1",`), 1), 0o644)
		}},
		{"trailing data", "trailing data", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, append(raw, []byte(`{}`)...), 0o644)
		}},
		{"path escapes the directory", "escapes", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.Cases[0].Original.Path = "../manifest.json" })
		}},
		{"symlinked image file", "regular file", func(t *testing.T, dir string) {
			outside := filepath.Join(t.TempDir(), "x.png")
			raw, _ := os.ReadFile(filepath.Join(dir, "originals/carton-1024x1024.png"))
			os.WriteFile(outside, raw, 0o644)
			p := filepath.Join(dir, "originals/carton-1024x1024.png")
			os.Remove(p)
			if err := os.Symlink(outside, p); err != nil {
				t.Skip("symlinks unavailable")
			}
		}},
		{"symlinked directory leaves the sample directory", "outside", func(t *testing.T, dir string) {
			outside := filepath.Join(t.TempDir(), "originals")
			if err := os.Rename(filepath.Join(dir, "originals"), outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "originals")); err != nil {
				t.Skip("symlinks unavailable")
			}
		}},
		{"canvas does not match the PNG", "canvas", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.Cases[0].Canvas = fs.Canvas{W: 1024, H: 1000} })
		}},
		{"only one background intent", "intents", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.Cases[0].Intents = m.Cases[0].Intents[:1] })
		}},
		{"unapproved thresholds version", "header", func(t *testing.T, dir string) {
			sampletest.Resign(t, dir, func(m *fs.Manifest) { m.ThresholdsVersion = "bg-lock-thresholds/v0" })
		}},
		{"set digest tampered", "set_sha256", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "manifest.json")
			raw, _ := os.ReadFile(p)
			set, _ := fs.Load(sampletest.Dir(t))
			os.WriteFile(p, bytes.Replace(raw, []byte(set.SHA256()), []byte(strings.Repeat("0", 64)), 1), 0o644)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := sampletest.Copy(t)
			c.mutate(t, dir)
			_, err := fs.Load(dir)
			if !errors.Is(err, fs.ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load error = %v, want ErrInvalid containing %q", err, c.want)
			}
		})
	}
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if _, err := fs.Load(dir); !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("Load(%q) = %v", dir, err)
		}
	}
}

func TestValidIntent(t *testing.T) {
	for in, want := range map[string]bool{"深蓝色背景": true, "": false, " x": false, "x ": false, "a\nb": false, strings.Repeat("界", 200): true, strings.Repeat("界", 201): false} {
		if fs.ValidIntent(in) != want {
			t.Fatalf("ValidIntent(%q) = %v, want %v", in, !want, want)
		}
	}
}
````

生成器测试（已提交集合与生成器一致、两次 `Build` 逐像素一致、写后再查通过、篡改 1 字节被发现）：

`server/internal/samplegen/samplegen_test.go`

````go
package samplegen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	"github.com/bianjiefilm/product-image-engine/server/internal/samplegen"
)

func TestCommittedSetStillMatchesTheGenerator(t *testing.T) {
	if err := samplegen.Check(sampletest.Dir(t)); err != nil {
		t.Fatal(err)
	}
}

func TestBuildIsDeterministicAndCoversNineCases(t *testing.T) {
	a, b := samplegen.Build(), samplegen.Build()
	if len(a) != 9 || len(b) != 9 {
		t.Fatalf("cases = %d/%d", len(a), len(b))
	}
	for i := range a {
		if a[i].Meta.Original.PixelSHA256 != b[i].Meta.Original.PixelSHA256 || a[i].Meta.Mask.PixelSHA256 != b[i].Meta.Mask.PixelSHA256 {
			t.Fatalf("case %s differs between two builds", a[i].Meta.CaseID)
		}
		if a[i].Meta.Original.PixelSHA256 == a[i].Meta.Mask.PixelSHA256 {
			t.Fatalf("case %s original and mask are identical", a[i].Meta.CaseID)
		}
	}
}

func TestWriteThenCheckRoundTripAndTamperDetection(t *testing.T) {
	dir := t.TempDir()
	if err := samplegen.Write(dir); err != nil {
		t.Fatal(err)
	}
	if err := samplegen.Check(dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "masks", "carton-1024x1024.png")
	raw, _ := os.ReadFile(target)
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := samplegen.Check(dir); err == nil || !strings.Contains(err.Error(), "differ") {
		t.Fatalf("tampered mask not detected: %v", err)
	}
}
````

构建身份测试（含 `-ldflags` 接线检查）：

`server/internal/buildinfo/buildinfo_test.go`

````go
package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromReadsRevisionAndModified(t *testing.T) {
	got := from([]debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "true"}, {Key: "GOOS", Value: "darwin"}})
	if got.VCSRevision != "abc123" || !got.VCSModified {
		t.Fatalf("%+v", got)
	}
	if got := from(nil); got.VCSRevision != "" || got.VCSModified {
		t.Fatalf("empty settings: %+v", got)
	}
}

func TestExplicitLinkTimeIdentityWinsAndModifiedFailsClosed(t *testing.T) {
	defer func(r, m string) { revision, modified = r, m }(revision, modified)
	revision, modified = "deadbeef", "false"
	if got := Read(); got.VCSRevision != "deadbeef" || got.VCSModified {
		t.Fatalf("%+v", got)
	}
	// Anything but an explicit "false" counts as modified: a missing flag never
	// makes a build look clean.
	revision, modified = "deadbeef", ""
	if got := Read(); !got.VCSModified {
		t.Fatalf("%+v", got)
	}
	revision, modified = "deadbeef", "true"
	if got := Read(); !got.VCSModified {
		t.Fatalf("%+v", got)
	}
}

// Run with the real link flags to prove the -X path names are right:
//
//	go test -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=cafebabe -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=false" -run LinkTimeFlags ./internal/buildinfo
func TestLinkTimeFlagsAreWiredWhenGiven(t *testing.T) {
	if revision == "" {
		t.Skip("no -ldflags given")
	}
	if got := Read(); got.VCSRevision != revision || got.VCSModified != (modified != "false") {
		t.Fatalf("%+v", got)
	}
}
````

配置测试补丁（默认关闭、两个新环境变量被读取；并把它们加入 `setEnvs` 的清理清单）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/config/config_test.go
+++ b/server/internal/config/config_test.go
@@ -19,6 +19,7 @@
 		"FEATURE_BG_REPLACE", "FEATURE_BG_CREATIVE", "PRODUCT_BG_MODEL_CREDENTIAL", "PRODUCT_BG_MODEL_URL", "PRODUCT_BG_MODEL",
 		"TEXT_IMAGE_FIXTURE_REGISTER", "PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL", "PRODUCT_TEXT_IMAGE_MODEL_URL", "PRODUCT_TEXT_IMAGE_MODEL",
 		"FEATURE_LIGHT_SCENE", "FEATURE_LIGHT_CREATIVE", "PRODUCT_LIGHT_MODEL_CREDENTIAL", "PRODUCT_LIGHT_MODEL_URL",
+		"FEATURE_BG_PLATE_LOCK", "PRODUCT_FIDELITY_SAMPLES_DIR",
 	} {
 		t.Setenv(k, "")
 	}
@@ -235,3 +236,14 @@
 		t.Fatal("invalid source flag must remain closed")
 	}
 }
+
+func TestPlateLockDefaultsOffAndReadsItsOwnEnv(t *testing.T) {
+	setEnvs(t, nil)
+	if c := Load(); c.BgPlateLockEnabled || c.FidelitySamplesDir != "" {
+		t.Fatalf("plate lock must default off and unconfigured: %+v", c)
+	}
+	setEnvs(t, map[string]string{"FEATURE_BG_PLATE_LOCK": "1", "PRODUCT_FIDELITY_SAMPLES_DIR": "/opt/samples"})
+	if c := Load(); !c.BgPlateLockEnabled || c.FidelitySamplesDir != "/opt/samples" {
+		t.Fatalf("%+v", c)
+	}
+}
PATCH1
````

- [ ] **Step 2: 运行，确认失败**

```bash
cd "$WT/server" && go test -count=1 ./internal/fidelitysamples/... ./internal/samplegen/... ./internal/buildinfo ./internal/config 2>&1 | tail -15
```

期望：`FAIL`，原因是包不存在 / 符号未定义（`no required module provides package …/sampletest`、`undefined: ...`、`c.BgPlateLockEnabled undefined`）。**这是预期的红灯**；若任何一个包反而 `ok`，说明测试没有触到新行为，先修测试。

- [ ] **Step 3: 写实现**

样本文件清单（`Case` / `Manifest` 的 JSON 形状即 `manifest.json` 的 schema；`CoverageSample()` 直接复用既有 `VerifyCoverage`）：

`server/internal/fidelitysamples/manifest.go`

````go
// Package fidelitysamples loads the frozen, server-controlled sample set that
// bounds what "limited fidelity background replacement" may claim. The set is
// authored by cmd/samplegen, committed under fixtures/frozen-samples/v1, and
// re-verified byte by byte at load time. Browsers never supply any of it.
package fidelitysamples

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
)

const (
	SetID             = "frozen-sample-set"
	SetVersion        = "v1"
	ThresholdsVersion = "bg-lock-thresholds/v1"
	SourceGenerator   = "deterministic-generator"
	License           = "原创合成作品，项目内部授权"
	ApprovalRef       = "HUI-2232 C2 spec R3"
	// Scope is the only claim the set can support.
	Scope = "frozen_synthetic_sample_only"

	KindCarton       = "carton"
	KindHandledMetal = "handled_metal"
	KindGlassBottle  = "glass_bottle"

	MaterialOpaque      = "opaque"
	MaterialReflective  = "reflective"
	MaterialTransparent = "transparent"
)

// ErrInvalid wraps every reason the set cannot be used. The message is for
// operators; HTTP surfaces only expose the stable reason code.
var ErrInvalid = errors.New("fidelitysamples: invalid sample set")

type Rect = bgreplace.CoverageRegion

type Generator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type Canvas struct {
	W int `json:"w"`
	H int `json:"h"`
}
type File struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	PixelSHA256 string `json:"pixel_sha256"`
}
type Coverage struct {
	Logo          []Rect `json:"logo"`
	PackagingText []Rect `json:"packaging_text"`
	Spec          []Rect `json:"spec"`
	Structure     []Rect `json:"structure"`
}
type Keypoints struct {
	Text      []string `json:"text"`
	Logo      string   `json:"logo"`
	Structure []string `json:"structure"`
}
type Case struct {
	CaseID            string    `json:"case_id"`
	Kind              string    `json:"kind"`
	MaterialClass     string    `json:"material_class"`
	Canvas            Canvas    `json:"canvas"`
	Original          File      `json:"original"`
	Mask              File      `json:"mask"`
	Coverage          Coverage  `json:"coverage"`
	ProductCount      int       `json:"product_count"`
	ExpectedKeypoints Keypoints `json:"expected_keypoints"`
	Intents           []string  `json:"intents"`
	HumanAxes         []string  `json:"human_axes"`
}
type Manifest struct {
	SetID             string    `json:"set_id"`
	Version           string    `json:"version"`
	Generator         Generator `json:"generator"`
	Source            string    `json:"source"`
	License           string    `json:"license"`
	ApprovalRef       string    `json:"approval_ref"`
	ThresholdsVersion string    `json:"thresholds_version"`
	SetSHA256         string    `json:"set_sha256"`
	Cases             []Case    `json:"cases"`
}

// ComputeSetSHA256 binds case ids and file digests in manifest order.
func ComputeSetSHA256(cases []Case) string {
	h := sha256.New()
	for _, c := range cases {
		h.Write([]byte(c.CaseID + "\n" + c.Original.SHA256 + "\n" + c.Mask.SHA256 + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PixelSHA256 hashes decoded NRGBA pixels plus dimensions, independent of the
// PNG encoder, so the generator can prove it still reproduces the frozen pixels.
func PixelSHA256(img image.Image) string {
	b := img.Bounds()
	h := sha256.New()
	h.Write([]byte{byte(b.Dx() >> 8), byte(b.Dx()), byte(b.Dy() >> 8), byte(b.Dy())})
	row := make([]byte, 0, b.Dx()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row = row[:0]
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			row = append(row, c.R, c.G, c.B, c.A)
		}
		h.Write(row)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CoverageSample converts one case to the existing server-controlled coverage
// annotation so VerifyCoverage is reused rather than reimplemented.
func (c Case) CoverageSample() bgreplace.CoverageSample {
	return bgreplace.CoverageSample{
		ID: c.CaseID, Source: SourceGenerator, License: License, ApprovalRef: ApprovalRef, Scope: Scope,
		OriginalSHA256: c.Original.SHA256, MaskSHA256: c.Mask.SHA256, Width: c.Canvas.W, Height: c.Canvas.H,
		Regions: map[string][]bgreplace.CoverageRegion{
			bgreplace.AxisLogo: c.Coverage.Logo, bgreplace.AxisPackagingText: c.Coverage.PackagingText,
			bgreplace.AxisSpec: c.Coverage.Spec, bgreplace.AxisStructure: c.Coverage.Structure,
		},
	}
}

// FileSHA256 is the digest recorded for committed file bytes.
func FileSHA256(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
````

装载器（严格 JSON：拒绝未知字段 / 重复键 / 尾随数据；路径只许清单目录内的相对路径、拒绝符号链接逃逸；逐 case 重算 SHA256 / 像素哈希 / 尺寸、`VerifyCoverage`、产品连通域数；任一失败整体拒绝，**没有逐 case 降级**）：

`server/internal/fidelitysamples/load.go`

````go
package fidelitysamples

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
)

const (
	maxManifestBytes = 1 << 20
	maxImageBytes    = 8 << 20
	maxCases         = 64
	minComponentArea = 64
)

var (
	caseIDPattern = regexp.MustCompile(`^[a-z0-9_]+-[0-9]+x[0-9]+$`)
	humanAxes     = map[string]bool{"background_extra_objects": true, "background_intent_followed": true, "transmission_edge": true, "visual_composite_quality": true}
	kindMaterial  = map[string]string{KindCarton: MaterialOpaque, KindHandledMetal: MaterialReflective, KindGlassBottle: MaterialTransparent}
)

// LoadedCase carries the verified bytes. Everything here was recomputed from
// disk; nothing is trusted from the manifest alone.
type LoadedCase struct {
	Case
	OriginalBytes, MaskBytes []byte
	Sample                   bgreplace.CoverageSample
	Evidence                 bgreplace.CoverageEvidence
}

type Set struct {
	Manifest Manifest
	cases    []*LoadedCase
	bySHA    map[string]*LoadedCase
}

func (s *Set) Cases() []*LoadedCase { return s.cases }
func (s *Set) ID() string           { return s.Manifest.SetID }
func (s *Set) SHA256() string       { return s.Manifest.SetSHA256 }

// BySHA finds the case whose frozen original has exactly this file digest.
func (s *Set) BySHA(sha string) (*LoadedCase, bool) { c, ok := s.bySHA[sha]; return c, ok }

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Load verifies the whole set. Any failure rejects the set as a unit: a
// partially trusted sample set could let a mismatched mask protect the wrong
// pixels, so there is no per-case fallback.
func Load(dir string) (*Set, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, invalid("directory not configured")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, invalid("directory unreadable: %v", err)
	}
	raw, err := readRegular(filepath.Join(root, "manifest.json"), root, maxManifestBytes)
	if err != nil {
		return nil, invalid("manifest: %v", err)
	}
	var m Manifest
	if err := decodeStrict(raw, &m); err != nil {
		return nil, invalid("manifest: %v", err)
	}
	if m.SetID != SetID || m.Version != SetVersion || m.Source != SourceGenerator || m.License != License || m.ApprovalRef != ApprovalRef || m.ThresholdsVersion != ThresholdsVersion || strings.TrimSpace(m.Generator.Name) == "" || strings.TrimSpace(m.Generator.Version) == "" {
		return nil, invalid("manifest header does not match the approved frozen set")
	}
	if len(m.Cases) == 0 || len(m.Cases) > maxCases {
		return nil, invalid("case count %d out of range", len(m.Cases))
	}
	set := &Set{Manifest: m, bySHA: map[string]*LoadedCase{}}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		lc, err := loadCase(root, c)
		if err != nil {
			return nil, invalid("case %q: %v", c.CaseID, err)
		}
		if seen[c.CaseID] {
			return nil, invalid("duplicate case id %q", c.CaseID)
		}
		seen[c.CaseID] = true
		if _, dup := set.bySHA[c.Original.SHA256]; dup {
			return nil, invalid("case %q: original digest already used by another case", c.CaseID)
		}
		set.bySHA[c.Original.SHA256] = lc
		set.cases = append(set.cases, lc)
	}
	if ComputeSetSHA256(m.Cases) != m.SetSHA256 {
		return nil, invalid("set_sha256 does not match the case digests")
	}
	return set, nil
}

func loadCase(root string, c Case) (*LoadedCase, error) {
	if !caseIDPattern.MatchString(c.CaseID) || len(c.CaseID) > 64 {
		return nil, fmt.Errorf("case id shape")
	}
	if want, ok := kindMaterial[c.Kind]; !ok || c.MaterialClass != want {
		return nil, fmt.Errorf("kind/material_class pair not approved")
	}
	if c.Canvas.W < 1 || c.Canvas.H < 1 || c.Canvas.W > 4096 || c.Canvas.H > 4096 {
		return nil, fmt.Errorf("canvas out of range")
	}
	if c.ProductCount < 1 || c.ProductCount > 8 {
		return nil, fmt.Errorf("product_count out of range")
	}
	if len(c.Intents) < 2 || len(c.Intents) > 8 {
		return nil, fmt.Errorf("need 2..8 background intents")
	}
	for _, in := range c.Intents {
		if !validIntent(in) {
			return nil, fmt.Errorf("background intent invalid")
		}
	}
	if len(c.HumanAxes) == 0 {
		return nil, fmt.Errorf("human_axes missing")
	}
	for _, a := range c.HumanAxes {
		if !humanAxes[a] {
			return nil, fmt.Errorf("unknown human axis %q", a)
		}
	}
	orig, _, err := loadImage(root, c.Original, c.Canvas)
	if err != nil {
		return nil, fmt.Errorf("original: %w", err)
	}
	mask, maskImg, err := loadImage(root, c.Mask, c.Canvas)
	if err != nil {
		return nil, fmt.Errorf("mask: %w", err)
	}
	sample := c.CoverageSample()
	evidence, err := bgreplace.VerifyCoverage(orig, mask, sample)
	if err != nil {
		return nil, fmt.Errorf("coverage: %v", err)
	}
	if n := countComponents(maskImg); n != c.ProductCount {
		return nil, fmt.Errorf("mask has %d product components, manifest says %d", n, c.ProductCount)
	}
	return &LoadedCase{Case: c, OriginalBytes: orig, MaskBytes: mask, Sample: sample, Evidence: evidence}, nil
}

// ValidIntent is the single rule for a background direction, used for frozen
// intents here and for the user's text at the HTTP boundary.
func ValidIntent(s string) bool { return validIntent(s) }

func validIntent(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 200 || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func loadImage(root string, f File, canvas Canvas) ([]byte, image.Image, error) {
	if !filepath.IsLocal(filepath.FromSlash(f.Path)) || strings.Contains(f.Path, `\`) {
		return nil, nil, fmt.Errorf("path escapes the sample directory")
	}
	raw, err := readRegular(filepath.Join(root, filepath.FromSlash(f.Path)), root, maxImageBytes)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != f.SHA256 {
		return nil, nil, fmt.Errorf("sha256 differs from manifest")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != canvas.W || cfg.Height != canvas.H {
		return nil, nil, fmt.Errorf("not a PNG of the case canvas")
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("PNG undecodable")
	}
	if PixelSHA256(img) != f.PixelSHA256 {
		return nil, nil, fmt.Errorf("pixel_sha256 differs from manifest")
	}
	return raw, img, nil
}

// readRegular reads a regular file that really lives under root (no symlink
// hops out of the directory).
func readRegular(path, root string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if rel, err := filepath.Rel(root, real); err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("resolves outside the sample directory")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit || len(raw) == 0 {
		return nil, fmt.Errorf("size out of range")
	}
	return raw, nil
}

// decodeStrict rejects unknown fields, duplicate keys at any depth and trailing
// data. encoding/json alone accepts all three.
func decodeStrict(raw []byte, dst any) error {
	if err := rejectDuplicateKeys(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the manifest object")
	}
	return nil
}

func rejectDuplicateKeys(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return err
				}
				k := kt.(string)
				if keys[k] {
					return fmt.Errorf("duplicate key %q", k)
				}
				keys[k] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		_, err = d.Token()
		return err
	}
	return walk()
}

// countComponents counts 4-connected regions of protected pixels
// (alpha >= 128) with at least minComponentArea pixels.
func countComponents(img image.Image) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	prot := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			prot[y*w+x] = a>>8 >= 128
		}
	}
	count := 0
	stack := make([]int, 0, 1024)
	for start := range prot {
		if !prot[start] {
			continue
		}
		area := 0
		prot[start] = false
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			area++
			x, y := p%w, p/w
			for _, n := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if n[0] < 0 || n[1] < 0 || n[0] >= w || n[1] >= h {
					continue
				}
				if q := n[1]*w + n[0]; prot[q] {
					prot[q] = false
					stack = append(stack, q)
				}
			}
		}
		if area >= minComponentArea {
			count++
		}
	}
	return count
}
````

测试辅助（只被测试 import；`Resign` 在改完文件后重算所有摘要，使“只有被测属性是坏的”）：

`server/internal/fidelitysamples/sampletest/sampletest.go`

````go
// Package sampletest gives tests the committed frozen set and a way to mutate a
// private copy of it. It is never imported by non-test code.
package sampletest

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	fsamples "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

// Dir is the committed fixtures/frozen-samples/v1 directory.
func Dir(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "fixtures", "frozen-samples", "v1")
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatalf("frozen sample set missing: %v", err)
	}
	return dir
}

// Copy duplicates the committed set into a temp directory the test may mutate.
func Copy(t testing.TB) string {
	t.Helper()
	src, dst := Dir(t), t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// Resign rewrites the manifest after fn mutated fields, recomputing every file
// digest, pixel digest and the set digest from the files on disk so that only
// the property under test is broken.
func Resign(t testing.TB, dir string, fn func(m *fsamples.Manifest)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m fsamples.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if fn != nil {
		fn(&m)
	}
	for i := range m.Cases {
		for _, f := range []*fsamples.File{&m.Cases[i].Original, &m.Cases[i].Mask} {
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err != nil {
				continue // a deliberately unreadable path keeps its old digests
			}
			f.SHA256 = fsamples.FileSHA256(b)
			if img, err := png.Decode(bytes.NewReader(b)); err == nil {
				f.PixelSHA256 = fsamples.PixelSHA256(img)
			}
		}
	}
	m.SetSHA256 = fsamples.ComputeSetSHA256(m.Cases)
	out, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}
````

确定性绘制（纯整数运算 + `basicfont` 点阵字，无随机无时间；`u = min(w,h)/16`，所有几何都是 `u` 的整数倍 / 分数）：

`server/internal/samplegen/draw.go`

````go
// Package samplegen deterministically draws the frozen synthetic product
// samples. It uses only integer arithmetic and the fixed basicfont bitmap, so
// the same pixels are reproduced on every machine and Go version.
package samplegen

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

func fillRect(img *image.NRGBA, r fs.Rect, c color.NRGBA) {
	for y := r[1]; y < r[3]; y++ {
		for x := r[0]; x < r[2]; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// fillDisk draws a filled disk using integer distance, clipped by optional minX.
func fillDisk(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// drawText renders s with the 7x13 bitmap face scaled by an integer factor and
// returns the exact box it occupies.
func drawText(img *image.NRGBA, s string, x, y, scale int, c color.NRGBA) fs.Rect {
	w, h := len(s)*7, 13
	tmp := image.NewNRGBA(image.Rect(0, 0, w, h))
	d := &font.Drawer{Dst: tmp, Src: image.NewUniform(color.NRGBA{255, 255, 255, 255}), Face: basicfont.Face7x13, Dot: fixed.P(0, 11)}
	d.DrawString(s)
	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			if tmp.NRGBAAt(tx, ty).A == 0 {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetNRGBA(x+tx*scale+dx, y+ty*scale+dy, c)
				}
			}
		}
	}
	return fs.Rect{x, y, x + w*scale, y + h*scale}
}

type drawn struct {
	original, mask *image.NRGBA
	cov            fs.Coverage
}

func newPair(w, h int, bg color.NRGBA) (*image.NRGBA, *image.NRGBA) {
	o := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillRect(o, fs.Rect{0, 0, w, h}, bg)
	m := image.NewNRGBA(image.Rect(0, 0, w, h)) // fully transparent
	return o, m
}

var opaqueMask = color.NRGBA{255, 255, 255, 255}

func drawCarton(w, h int) drawn {
	u := min(w, h) / 16
	o, m := newPair(w, h, color.NRGBA{226, 222, 214, 255})
	cx, cy := w/2, h/2
	hw, hh := 7*u/2, 9*u/2
	body := fs.Rect{cx - hw, cy - hh, cx + hw, cy + hh}
	fillRect(o, body, color.NRGBA{196, 60, 48, 255})
	fillRect(m, body, opaqueMask)
	flap := fs.Rect{body[0], body[1], body[2], body[1] + u}
	fillRect(o, flap, color.NRGBA{150, 40, 34, 255})
	fillRect(o, fs.Rect{body[0], flap[3], body[2], flap[3] + 2}, color.NRGBA{110, 30, 26, 255})
	panel := fs.Rect{body[0] + u, body[1] + u + u/4, body[2] - u, body[3] - u}
	fillRect(o, panel, color.NRGBA{246, 244, 238, 255})
	ly := panel[1] + u + u/4
	fillDisk(o, cx, ly, u, color.NRGBA{196, 60, 48, 255})
	fillDisk(o, cx, ly, u/2, color.NRGBA{246, 244, 238, 255})
	logo := fs.Rect{cx - u, ly - u, cx + u + 1, ly + u + 1}
	s := u / 12
	s2 := (s + 1) / 2
	s3 := (s2 + 1) / 2
	ink := color.NRGBA{34, 30, 28, 255}
	tx := drawText(o, "PAINUO", cx-6*7*s/2, panel[1]+2*u+u/2, s, ink)
	sp := drawText(o, "NET 250G", cx-8*7*s2/2, tx[3]+u/4, s2, ink)
	lot := drawText(o, "LOT 20261002", cx-12*7*s3/2, sp[3]+u/4, s3, ink)
	return drawn{o, m, fs.Coverage{
		Logo: []fs.Rect{logo}, PackagingText: []fs.Rect{tx, lot}, Spec: []fs.Rect{sp},
		Structure: []fs.Rect{flap, {body[0], body[3] - u/2, body[2], body[3]}},
	}}
}

var steel = [8]uint8{120, 170, 215, 235, 225, 190, 150, 125}

func drawHandledMetal(w, h int) drawn {
	u := min(w, h) / 16
	o, m := newPair(w, h, color.NRGBA{214, 220, 226, 255})
	cx, cy := w/2, h/2
	x0 := cx - 7*u/2
	body := fs.Rect{x0, cy - 3*u, x0 + 5*u, cy + 3*u}
	bw := body[2] - body[0]
	for x := body[0]; x < body[2]; x++ {
		v := steel[(x-body[0])*8/bw]
		fillRect(o, fs.Rect{x, body[1], x + 1, body[3]}, color.NRGBA{v, v + 4, v + 10, 255})
	}
	fillRect(m, body, opaqueMask)
	rim := fs.Rect{body[0], body[1], body[2], body[1] + u/2}
	fillRect(o, rim, color.NRGBA{240, 244, 248, 255})
	hx, hy, big, small := body[2], cy, 2*u, u+u/4
	for y := hy - big; y <= hy+big; y++ {
		for x := hx; x <= hx+big; x++ {
			d := (x-hx)*(x-hx) + (y-hy)*(y-hy)
			if d <= big*big && d >= small*small {
				o.SetNRGBA(x, y, color.NRGBA{190, 196, 204, 255})
				m.SetNRGBA(x, y, opaqueMask)
			}
		}
	}
	s := u / 12
	s2 := (s + 1) / 2
	ink := color.NRGBA{52, 56, 64, 255}
	ly := body[1] + u + u/2
	fillDisk(o, (body[0]+body[2])/2, ly, u/2, ink)
	fillDisk(o, (body[0]+body[2])/2, ly, u/4, color.NRGBA{225, 230, 236, 255})
	logo := fs.Rect{(body[0]+body[2])/2 - u/2, ly - u/2, (body[0]+body[2])/2 + u/2 + 1, ly + u/2 + 1}
	tx := drawText(o, "PAINUO", (body[0]+body[2])/2-6*7*s/2, body[1]+2*u+u/2, s, ink)
	sp := drawText(o, "350ML", (body[0]+body[2])/2-5*7*s2/2, tx[3]+u/2, s2, ink)
	bar := fs.Rect{hx, hy - big + u/8, hx + u/4, hy - small - u/16}
	return drawn{o, m, fs.Coverage{
		Logo: []fs.Rect{logo}, PackagingText: []fs.Rect{tx}, Spec: []fs.Rect{sp},
		Structure: []fs.Rect{rim, bar},
	}}
}

func drawGlassBottle(w, h int) drawn {
	u := min(w, h) / 16
	o, m := newPair(w, h, color.NRGBA{222, 228, 222, 255})
	cx, cy := w/2, h/2
	top := cy - 4*u
	cap := fs.Rect{cx - u, top, cx + u, top + 3*u/4}
	neck := fs.Rect{cx - 3*u/4, cap[3], cx + 3*u/4, cap[3] + 2*u}
	body := fs.Rect{cx - 2*u, neck[3], cx + 2*u, neck[3] + 5*u}
	fillRect(o, cap, color.NRGBA{40, 90, 120, 255})
	fillRect(o, neck, color.NRGBA{176, 214, 210, 255})
	fillRect(o, body, color.NRGBA{190, 226, 222, 255})
	fillRect(o, fs.Rect{body[0], body[1], body[0] + u/8, body[3]}, color.NRGBA{140, 190, 186, 255})
	fillRect(o, fs.Rect{body[2] - u/8, body[1], body[2], body[3]}, color.NRGBA{140, 190, 186, 255})
	fillRect(o, fs.Rect{body[0] + u/4, body[1] + u/4, body[0] + u/2, body[3] - u/4}, color.NRGBA{240, 250, 248, 255})
	for _, r := range []fs.Rect{cap, neck, body} {
		fillRect(m, r, opaqueMask)
	}
	label := fs.Rect{body[0] + u/4 + u/2, body[1] + u, body[2] - u/4, body[3] - u}
	fillRect(o, label, color.NRGBA{246, 246, 240, 255})
	s := u / 16
	s2 := (s + 1) / 2
	ink := color.NRGBA{30, 70, 96, 255}
	lcx := (label[0] + label[2]) / 2
	ly := label[1] + u/8 + u/2
	fillDisk(o, lcx, ly, u/2, color.NRGBA{40, 90, 120, 255})
	fillDisk(o, lcx, ly, u/4, color.NRGBA{246, 246, 240, 255})
	logo := fs.Rect{lcx - u/2, ly - u/2, lcx + u/2 + 1, ly + u/2 + 1}
	tx := drawText(o, "PAINUO", lcx-6*7*s/2, ly+u/2+u/8, s, ink)
	sp := drawText(o, "500ML", lcx-5*7*s2/2, tx[3]+u/8, s2, ink)
	return drawn{o, m, fs.Coverage{
		Logo: []fs.Rect{logo}, PackagingText: []fs.Rect{tx}, Spec: []fs.Rect{sp},
		Structure: []fs.Rect{cap, neck},
	}}
}
````

生成器与 `-check`（`Check` 同时核对：提交字节 = 清单摘要、解码像素 = 清单像素摘要 = 重新生成的像素摘要、清单除文件摘要外的所有字段 = 生成器清单）：

`server/internal/samplegen/samplegen.go`

````go
package samplegen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

const GeneratorName, GeneratorVersion = "samplegen", "1"

// Canvases are the common e-commerce outputs the gate covers.
var Canvases = []fs.Canvas{{W: 1024, H: 1024}, {W: 1024, H: 1280}, {W: 768, H: 1024}}

type kindSpec struct {
	kind, material string
	draw           func(w, h int) drawn
	keypoints      fs.Keypoints
	intents        []string
	humanAxes      []string
}

var kinds = []kindSpec{
	{fs.KindCarton, fs.MaterialOpaque, drawCarton,
		fs.Keypoints{Text: []string{"PAINUO", "NET 250G", "LOT 20261002"}, Logo: "circle-emblem", Structure: []string{"top-flap", "body"}},
		[]string{"深蓝色渐变摄影棚背景，柔和顶光", "暖色原木桌面，窗边自然光"},
		[]string{"background_extra_objects", "background_intent_followed", "visual_composite_quality"}},
	{fs.KindHandledMetal, fs.MaterialReflective, drawHandledMetal,
		fs.Keypoints{Text: []string{"PAINUO", "350ML"}, Logo: "circle-emblem", Structure: []string{"rim", "handle-hole"}},
		[]string{"深灰色大理石台面，冷色侧光", "绿色植物墙背景，浅景深"},
		[]string{"background_extra_objects", "background_intent_followed", "visual_composite_quality"}},
	{fs.KindGlassBottle, fs.MaterialTransparent, drawGlassBottle,
		fs.Keypoints{Text: []string{"PAINUO", "500ML"}, Logo: "circle-emblem", Structure: []string{"cap", "neck", "body"}},
		[]string{"黑色亚克力台面，细长高光", "海边沙滩与蓝天，明亮日光"},
		[]string{"background_extra_objects", "background_intent_followed", "transmission_edge", "visual_composite_quality"}},
}

// Case is one generated sample before encoding.
type Case struct {
	Meta           fs.Case
	Original, Mask *image.NRGBA
}

// Build returns the nine cases (3 kinds x 3 canvases) in manifest order.
func Build() []Case {
	var out []Case
	for _, k := range kinds {
		for _, cv := range Canvases {
			d := k.draw(cv.W, cv.H)
			id := fmt.Sprintf("%s-%dx%d", k.kind, cv.W, cv.H)
			out = append(out, Case{
				Original: d.original, Mask: d.mask,
				Meta: fs.Case{
					CaseID: id, Kind: k.kind, MaterialClass: k.material, Canvas: cv,
					Original:     fs.File{Path: "originals/" + id + ".png", PixelSHA256: fs.PixelSHA256(d.original)},
					Mask:         fs.File{Path: "masks/" + id + ".png", PixelSHA256: fs.PixelSHA256(d.mask)},
					Coverage:     d.cov,
					ProductCount: 1, ExpectedKeypoints: k.keypoints,
					Intents: append([]string(nil), k.intents...), HumanAxes: append([]string(nil), k.humanAxes...),
				},
			})
		}
	}
	return out
}

func encode(img *image.NRGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func fileSHA(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func manifestFor(cases []Case, fileShas map[string]string) fs.Manifest {
	m := fs.Manifest{SetID: fs.SetID, Version: fs.SetVersion, Generator: fs.Generator{Name: GeneratorName, Version: GeneratorVersion},
		Source: fs.SourceGenerator, License: fs.License, ApprovalRef: fs.ApprovalRef, ThresholdsVersion: fs.ThresholdsVersion}
	for _, c := range cases {
		meta := c.Meta
		meta.Original.SHA256 = fileShas[meta.Original.Path]
		meta.Mask.SHA256 = fileShas[meta.Mask.Path]
		m.Cases = append(m.Cases, meta)
	}
	m.SetSHA256 = fs.ComputeSetSHA256(m.Cases)
	return m
}

func marshal(m fs.Manifest) []byte {
	raw, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		panic(err)
	}
	return append(raw, '\n')
}

// Write encodes every PNG, writes the manifest and the README under dir.
func Write(dir string) error {
	cases := Build()
	shas := map[string]string{}
	for _, c := range cases {
		for _, f := range []struct {
			path string
			img  *image.NRGBA
		}{{c.Meta.Original.Path, c.Original}, {c.Meta.Mask.Path, c.Mask}} {
			raw := encode(f.img)
			full := filepath.Join(dir, filepath.FromSlash(f.path))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(full, raw, 0o644); err != nil {
				return err
			}
			shas[f.path] = fileSHA(raw)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), marshal(manifestFor(cases, shas)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644)
}

// Check proves the committed set is still exactly what the generator draws:
// committed bytes match the manifest digests, decoded pixels match the
// manifest, regenerated pixels match the manifest, and every non-digest field
// equals the regenerated manifest. It never rewrites anything.
func Check(dir string) error {
	rawManifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var committed fs.Manifest
	if err := json.Unmarshal(rawManifest, &committed); err != nil {
		return err
	}
	cases := Build()
	if len(committed.Cases) != len(cases) {
		return fmt.Errorf("case count %d != generated %d", len(committed.Cases), len(cases))
	}
	shas := map[string]string{}
	for i, c := range cases {
		cm := committed.Cases[i]
		for _, f := range []struct {
			file  fs.File
			pixel string
		}{{cm.Original, c.Meta.Original.PixelSHA256}, {cm.Mask, c.Meta.Mask.PixelSHA256}} {
			raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.file.Path)))
			if err != nil {
				return err
			}
			if fileSHA(raw) != f.file.SHA256 {
				return fmt.Errorf("%s: committed bytes differ from manifest sha256", f.file.Path)
			}
			img, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				return fmt.Errorf("%s: %w", f.file.Path, err)
			}
			if got := fs.PixelSHA256(img); got != f.file.PixelSHA256 || got != f.pixel {
				return fmt.Errorf("%s: pixels differ from generator (manifest %s, decoded %s, generated %s)", f.file.Path, f.file.PixelSHA256, got, f.pixel)
			}
			shas[f.file.Path] = f.file.SHA256
		}
	}
	if want := marshal(manifestFor(cases, shas)); !bytes.Equal(want, rawManifest) {
		return fmt.Errorf("manifest.json differs from the generator's manifest (fields other than file digests)")
	}
	return nil
}

const readme = `# 冻结授权样本集 v1（HUI-2232 C2）

原创合成商品图，由 server/cmd/samplegen 确定性生成，项目内部授权，仅用于验证“限定保真换背景”。
它们不是实物照片：主体为硬边渲染，蒙版即主体轮廓，不覆盖真实照片的边缘羽化/光晕。

- 3 类商品（纸盒/带把金属杯/玻璃瓶）x 3 个画布（1024x1024、1024x1280、768x1024）= 9 个 case。
- manifest.json 记录每个文件的 sha256（文件字节）与 pixel_sha256（解码像素），改一个像素都会被发现。
- 校验：cd server && go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1
- 重新生成（仅在有意修改样本并升版本时）：go run ./cmd/samplegen -write ../fixtures/frozen-samples/v1
- 已验证范围只到本集合；任意用户照片不在范围内。
`
````

命令行：

`server/cmd/samplegen/main.go`

````go
// samplegen writes or checks the frozen synthetic sample set.
//
//	go run ./cmd/samplegen -write ../fixtures/frozen-samples/v1
//	go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bianjiefilm/product-image-engine/server/internal/samplegen"
)

func main() {
	write := flag.String("write", "", "write the set into this directory")
	check := flag.String("check", "", "verify the committed set in this directory")
	flag.Parse()
	if (*write == "") == (*check == "") {
		fmt.Fprintln(os.Stderr, "usage: samplegen -write DIR | -check DIR")
		os.Exit(2)
	}
	if *write != "" {
		if err := samplegen.Write(*write); err != nil {
			fmt.Fprintln(os.Stderr, "samplegen:", err)
			os.Exit(1)
		}
		fmt.Println("samplegen: wrote", *write)
		return
	}
	if err := samplegen.Check(*check); err != nil {
		fmt.Fprintln(os.Stderr, "samplegen: CHECK FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("samplegen: check ok (9 cases)")
}
````

构建身份（显式 `-ldflags` 优先；缺省 `modified` 一律按“已修改”处理，永远不会让没注入的构建看起来干净）：

`server/internal/buildinfo/buildinfo.go`

````go
// Package buildinfo reports the code identity compiled into the binary so that
// acceptance evidence can prove which commit produced a result.
//
// Go's own VCS stamping is not trusted here: inside a linked git worktree it can
// walk up to the main repository and stamp the wrong commit. Release and
// acceptance builds therefore pass the identity explicitly:
//
//	go build -buildvcs=false -ldflags "\
//	  -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=$(git rev-parse HEAD) \
//	  -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=$([ -n "$(git status --porcelain)" ] && echo true || echo false)" ./cmd/server
package buildinfo

import "runtime/debug"

// Set only by -ldflags -X (string variables are the only kind -X can set).
var revision, modified string

type Info struct {
	VCSRevision string
	VCSModified bool
}

// Read returns the explicit link-time identity when present, else whatever the
// toolchain stamped. A binary built without either reports an empty revision,
// which acceptance preflight refuses.
func Read() Info {
	if revision != "" {
		return Info{VCSRevision: revision, VCSModified: modified != "false"}
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{}
	}
	return from(bi.Settings)
}

func from(settings []debug.BuildSetting) Info {
	var out Info
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			out.VCSRevision = s.Value
		case "vcs.modified":
			out.VCSModified = s.Value == "true"
		}
	}
	return out
}
````

配置补丁（两个新字段，默认关闭 / 空）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/config/config.go
+++ b/server/internal/config/config.go
@@ -14,6 +14,12 @@
 	SourceOrgImagesEnabled bool   // FEATURE_SOURCE_ORG_IMAGES, default off
 	SourcePricingVersion   string // PRODUCT_SOURCE_PRICING_VERSION, explicit only
 	SourceDownloadHosts    string // PRODUCT_SOURCE_DOWNLOAD_HOSTS, exact OSS origins
+
+	// --- HUI-2232 C2: limited-fidelity background_plate_lock (default off) ---
+	// The mode additionally needs the source runtime above, a personal payer and a
+	// frozen sample set that loads and verifies. It never reads a browser claim.
+	BgPlateLockEnabled bool   // FEATURE_BG_PLATE_LOCK
+	FidelitySamplesDir string // PRODUCT_FIDELITY_SAMPLES_DIR: directory holding manifest.json
 
 	Addr          string // PRODUCT_SERVER_ADDR,默认 127.0.0.1:18220
 	DBPath        string // PRODUCT_DB_PATH,默认 ./data/product-image.db
@@ -122,6 +128,8 @@
 		SourceOrgImagesEnabled: getBoolEnv("FEATURE_SOURCE_ORG_IMAGES", false),
 		SourcePricingVersion:   os.Getenv("PRODUCT_SOURCE_PRICING_VERSION"),
 		SourceDownloadHosts:    os.Getenv("PRODUCT_SOURCE_DOWNLOAD_HOSTS"),
+		BgPlateLockEnabled:     getBoolEnv("FEATURE_BG_PLATE_LOCK", false),
+		FidelitySamplesDir:     os.Getenv("PRODUCT_FIDELITY_SAMPLES_DIR"),
 		Addr:                   getEnv("PRODUCT_SERVER_ADDR", "127.0.0.1:18220"),
 		DBPath:                 getEnv("PRODUCT_DB_PATH", "./data/product-image.db"),
 		InternalToken:          os.Getenv("PRODUCT_INTERNAL_TOKEN"),
PATCH1
````

部署样例补充（只加注释与默认值，不写任何真实值）：

```bash
cd "$WT" && cat >> deploy/product-image.env.example <<'ENVEOF'

# --- 限定保真换背景(HUI-2232 C2;默认关闭)---
# 开启还需要:上面的正式来源生成已就绪、付款人为个人、样本集装载并校验通过。
# 样本集是仓库内的冻结合成样本(fixtures/frozen-samples/v1),部署时把该目录放到服务器并在此指向它。
# 样本集缺失 / 被改动 / 校验不过只会关闭本模式(能力端点给出原因),不会让进程退出。
FEATURE_BG_PLATE_LOCK=0
PRODUCT_FIDELITY_SAMPLES_DIR=
ENVEOF
```

- [ ] **Step 4: 生成并提交冻结样本集**

```bash
cd "$WT/server" && mkdir -p ../fixtures/frozen-samples/v1 && go run ./cmd/samplegen -write ../fixtures/frozen-samples/v1
go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1      # 期望输出：samplegen: check ok (9 cases)
ls ../fixtures/frozen-samples/v1/originals ../fixtures/frozen-samples/v1/masks | head -30
du -sh ../fixtures                                                # 期望 ≈ 180K
```

跨机器确定性核对：下表是计划作者在本机生成的**解码像素摘要**（`pixel_sha256`）。你生成的 `manifest.json` 里这两列必须**逐字相同**；文件字节摘要 `sha256` 取决于 Go 的 PNG 编码器版本，**不要求**相同（提交进去的字节才是冻结物）。

| case_id | original.pixel_sha256 | mask.pixel_sha256 |
|---|---|---|
| `carton-1024x1024` | `46d7af4f1390a1105e01b6967104327c8645652578b6ab6b73b5ab1365ee5637` | `8fbce25075da8854ccb01d3a9b3be220914e1defcfe4c57d58722d0c9aa1f564` |
| `carton-1024x1280` | `50ccf84c98e91709a8a84ef35826ecd4779b01be886d98289983977639b8165f` | `1423b9b85fa1c95e26845b5c5216deafdf2f6c811bfcb530255f99e85764c28a` |
| `carton-768x1024` | `c118b2acc7a964e35b546882a56fba2dcf64fa06a44b7e817342f1cee823dc84` | `62f91267861efaae0aafaa2c76d1cdd1c273b20ddc83d0dcb0f92aabf79617ca` |
| `handled_metal-1024x1024` | `2e44e3b670c3a77cdff936cbdcd8ee128de39d1d2507747052b614c511eceee7` | `a7b35d5bdecf6f83c07adae609fb2fc4306df911916f42745d375e02242fc02f` |
| `handled_metal-1024x1280` | `adfedeea73c05a6920220ae121050a5bf8d169c0432c0403028f8586e0c20fde` | `279ca76d34110fa85818a9266a9edba554f5483620bb06594bb209c539a4b5d6` |
| `handled_metal-768x1024` | `1ca35d819095fb5b5649e1365946dbc2870d8bc512490ba3af4c52c92b63d636` | `81f7e76c2a6bbedb31865df645f81386508fe3fb76edc28f54169562b5da2b69` |
| `glass_bottle-1024x1024` | `9f7bfc2d9875f7f43f09a68b8e7f77ea89393d7f90d2948032a87d4b67abe6a4` | `68408a7f3f0dc7068701586aaae515b21c2105002ac2ff02dbc07033e1e33aea` |
| `glass_bottle-1024x1280` | `f6cefa2bbf333742d366269ee27af4ab192ba3a1d9bcc6c3f85ec6deebee327d` | `1eb0a42dc9e32b9c9ca266a05aacba7a78f20c026074328e50c8eb03bd2c1f68` |
| `glass_bottle-768x1024` | `3ee0aa782c572e17867a0a7a5d95bb206876f0349b2592b683a07d28c021ad0c` | `8f454aaf8c197ac0cea8cf090c4e68ac6c0cbed0698925f8246f79afe3949202` |

用肉眼确认三类样本确实是“纸盒（含 Logo 圆环、`PAINUO` / `NET 250G` / `LOT 20261002`）”“带把手金属杯（把手环中间是空洞、孔内是背景）”“玻璃瓶（瓶盖 / 瓶颈 / 瓶身 + 标签）”（用 Read 工具打开 `originals/carton-1024x1280.png`、`originals/handled_metal-768x1024.png`、`originals/glass_bottle-1024x1024.png` 即可）。

- [ ] **Step 5: 运行，确认通过**

```bash
cd "$WT/server" && "$RUN/bin/heavy" -- go test -count=1 ./internal/fidelitysamples/... ./internal/samplegen/... ./internal/buildinfo ./internal/config 2>&1 | tail -12
go test -count=1 -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=cafebabe -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=false" -run LinkTimeFlags -v ./internal/buildinfo | tail -4
```

期望：四个包全 `ok`（`fidelitysamples` 约 7–9 秒，因为反例矩阵反复装载 9 个 1MP 样本）；`-ldflags` 那条出现 `--- PASS: TestLinkTimeFlagsAreWiredWhenGiven`（不是 SKIP）。

- [ ] **Step 6: 全仓静态检查与基线不退化**

```bash
cd "$WT/server" && gofmt -l internal cmd | grep -v -E "entry_test.go"      # 期望：无输出（entry_test.go 是既有未格式化文件，不是本任务的）
go vet ./...                                                                 # 期望：无输出，退出码 0
"$RUN/bin/heavy" -- go test -p 2 -count=1 ./... 2>&1 | tee "$EV/task1-go-test.log" | tail -5   # 期望：全部 ok
```

- [ ] **Step 7: 提交**

```bash
cd "$WT" && git add server/internal/fidelitysamples server/internal/samplegen server/cmd/samplegen server/internal/buildinfo server/internal/config fixtures deploy/product-image.env.example
git status --short     # 只能出现上面这些路径
git commit -m "feat(fidelity): freeze a verifiable synthetic sample set and its loader

3 类商品 x 3 画布共 9 个确定性合成样本（PNG 字节与解码像素双摘要冻结），
samplegen -check 可复验；fidelitysamples.Load 严格解析清单并复用 VerifyCoverage，
任一失败整体拒绝。新增 FEATURE_BG_PLATE_LOCK / PRODUCT_FIDELITY_SAMPLES_DIR
（默认关闭）与显式 -ldflags 构建身份 buildinfo。

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


---

## Task 2：`platelock` 引擎——等比覆盖适配、主体锁回、服务端质量证据报告 v2

**交付物：** 纯函数包 `platelock`（无 I/O、无时钟、无随机）：`FitPlateCover`、`Compose`、`Evaluate`、`Derive`、`StateOf`、`ReportJSON`、`DecodeReport`；冻结阈值 `bg-lock-thresholds/v1` 及其正反例标定；报告里不存在任何分数入口。对应 spec §4.2（阈值）、§4.5、§4.4 第 5 点（适配）；反例 F06、F07、F08(b)(c)、F18、F22。

**Files:**
- Create: `server/internal/platelock/fit.go`、`report.go`、`platelock_test.go`

**Interfaces:**
- Consumes（Task 1）：`fidelitysamples.Case` / `Rect` / `Scope` / `ThresholdsVersion` / `MaterialTransparent`；`fidelitysamples.Load`、`sampletest.Dir`（仅测试）；既有 `bgreplace.LockSubject`、`bgreplace.SubjectPixelChecks`（仅测试，用来做**独立**对照）。
- Produces（Task 3 / 4 精确使用）：
  - 常量 `ReportVersion="product-source-quality/v2"`、`Claim`、`FitID="fit-cover-center-catmullrom/v1"`、`ComposeID="compose-mask-lock/v1"`、`PlateSide=1024`、`Pass/Fail/Unknown/NA`、`CandidateLimited/CandidatePending/CandidateNotUsable`、轴名常量 `Axis*`。
  - `type Input struct{ Case fidelitysamples.Case; Original, Mask []byte; CoverageSHA256 string; Plate []byte; Facts Facts; Build Build }`；`type Facts struct{ Mode, Provider, Model, PricingVersion, UsageID, QuoteID, TaskID, OriginalChargeID, PlateAssetID, PlateReferenceID, PlateSHA256 string; TaskSucceeded, PlateHashVerified, ChargeSeen bool }`；`type Build struct{ VCSRevision string; VCSModified bool }`。
  - `type Report`（含 `Axes []Axis`、`Verdicts`、`CandidateState`、`Limits []string`、`Facts ReportFacts`、`Build`、可选 `Review *Review`）；`type Output struct{ Composite []byte; Report Report }`。
  - `func FitPlateCover(plate []byte, w, h int) ([]byte, error)`、`func Compose(original, fittedPlate, mask []byte) ([]byte, error)`、`func Evaluate(in Input, fitted, composite []byte) Report`、`func Derive(in Input) Output`、`func StateOf(axes []Axis) string`、`func ReportJSON(Report) ([]byte, error)`、`func DecodeReport([]byte) (Report, error)`。

- [ ] **Step 1: 先写失败的测试**

测试里包含**阈值标定**（计划阶段要求）：用原图当底板、原图 ±3 噪声、仅 6 个灰阶之差的底色、单通道只差 12 的浅灰、只改了一半背景的底板这五类“假新背景”，必须全部判 `background_changed = FAIL`；白棚（250,250,250）、深蓝纯色、渐变这三类真实不同的背景必须 PASS。阈值只能收紧，不能放宽。

另有：每个 case 的主体像素用既有 `bgreplace.SubjectPixelChecks` **独立复核**；`Compose` 与既有 `LockSubject` 逐字节一致（在它拒绝的“背景没换”场景里 `Compose` 仍给出结果，让该情形成为一条 FAIL 记录而不是一个丢失的错误）；不可解码 / 错尺寸 / 空底板都变成 FAIL 轴而不是 panic 或 error；F22：`StateOf` 对单轴与两轴的**全部**组合做性质检查（`limited_candidate ⇔ 必需轴全 PASS`；任何 FAIL 压过一切；缺轴是 `pending`），`DecodeReport` 拒绝分数字段、浏览器 `passed`、被手改的状态 / 结论、把限制轴改成 PASS、`human_usefulness` 被改、尾随数据、非规范空白、把 FAIL 轴改写成 PASS。

`server/internal/platelock/platelock_test.go`

````go
package platelock_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	pl "github.com/bianjiefilm/product-image-engine/server/internal/platelock"
)

func loadSet(t *testing.T) *fs.Set {
	t.Helper()
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func caseByID(t *testing.T, set *fs.Set, id string) *fs.LoadedCase {
	t.Helper()
	for _, c := range set.Cases() {
		if c.CaseID == id {
			return c
		}
	}
	t.Fatalf("case %s missing", id)
	return nil
}

func solid(w, h int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return enc(img)
}

func enc(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// gradient is a deterministic, textured, clearly different plate.
func gradient(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*60/w), uint8(50 + y*90/h), uint8(110 + (x+y)*40/(w+h)), 255})
		}
	}
	return enc(img)
}

func inputFor(c *fs.LoadedCase, plate []byte) pl.Input {
	return pl.Input{
		Case: c.Case, Original: c.OriginalBytes, Mask: c.MaskBytes, CoverageSHA256: c.Evidence.CoverageSHA256, Plate: plate,
		Facts: pl.Facts{Mode: "background_plate_lock", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", PricingVersion: "test-v1",
			UsageID: "usage-1", QuoteID: "quote-1", TaskID: "task-1", OriginalChargeID: "charge-1",
			PlateAssetID: "asset-1", PlateReferenceID: "ref-1", PlateSHA256: "aa", TaskSucceeded: true, PlateHashVerified: true, ChargeSeen: true},
		Build: pl.Build{VCSRevision: "deadbeef", VCSModified: false},
	}
}

func axisState(t *testing.T, r pl.Report, name string) string {
	t.Helper()
	for _, a := range r.Axes {
		if a.Name == name {
			return a.State
		}
	}
	t.Fatalf("axis %s missing", name)
	return ""
}

// Calibration of bg-lock-thresholds/v1 against the three required references
// plus boundary plates. These numbers may only ever be tightened.
func TestThresholdCalibration(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	bg := color.NRGBA{226, 222, 214, 255} // the frozen carton background
	noisy := func() []byte {
		img, _ := png.Decode(bytes.NewReader(c.OriginalBytes))
		out := image.NewNRGBA(img.Bounds())
		for y := 0; y < 1024; y++ {
			for x := 0; x < 1024; x++ {
				p := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				d := uint8((x*7 + y*13) % 7) // 0..6, i.e. +-3 around 3
				p.R, p.G, p.B = clamp(int(p.R)+int(d)-3), clamp(int(p.G)+int(d)-3), clamp(int(p.B)+int(d)-3)
				out.SetNRGBA(x, y, p)
			}
		}
		return enc(out)
	}()
	half := func() []byte { // left half equals the original background, right half is far away
		img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
		for y := 0; y < 1024; y++ {
			for x := 0; x < 1024; x++ {
				if x < 512 {
					img.SetNRGBA(x, y, bg)
				} else {
					img.SetNRGBA(x, y, color.NRGBA{20, 60, 120, 255})
				}
			}
		}
		return enc(img)
	}()
	cases := []struct {
		name           string
		plate          []byte
		newPlate, back string
		state          string
	}{
		{"F07 plate equals the original", c.OriginalBytes, pl.Fail, pl.Fail, pl.CandidateNotUsable},
		{"F07 plate is the original with +-3 noise", noisy, pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"background colour only 6 away", solid(1024, 1024, color.NRGBA{232, 228, 220, 255}), pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"light grey that is 12 away on one channel", solid(1024, 1024, color.NRGBA{214, 214, 214, 255}), pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"only half the background changed", half, pl.Pass, pl.Fail, pl.CandidateNotUsable},
		{"white studio background", solid(1024, 1024, color.NRGBA{250, 250, 250, 255}), pl.Pass, pl.Pass, pl.CandidateLimited},
		{"deep blue solid", solid(1024, 1024, color.NRGBA{30, 60, 110, 255}), pl.Pass, pl.Pass, pl.CandidateLimited},
		{"textured gradient", gradient(1024, 1024), pl.Pass, pl.Pass, pl.CandidateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := pl.Derive(inputFor(c, tc.plate))
			if got := axisState(t, out.Report, pl.AxisNewPlate); got != tc.newPlate {
				t.Errorf("plate_is_new_background = %s, want %s", got, tc.newPlate)
			}
			if got := axisState(t, out.Report, pl.AxisBackground); got != tc.back {
				t.Errorf("background_changed = %s, want %s", got, tc.back)
			}
			if out.Report.CandidateState != tc.state {
				t.Errorf("candidate_state = %s, want %s", out.Report.CandidateState, tc.state)
			}
			if out.Composite == nil {
				t.Fatal("a failed background must still produce a record with its composite")
			}
			// Subject pixels are never what fails in these cases.
			for _, n := range []string{pl.AxisMaskPixels, pl.AxisLogo, pl.AxisText, pl.AxisSpec, pl.AxisStructure, pl.AxisProductCount, pl.AxisOutputSize, pl.AxisPlateSize} {
				if got := axisState(t, out.Report, n); got != pl.Pass {
					t.Errorf("%s = %s, want PASS", n, got)
				}
			}
		})
	}
}

func clamp(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func TestEveryCaseLimitedCandidateWithRealisticPlate(t *testing.T) {
	set := loadSet(t)
	for _, c := range set.Cases() {
		out := pl.Derive(inputFor(c, gradient(1024, 1024)))
		if out.Report.CandidateState != pl.CandidateLimited || len(out.Report.Limits) < 4 {
			t.Fatalf("%s: %s limits=%d axes=%+v", c.CaseID, out.Report.CandidateState, len(out.Report.Limits), out.Report.Axes)
		}
		if out.Report.Verdicts.Fidelity != pl.Pass || out.Report.Verdicts.Generation != pl.Pass || out.Report.Verdicts.Billing != pl.Pass || out.Report.Verdicts.HumanUsefulness != "NOT_RUN" {
			t.Fatalf("%s verdicts %+v", c.CaseID, out.Report.Verdicts)
		}
		wantTrans := pl.NA
		if c.MaterialClass == fs.MaterialTransparent {
			wantTrans = pl.Unknown
			if len(out.Report.Limits) != 5 {
				t.Fatalf("glass must carry the transmission limit: %v", out.Report.Limits)
			}
		}
		if got := axisState(t, out.Report, pl.AxisTransmission); got != wantTrans {
			t.Fatalf("%s transmission = %s", c.CaseID, got)
		}
		for _, n := range []string{pl.AxisExtraObjects, pl.AxisIntent, pl.AxisVisual} {
			if axisState(t, out.Report, n) != pl.Unknown {
				t.Fatalf("%s: %s must stay UNKNOWN", c.CaseID, n)
			}
		}
		// Independent proof that the composite really locks the subject.
		if _, err := bgreplace.SubjectPixelChecks(c.OriginalBytes, out.Composite, c.MaskBytes); err != nil {
			t.Fatalf("%s: %v", c.CaseID, err)
		}
		img, _ := png.Decode(bytes.NewReader(out.Composite))
		if img.Bounds().Dx() != c.Canvas.W || img.Bounds().Dy() != c.Canvas.H {
			t.Fatalf("%s composite %v", c.CaseID, img.Bounds())
		}
	}
}

func TestF06WrongPlateSizeIsNotUsableButStillRecorded(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	out := pl.Derive(inputFor(c, gradient(512, 512)))
	if axisState(t, out.Report, pl.AxisPlateSize) != pl.Fail || out.Report.CandidateState != pl.CandidateNotUsable {
		t.Fatalf("%+v", out.Report)
	}
	if out.Composite == nil || out.Report.Facts.CompositeSHA256 == "" || out.Report.Facts.OriginalChargeID != "charge-1" {
		t.Fatal("the record must keep the charge fact and the composite")
	}
}

func TestF08UndecodablePlateBytesAreFailedAxesNotErrors(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	for name, plate := range map[string][]byte{"not a PNG": []byte("GIF89a not a png"), "empty": nil, "truncated PNG": gradient(1024, 1024)[:200]} {
		out := pl.Derive(inputFor(c, plate))
		if out.Composite != nil || out.Report.CandidateState != pl.CandidateNotUsable {
			t.Fatalf("%s: composite=%v state=%s", name, out.Composite != nil, out.Report.CandidateState)
		}
		for _, n := range []string{pl.AxisMaskPixels, pl.AxisPlateSize, pl.AxisNewPlate, pl.AxisBackground} {
			if axisState(t, out.Report, n) != pl.Fail {
				t.Fatalf("%s: %s must FAIL", name, n)
			}
		}
		raw, err := pl.ReportJSON(out.Report)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pl.DecodeReport(raw); err != nil {
			t.Fatalf("%s: a failed report must still be a valid frozen report: %v", name, err)
		}
	}
}

func TestFitPlateCoverNeverStretches(t *testing.T) {
	plate := gradient(1024, 1024)
	src, _ := png.Decode(bytes.NewReader(plate))
	for _, tc := range []struct{ w, h int }{{1024, 1024}, {1024, 1280}, {768, 1024}} {
		raw, err := pl.FitPlateCover(plate, tc.w, tc.h)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil || img.Bounds().Dx() != tc.w || img.Bounds().Dy() != tc.h {
			t.Fatalf("%v: %v %v", tc, img.Bounds(), err)
		}
		if tc.w != 1024 || tc.h != 1024 {
			// 768x1024 is a pure centre crop: column 0 of the output is column 128 of the plate.
			if tc.w == 768 {
				for _, y := range []int{0, 300, 1023} {
					if color.NRGBAModel.Convert(img.At(0, y)) != color.NRGBAModel.Convert(src.At(128, y)) {
						t.Fatalf("768x1024 is not a centre crop at y=%d", y)
					}
				}
			}
		}
	}
	if _, err := pl.FitPlateCover([]byte("x"), 10, 10); err == nil {
		t.Fatal("garbage plate accepted")
	}
	if _, err := pl.FitPlateCover(plate, 0, 10); err == nil {
		t.Fatal("zero canvas accepted")
	}
}

func TestComposeMatchesLockSubjectWhenLockSubjectSucceeds(t *testing.T) {
	set := loadSet(t)
	for _, c := range set.Cases() {
		fitted, err := pl.FitPlateCover(gradient(1024, 1024), c.Canvas.W, c.Canvas.H)
		if err != nil {
			t.Fatal(err)
		}
		want, err := bgreplace.LockSubject(c.OriginalBytes, fitted, c.MaskBytes)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pl.Compose(c.OriginalBytes, fitted, c.MaskBytes)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: Compose differs from LockSubject (%v)", c.CaseID, err)
		}
	}
	// ...and where LockSubject refuses (plate == original) Compose still answers.
	c := set.Cases()[0]
	if _, err := bgreplace.LockSubject(c.OriginalBytes, c.OriginalBytes, c.MaskBytes); err == nil {
		t.Fatal("LockSubject should refuse an unchanged background")
	}
	if _, err := pl.Compose(c.OriginalBytes, c.OriginalBytes, c.MaskBytes); err != nil {
		t.Fatal(err)
	}
}

func TestF18DeriveIsDeterministic(t *testing.T) {
	set := loadSet(t)
	for _, id := range []string{"carton-1024x1024", "handled_metal-1024x1280", "glass_bottle-768x1024"} {
		c := caseByID(t, set, id)
		a, b := pl.Derive(inputFor(c, gradient(1024, 1024))), pl.Derive(inputFor(c, gradient(1024, 1024)))
		ra, _ := pl.ReportJSON(a.Report)
		rb, _ := pl.ReportJSON(b.Report)
		if !bytes.Equal(a.Composite, b.Composite) || !bytes.Equal(ra, rb) {
			t.Fatalf("%s: two derivations differ", id)
		}
	}
}

func TestSubjectTamperIsCaughtOnTheComposite(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	in := inputFor(c, gradient(1024, 1024))
	fitted, _ := pl.FitPlateCover(in.Plate, 1024, 1024)
	composite, _ := pl.Compose(in.Original, fitted, in.Mask)
	img, _ := png.Decode(bytes.NewReader(composite))
	bad := image.NewNRGBA(img.Bounds())
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			bad.Set(x, y, img.At(x, y))
		}
	}
	r := c.Coverage.PackagingText[0]
	bad.SetNRGBA(r[0]+5, r[1]+5, color.NRGBA{1, 2, 3, 255}) // one pixel of the brand text altered
	rep := pl.Evaluate(in, fitted, enc(bad))
	if axisState(t, rep, pl.AxisMaskPixels) != pl.Fail || axisState(t, rep, pl.AxisText) != pl.Fail || rep.CandidateState != pl.CandidateNotUsable || rep.Verdicts.Fidelity != pl.Fail {
		t.Fatalf("a single altered text pixel must fail fidelity: %+v", rep.Axes)
	}
	if axisState(t, rep, pl.AxisLogo) != pl.Pass {
		t.Fatal("untouched logo axis must stay PASS (axes do not collapse)")
	}
}

func TestF22StateOfProperty(t *testing.T) {
	names := []string{pl.AxisMaskPixels, pl.AxisLogo, pl.AxisText, pl.AxisSpec, pl.AxisStructure, pl.AxisProductCount, pl.AxisOutputSize, pl.AxisPlateSize, pl.AxisNewPlate, pl.AxisBackground, pl.AxisTask, pl.AxisAssetHash, pl.AxisCharged}
	states := []string{pl.Pass, pl.Fail, pl.Unknown}
	// All 3^13 combinations would be 1.6M; sweep every single-axis deviation from
	// all-PASS plus every pair, which covers each axis in each position.
	build := func(over map[int]string) []pl.Axis {
		axes := make([]pl.Axis, 0, len(names)+4)
		for i, n := range names {
			st := pl.Pass
			if s, ok := over[i]; ok {
				st = s
			}
			axes = append(axes, pl.Axis{Name: n, State: st, MachineChecked: true})
		}
		for _, n := range []string{pl.AxisExtraObjects, pl.AxisIntent, pl.AxisVisual, pl.AxisTransmission} {
			axes = append(axes, pl.Axis{Name: n, State: pl.Unknown})
		}
		return axes
	}
	want := func(over map[int]string) string {
		anyFail, anyOther := false, false
		for _, s := range over {
			if s == pl.Fail {
				anyFail = true
			} else if s != pl.Pass {
				anyOther = true
			}
		}
		switch {
		case anyFail:
			return pl.CandidateNotUsable
		case anyOther:
			return pl.CandidatePending
		}
		return pl.CandidateLimited
	}
	if pl.StateOf(build(nil)) != pl.CandidateLimited {
		t.Fatal("all PASS must be a limited candidate")
	}
	for i := range names {
		for _, a := range states {
			over := map[int]string{i: a}
			if got := pl.StateOf(build(over)); got != want(over) {
				t.Fatalf("single %s=%s: got %s want %s", names[i], a, got, want(over))
			}
			for j := i + 1; j < len(names); j++ {
				for _, b := range states {
					over := map[int]string{i: a, j: b}
					if got := pl.StateOf(build(over)); got != want(over) {
						t.Fatalf("%s=%s %s=%s: got %s want %s", names[i], a, names[j], b, got, want(over))
					}
				}
			}
		}
	}
	// A missing required axis can never be a candidate.
	if pl.StateOf(build(nil)[1:]) != pl.CandidatePending {
		t.Fatal("a report without a required axis must be pending")
	}
}

func TestF22DecodeReportRejectsScoresAndInconsistentDocuments(t *testing.T) {
	set := loadSet(t)
	c := caseByID(t, set, "carton-1024x1024")
	good := pl.Derive(inputFor(c, gradient(1024, 1024))).Report
	raw, err := pl.ReportJSON(good)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := pl.DecodeReport(raw); err != nil || got.CandidateState != pl.CandidateLimited {
		t.Fatal(got.CandidateState, err)
	}
	mutations := map[string]func(string) string{
		"a similarity score field": func(s string) string {
			return strings.Replace(s, `"candidate_state"`, `"similarity_score":0.99,"candidate_state"`, 1)
		},
		"a quality score field":           func(s string) string { return strings.Replace(s, `"build"`, `"score":98,"build"`, 1) },
		"a browser supplied passed claim": func(s string) string { return strings.Replace(s, `"build"`, `"passed":true,"build"`, 1) },
		"state flipped to limited by hand": func(s string) string {
			return strings.Replace(s, `"candidate_state":"limited_candidate"`, `"candidate_state":"pending"`, 1)
		},
		"a limit axis forced to PASS": func(s string) string {
			return strings.Replace(s, `"name":"visual_composite_quality","state":"UNKNOWN"`, `"name":"visual_composite_quality","state":"PASS"`, 1)
		},
		"human usefulness claimed": func(s string) string {
			return strings.Replace(s, `"human_usefulness":"NOT_RUN"`, `"human_usefulness":"PASS"`, 1)
		},
		"trailing data":            func(s string) string { return s + "{}" },
		"non canonical whitespace": func(s string) string { return strings.Replace(s, `{"report_version"`, "{ \"report_version\"", 1) },
		"a FAIL axis hidden as PASS": func(s string) string {
			return strings.Replace(s, `"state":"PASS","machine_checked":true,"evidence":"protected=`, `"state":"FAIL","machine_checked":true,"evidence":"protected=`, 1)
		},
	}
	for name, mut := range mutations {
		if _, err := pl.DecodeReport([]byte(mut(string(raw)))); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A failing report cannot be laundered by flipping its FAIL axis to PASS.
	bad := pl.Derive(inputFor(c, gradient(512, 512))).Report
	badRaw, _ := pl.ReportJSON(bad)
	if _, err := pl.DecodeReport(badRaw); err != nil {
		t.Fatal(err)
	}
	laundered := strings.Replace(string(badRaw), `"name":"plate_size","state":"FAIL"`, `"name":"plate_size","state":"PASS"`, 1)
	if laundered == string(badRaw) {
		t.Fatal("test setup: plate_size FAIL not found")
	}
	if _, err := pl.DecodeReport([]byte(laundered)); err == nil {
		t.Error("a FAIL axis rewritten as PASS was accepted")
	}
}
````

- [ ] **Step 2: 运行，确认失败**

```bash
cd "$WT/server" && go test -count=1 ./internal/platelock/... 2>&1 | tail -8
```

期望：`FAIL … [build failed]`，原因 `undefined: pl.Derive` 等（包里还没有实现文件）。

- [ ] **Step 3: 写实现**

适配与合成（`decode` 一律转成零原点的 `*image.NRGBA`，后面所有像素循环都是直接索引——比走 `image.Image` 接口快一个数量级，单个 `Derive` 约 0.25 秒）：

`server/internal/platelock/fit.go`

````go
// Package platelock turns a verified model background plate plus a frozen
// sample into a derived composite and a server-side quality report. It is pure:
// no I/O, no clocks, no randomness, and no input that could carry a score or a
// "passed" claim from the browser.
package platelock

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

const (
	// FitID names the canvas adaptation recorded in every report.
	FitID = "fit-cover-center-catmullrom/v1"
	// ComposeID names the subject lock recorded in every report.
	ComposeID = "compose-mask-lock/v1"
	// PlateSide is the only plate size the priced SKU produces.
	PlateSide = 1024
)

var ErrImage = errors.New("platelock: image cannot be decoded as PNG")

// decode returns a zero-origin NRGBA copy so every pixel loop is a direct index.
func decode(raw []byte) (*image.NRGBA, error) {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrImage
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	return out, nil
}

func encode(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FitPlateCover scales the plate uniformly until it covers w x h and crops the
// centre. It never stretches: a 1024x1024 plate on a 1024x1280 canvas becomes
// 1280x1280 (CatmullRom) then loses 128 columns on each side; on 768x1024 it is
// only cropped. The result is a PNG exactly w x h.
func FitPlateCover(plate []byte, w, h int) ([]byte, error) {
	if w < 1 || h < 1 || w > 4096 || h > 4096 {
		return nil, ErrImage
	}
	src, err := decode(plate)
	if err != nil {
		return nil, err
	}
	pw, ph := src.Bounds().Dx(), src.Bounds().Dy()
	if pw < 1 || ph < 1 {
		return nil, ErrImage
	}
	var sw, sh int
	if w*ph >= h*pw { // width drives the scale
		sw, sh = w, (ph*w+pw-1)/pw
	} else {
		sh, sw = h, (pw*h+ph-1)/ph
	}
	var scaled *image.NRGBA
	if sw == pw && sh == ph {
		scaled = src
	} else {
		scaled = image.NewNRGBA(image.Rect(0, 0, sw, sh))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	}
	x0, y0 := (sw-w)/2, (sh-h)/2
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.SetNRGBA(x, y, scaled.NRGBAAt(x0+x, y0+y))
		}
	}
	return encode(out)
}

// Compose copies original pixels where the mask protects them (alpha >= 128)
// and plate pixels elsewhere. It is bgreplace.LockSubject without its two
// refusals, because "no background change" and "no protected pixel" must be
// reported as failed axes with a record, not swallowed as an error.
func Compose(original, fittedPlate, mask []byte) ([]byte, error) {
	o, err := decode(original)
	if err != nil {
		return nil, err
	}
	p, err := decode(fittedPlate)
	if err != nil {
		return nil, err
	}
	m, err := decode(mask)
	if err != nil {
		return nil, err
	}
	b := o.Bounds()
	if p.Bounds().Dx() != b.Dx() || p.Bounds().Dy() != b.Dy() || m.Bounds().Dx() != b.Dx() || m.Bounds().Dy() != b.Dy() {
		return nil, ErrImage
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if m.NRGBAAt(x, y).A >= 128 {
				out.SetNRGBA(x, y, o.NRGBAAt(x, y))
			} else {
				out.SetNRGBA(x, y, p.NRGBAAt(x, y))
			}
		}
	}
	return encode(out)
}
````

报告、轴、状态与裁决（`StateOf` 是**唯一**决定候选状态的地方；限制轴只会是 `UNKNOWN` / `N/A`，`DecodeReport` 会拒绝任何把它们判成 PASS / FAIL 的文档）：

`server/internal/platelock/report.go`

````go
package platelock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"

	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
)

const (
	ReportVersion = "product-source-quality/v2"
	Claim         = fs.Scope

	Pass, Fail, Unknown, NA = "PASS", "FAIL", "UNKNOWN", "N/A"

	CandidateLimited   = "limited_candidate"
	CandidatePending   = "pending"
	CandidateNotUsable = "not_usable_candidate"

	// bg-lock-thresholds/v1. They may only be tightened, with evidence, and
	// every change needs a new fs.ThresholdsVersion.
	ChangedChannelDelta     = 16  // a pixel counts as changed at >= this per-channel difference
	MinChangedPermille      = 800 // >= 80.0% of background pixels must be changed
	MinMeanAbsDiffPerChanel = 12  // mean absolute RGB difference over the background
	minComponentArea        = 64
)

// Machine-checked axes. Every one must be PASS for a limited candidate.
const (
	AxisMaskPixels   = "mask_pixels_identical"
	AxisLogo         = "coverage_logo"
	AxisText         = "coverage_packaging_text"
	AxisSpec         = "coverage_spec"
	AxisStructure    = "coverage_structure"
	AxisProductCount = "product_count"
	AxisOutputSize   = "output_size"
	AxisPlateSize    = "plate_size"
	AxisNewPlate     = "plate_is_new_background"
	AxisBackground   = "background_changed"
	AxisTask         = "plate_task_succeeded"
	AxisAssetHash    = "plate_asset_hash_verified"
	AxisCharged      = "plate_charged_observed"
)

// Limit axes cannot be machine checked: they are only ever UNKNOWN or N/A.
const (
	AxisExtraObjects = "background_extra_objects"
	AxisIntent       = "background_intent_followed"
	AxisVisual       = "visual_composite_quality"
	AxisTransmission = "transmission_edge"
)

var subjectAxes = []string{AxisMaskPixels, AxisLogo, AxisText, AxisSpec, AxisStructure, AxisProductCount}
var requiredAxes = append(append([]string{}, subjectAxes...), AxisOutputSize, AxisPlateSize, AxisNewPlate, AxisBackground, AxisTask, AxisAssetHash, AxisCharged)
var limitAxes = []string{AxisExtraObjects, AxisIntent, AxisVisual, AxisTransmission}

type Axis struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	MachineChecked bool   `json:"machine_checked"`
	Evidence       string `json:"evidence"`
}
type Scope struct {
	Mode           string `json:"mode"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	PricingVersion string `json:"pricing_version"`
	SampleSetID    string `json:"sample_set_id"`
	CaseID         string `json:"case_id"`
	Canvas         string `json:"canvas"`
	MaterialClass  string `json:"material_class"`
	Claim          string `json:"claim"`
}
type Verdicts struct {
	Fidelity        string `json:"fidelity"`
	Generation      string `json:"generation"`
	Billing         string `json:"billing"`
	HumanUsefulness string `json:"human_usefulness"`
}
type PlateFacts struct {
	AssetID     string `json:"asset_id"`
	ReferenceID string `json:"reference_id"`
	SHA256      string `json:"sha256"`
}
type ReportFacts struct {
	UsageID          string     `json:"usage_id"`
	QuoteID          string     `json:"quote_id"`
	TaskID           string     `json:"task_id"`
	OriginalChargeID string     `json:"original_charge_id"`
	Plate            PlateFacts `json:"plate"`
	OriginalSHA256   string     `json:"original_sha256"`
	MaskSHA256       string     `json:"mask_sha256"`
	CoverageSHA256   string     `json:"coverage_sha256"`
	CompositeSHA256  string     `json:"composite_sha256"`
	FitID            string     `json:"fit_id"`
	ComposeID        string     `json:"compose_id"`
	ThresholdsVer    string     `json:"thresholds_version"`
}
type Build struct {
	VCSRevision string `json:"vcs_revision"`
	VCSModified bool   `json:"vcs_modified"`
}

// Review is an optional pointer to a SIMULATED-HUMAN review. It is carried for
// reference only and never read by StateOf.
type Review struct {
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256"`
}
type Report struct {
	ReportVersion  string      `json:"report_version"`
	Scope          Scope       `json:"scope"`
	Axes           []Axis      `json:"axes"`
	Verdicts       Verdicts    `json:"verdicts"`
	CandidateState string      `json:"candidate_state"`
	Limits         []string    `json:"limits"`
	Facts          ReportFacts `json:"facts"`
	Build          Build       `json:"build"`
	Review         *Review     `json:"review,omitempty"`
}

// Input holds only server-held facts. There is deliberately no field that can
// carry a score, a browser boolean, or a supplier authorization.
type Input struct {
	Case           fs.Case
	Original       []byte // frozen original from the loaded set
	Mask           []byte // frozen mask from the loaded set
	CoverageSHA256 string // evidence digest from fidelitysamples.LoadedCase
	Plate          []byte // model plate bytes downloaded from the verified Upload asset
	Facts          Facts
	Build          Build
}

// Facts are the run facts the derivation trigger already established.
type Facts struct {
	Mode, Provider, Model, PricingVersion        string
	UsageID, QuoteID, TaskID, OriginalChargeID   string
	PlateAssetID, PlateReferenceID, PlateSHA256  string
	TaskSucceeded, PlateHashVerified, ChargeSeen bool
}

func sha(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func compositeSHA(raw []byte) string {
	if raw == nil {
		return ""
	}
	return sha(raw)
}

func axis(name, state string, machine bool, evidence string) Axis {
	return Axis{Name: name, State: state, MachineChecked: machine, Evidence: evidence}
}
func pf(ok bool) string {
	if ok {
		return Pass
	}
	return Fail
}
func known(ok bool) string {
	if ok {
		return Pass
	}
	return Unknown
}

// StateOf is the only place a candidate state is decided. A FAIL on any
// required axis wins over everything; anything not PASS is pending.
func StateOf(axes []Axis) string {
	byName := map[string]string{}
	for _, a := range axes {
		byName[a.Name] = a.State
	}
	for _, name := range requiredAxes {
		if byName[name] == Fail {
			return CandidateNotUsable
		}
	}
	for _, name := range requiredAxes {
		if byName[name] != Pass {
			return CandidatePending
		}
	}
	return CandidateLimited
}

func verdicts(axes []Axis) Verdicts {
	by := map[string]string{}
	for _, a := range axes {
		by[a.Name] = a.State
	}
	all := func(names ...string) string {
		out := Pass
		for _, n := range names {
			switch by[n] {
			case Fail:
				return Fail
			case Pass:
			default:
				out = Unknown
			}
		}
		return out
	}
	return Verdicts{Fidelity: all(subjectAxes...), Generation: all(AxisTask, AxisAssetHash), Billing: all(AxisCharged), HumanUsefulness: "NOT_RUN"}
}

func limitsFor(c fs.Case) []string {
	out := []string{
		"已验证范围仅限冻结合成样本（纸盒/金属把手杯/玻璃瓶），不代表任意商品照片",
		"样本为硬边渲染，未覆盖真实照片的边缘羽化与光晕",
		"背景内容未经机器检查：可能出现多余物体，也可能与所写方向不符",
		"整体观感未经人工评审",
	}
	if c.MaterialClass == fs.MaterialTransparent {
		out = append(out, "玻璃透射与接触阴影不可机检：主体像素被锁定，但透射效果不保证")
	}
	return out
}

// Evaluate measures a composite against the frozen sample. It never returns an
// error: undecodable or mis-sized inputs become FAIL axes on the record.
func Evaluate(in Input, fitted, composite []byte) Report {
	c := in.Case
	axes := []Axis{}
	mask, merr := decode(in.Mask)
	orig, oerr := decode(in.Original)
	comp, cerr := decode(composite)
	haveSubject := merr == nil && oerr == nil && cerr == nil &&
		comp.Bounds().Dx() == c.Canvas.W && comp.Bounds().Dy() == c.Canvas.H &&
		orig.Bounds().Dx() == c.Canvas.W && orig.Bounds().Dy() == c.Canvas.H &&
		mask.Bounds().Dx() == c.Canvas.W && mask.Bounds().Dy() == c.Canvas.H

	protected := func(x, y int) bool { return mask.NRGBAAt(x, y).A >= 128 }
	same := func(x, y int) bool { return orig.NRGBAAt(x, y) == comp.NRGBAAt(x, y) }
	if haveSubject {
		prot, bad := 0, 0
		for y := 0; y < c.Canvas.H; y++ {
			for x := 0; x < c.Canvas.W; x++ {
				if protected(x, y) {
					prot++
					if !same(x, y) {
						bad++
					}
				}
			}
		}
		axes = append(axes, axis(AxisMaskPixels, pf(prot > 0 && bad == 0), true, fmt.Sprintf("protected=%d mismatched=%d", prot, bad)))
	} else {
		axes = append(axes, axis(AxisMaskPixels, Fail, true, "composite, original or mask unusable or not the case canvas"))
	}
	regionAxis := func(name string, regions []fs.Rect) Axis {
		if !haveSubject {
			return axis(name, Fail, true, "subject unusable")
		}
		inside, total := 0, 0
		for _, r := range regions {
			for y := r[1]; y < r[3]; y++ {
				for x := r[0]; x < r[2]; x++ {
					total++
					if x >= 0 && y >= 0 && x < c.Canvas.W && y < c.Canvas.H && protected(x, y) && same(x, y) {
						inside++
					}
				}
			}
		}
		return axis(name, pf(total > 0 && inside == total), true, fmt.Sprintf("regions=%d pixels=%d preserved=%d", len(regions), total, inside))
	}
	axes = append(axes,
		regionAxis(AxisLogo, c.Coverage.Logo), regionAxis(AxisText, c.Coverage.PackagingText),
		regionAxis(AxisSpec, c.Coverage.Spec), regionAxis(AxisStructure, c.Coverage.Structure))
	if merr == nil {
		n := components(mask)
		axes = append(axes, axis(AxisProductCount, pf(n == c.ProductCount), true, fmt.Sprintf("components=%d expected=%d", n, c.ProductCount)))
	} else {
		axes = append(axes, axis(AxisProductCount, Fail, true, "mask unusable"))
	}
	axes = append(axes, axis(AxisOutputSize, pf(cerr == nil && comp.Bounds().Dx() == c.Canvas.W && comp.Bounds().Dy() == c.Canvas.H), true, fmt.Sprintf("canvas=%dx%d", c.Canvas.W, c.Canvas.H)))
	plate, perr := decode(in.Plate)
	plateOK := perr == nil && plate.Bounds().Dx() == PlateSide && plate.Bounds().Dy() == PlateSide
	if perr != nil {
		axes = append(axes, axis(AxisPlateSize, Fail, true, "plate is not a PNG"))
	} else {
		axes = append(axes, axis(AxisPlateSize, pf(plateOK), true, fmt.Sprintf("plate=%dx%d expected=%dx%d", plate.Bounds().Dx(), plate.Bounds().Dy(), PlateSide, PlateSide)))
	}
	fit, ferr := decode(fitted)
	if haveSubject && ferr == nil && fit.Bounds().Dx() == c.Canvas.W && fit.Bounds().Dy() == c.Canvas.H {
		u, changed, sum := 0, 0, 0
		for y := 0; y < c.Canvas.H; y++ {
			for x := 0; x < c.Canvas.W; x++ {
				if protected(x, y) {
					continue
				}
				u++
				o, p := orig.NRGBAAt(x, y), fit.NRGBAAt(x, y)
				dr, dg, db := absDiff(o.R, p.R), absDiff(o.G, p.G), absDiff(o.B, p.B)
				sum += dr + dg + db
				if max(dr, dg, db) >= ChangedChannelDelta {
					changed++
				}
			}
		}
		anyDiff := sum > 0
		newPlate := sha(in.Plate) != sha(in.Original) && anyDiff
		axes = append(axes, axis(AxisNewPlate, pf(newPlate), true, fmt.Sprintf("plate_sha_differs=%v background_pixels_differ=%v", sha(in.Plate) != sha(in.Original), anyDiff)))
		ok := u > 0 && changed*1000 >= MinChangedPermille*u && sum >= MinMeanAbsDiffPerChanel*u*3
		axes = append(axes, axis(AxisBackground, pf(ok), true, fmt.Sprintf("background=%d changed=%d mean_abs_x1000=%d", u, changed, sum*1000/max(u*3, 1))))
	} else {
		axes = append(axes, axis(AxisNewPlate, Fail, true, "fitted plate unusable"), axis(AxisBackground, Fail, true, "fitted plate unusable"))
	}
	axes = append(axes,
		axis(AxisTask, known(in.Facts.TaskSucceeded), true, "task_id="+in.Facts.TaskID),
		axis(AxisAssetHash, known(in.Facts.PlateHashVerified), true, "plate_sha256="+in.Facts.PlateSHA256),
		axis(AxisCharged, known(in.Facts.ChargeSeen), true, "original_charge_id="+in.Facts.OriginalChargeID))
	transmission := NA
	if c.MaterialClass == fs.MaterialTransparent {
		transmission = Unknown
	}
	axes = append(axes,
		axis(AxisExtraObjects, Unknown, false, "not machine checkable"),
		axis(AxisIntent, Unknown, false, "not machine checkable"),
		axis(AxisVisual, Unknown, false, "not machine checkable"),
		axis(AxisTransmission, transmission, false, "not machine checkable"))
	state := StateOf(axes)
	rep := Report{
		ReportVersion: ReportVersion,
		Scope: Scope{Mode: in.Facts.Mode, Provider: in.Facts.Provider, Model: in.Facts.Model, PricingVersion: in.Facts.PricingVersion,
			SampleSetID: fs.SetID, CaseID: c.CaseID, Canvas: fmt.Sprintf("%dx%d", c.Canvas.W, c.Canvas.H), MaterialClass: c.MaterialClass, Claim: Claim},
		Axes: axes, Verdicts: verdicts(axes), CandidateState: state, Limits: []string{},
		Facts: ReportFacts{
			UsageID: in.Facts.UsageID, QuoteID: in.Facts.QuoteID, TaskID: in.Facts.TaskID, OriginalChargeID: in.Facts.OriginalChargeID,
			Plate:          PlateFacts{AssetID: in.Facts.PlateAssetID, ReferenceID: in.Facts.PlateReferenceID, SHA256: in.Facts.PlateSHA256},
			OriginalSHA256: sha(in.Original), MaskSHA256: sha(in.Mask), CoverageSHA256: in.CoverageSHA256, CompositeSHA256: compositeSHA(composite),
			FitID: FitID, ComposeID: ComposeID, ThresholdsVer: fs.ThresholdsVersion,
		},
		Build: in.Build,
	}
	if state == CandidateLimited {
		rep.Limits = limitsFor(c)
	}
	return rep
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func components(img *image.NRGBA) int {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	prot := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			prot[y*w+x] = img.NRGBAAt(x, y).A >= 128
		}
	}
	count := 0
	var stack []int
	for start := range prot {
		if !prot[start] {
			continue
		}
		area := 0
		prot[start] = false
		stack = append(stack[:0], start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			area++
			x, y := p%w, p/w
			for _, n := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if n[0] < 0 || n[1] < 0 || n[0] >= w || n[1] >= h {
					continue
				}
				if q := n[1]*w + n[0]; prot[q] {
					prot[q] = false
					stack = append(stack, q)
				}
			}
		}
		if area >= minComponentArea {
			count++
		}
	}
	return count
}

// ReportJSON is the canonical byte form that gets hashed and frozen.
func ReportJSON(r Report) ([]byte, error) { return json.Marshal(r) }

// DecodeReport accepts only a canonical, internally consistent report: no
// unknown field (so no score), no duplicate axis, every state recomputed.
func DecodeReport(raw []byte) (Report, error) {
	var r Report
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return Report{}, fmt.Errorf("platelock: report: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return Report{}, fmt.Errorf("platelock: report: trailing data")
	}
	if r.ReportVersion != ReportVersion || r.Scope.Claim != Claim || r.Verdicts.HumanUsefulness != "NOT_RUN" {
		return Report{}, fmt.Errorf("platelock: report: header")
	}
	seen := map[string]bool{}
	want := map[string]bool{}
	for _, n := range requiredAxes {
		want[n] = true
	}
	for _, n := range limitAxes {
		want[n] = false
	}
	for _, a := range r.Axes {
		machine, ok := want[a.Name]
		if !ok || seen[a.Name] || a.MachineChecked != machine {
			return Report{}, fmt.Errorf("platelock: report: axis %q", a.Name)
		}
		seen[a.Name] = true
		switch a.State {
		case Pass, Fail, Unknown, NA:
		default:
			return Report{}, fmt.Errorf("platelock: report: axis %q state %q", a.Name, a.State)
		}
		if !machine && (a.State == Pass || a.State == Fail) {
			return Report{}, fmt.Errorf("platelock: report: axis %q cannot be machine decided", a.Name)
		}
	}
	if len(seen) != len(want) {
		return Report{}, fmt.Errorf("platelock: report: axes incomplete")
	}
	if r.CandidateState != StateOf(r.Axes) || r.Verdicts != verdicts(r.Axes) {
		return Report{}, fmt.Errorf("platelock: report: state does not follow from axes")
	}
	if (r.CandidateState == CandidateLimited) != (len(r.Limits) > 0) {
		return Report{}, fmt.Errorf("platelock: report: limits")
	}
	if canonical, err := ReportJSON(r); err != nil || !bytes.Equal(canonical, raw) {
		return Report{}, fmt.Errorf("platelock: report: not canonical")
	}
	return r, nil
}

// Output is what a derivation produces.
type Output struct {
	Composite []byte // nil when the plate could not be fitted or composed
	Report    Report
}

// Derive runs fit, compose and evaluate. Deterministic for equal inputs on one
// build; the stored record, not a recomputation, is the authority afterwards.
func Derive(in Input) Output {
	fitted, ferr := FitPlateCover(in.Plate, in.Case.Canvas.W, in.Case.Canvas.H)
	var composite []byte
	if ferr == nil {
		composite, _ = Compose(in.Original, fitted, in.Mask)
	}
	return Output{Composite: composite, Report: Evaluate(in, fitted, composite)}
}
````

- [ ] **Step 4: 运行，确认通过**

```bash
cd "$WT/server" && gofmt -l internal/platelock; go vet ./internal/platelock
"$RUN/bin/heavy" -- go test -count=1 -v ./internal/platelock/... 2>&1 | grep -E "^(--- |ok|FAIL|PASS)" | head -30
```

期望：`TestThresholdCalibration`（8 个子用例）、`TestEveryCaseLimitedCandidateWithRealisticPlate`、`TestF06…`、`TestF08…`、`TestFitPlateCoverNeverStretches`、`TestComposeMatchesLockSubjectWhenLockSubjectSucceeds`、`TestF18DeriveIsDeterministic`、`TestSubjectTamperIsCaughtOnTheComposite`、`TestF22…`（2 个）全部 `PASS`，包 `ok`（整包约 18–35 秒）。

- [ ] **Step 5: 竞态检查（纯函数，但并发读共享字节切片不得被写）**

```bash
cd "$WT/server" && "$RUN/bin/heavy" -- go test -count=1 -race -run 'F18|F22|Compose|Fit' ./internal/platelock/ 2>&1 | tail -3     # 期望：ok
```

- [ ] **Step 6: 提交**

```bash
cd "$WT" && git add server/internal/platelock && git commit -m "feat(platelock): derive and judge a locked-subject composite from a paid plate

等比覆盖适配（禁止拉伸）、蒙版内像素逐点锁回、product-source-quality/v2
逐项报告（PASS/FAIL/UNKNOWN/N/A 不折叠，无分数入口，StateOf 唯一裁决）。
阈值 bg-lock-thresholds/v1 用五类假背景与三类真背景标定。

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


---

## Task 3：`background_plate_lock` 模式的 run 流水线——冻结输入、写一次的派生、恢复与门控

**交付物：** source run 新增 `background_plate_lock` 模式：创建时冻结服务端派生的输入（样本 / 蒙版 / 覆盖摘要 / 背景文字 / 适配与合成标识）并绑进 `Intent.PlateDigest`；底板 Task 成功且已扣费后，**恢复协调器或显式 reconcile** 触发一次幂等派生（下载并核对底板 → `platelock.Derive` → 写一次的派生记录）；选定被 `limited_candidate` 门控；plate 的 Upload 资产永不作为结果展示。对应 spec §4.4 第 1–5 点、§4.5 的落库部分；反例 F06–F10（服务层）、F15、F17（按 P1 改写）、F18。

**Files:**
- Create: `server/internal/sourceimage/plate.go`、`plate_test.go`
- Create: `server/internal/store/migrations/0020_plate_lock.sql`、`server/internal/store/plate.go`、`plate_test.go`
- Create: `server/internal/sourceflow/plate.go`、`plate_test.go`、`plate_io_test.go`
- Modify: `server/internal/sourceimage/{types.go,validate.go,quality.go}`、`server/internal/store/{source_runs.go,source_http.go}`、`server/internal/sourceflow/{types.go,quote.go,recover.go,task.go,output.go}`

**Interfaces:**
- Consumes（Task 1 / 2）：`fidelitysamples.Set`（`BySHA/ID/SHA256`）、`fidelitysamples.LoadedCase`、`fidelitysamples.ValidIntent`、`sampletest.Dir/Copy/Resign`；`platelock.Derive/Input/Facts/Build/ReportJSON/DecodeReport/FitID/ComposeID`；既有 `bgreplace.ModelPlatePrompt`、`bgreplace.SubjectPixelChecks`、`bgreplace.LockSubject`。
- Produces（Task 4 精确使用）：
  - `sourceimage`：`ModeTextGenerate`、`ModePlateLock`（= `"background_plate_lock"`）、`ErrNotFrozen`、`ErrNotUsable`、`Intent.PlateDigest string`（`json:",omitempty"`，旧行 JSON 字节不变）、`PlateInput`、`PlateDigest(PlateInput) string`、`ValidIntentText(string) bool`、`PlateDerivation`、`ValidatePlateDerivation`、`SHA256Hex`、状态常量 `DerivationDerived/Blocked`、`CandidateLimited/NotUsable/Pending/Blocked`、`BlockedSampleSetChanged`。
  - `store`：`(*Store).CreateSourcePlateRunForProject(ctx, si.Intent, si.PlateInput) (si.Run, bool, error)`、`GetPlateInput(ctx, si.Run) (si.PlateInput, error)`、`GetPlateDerivation(ctx, runID) (si.PlateDerivation, bool, error)`、`SavePlateDerivation(ctx, si.PlateDerivation) (si.PlateDerivation, error)`、`ListPlateRunsAwaitingDerivation(ctx, limit) ([]si.Run, error)`；`createSourceRun` 增加可选的同事务 `extra` 钩子。
  - `sourceflow.Service`：字段 `PlateEnabled bool`、`Samples *fidelitysamples.Set`、`SamplesReason string`、`Build platelock.Build`；`PlateRequest{RequestKey, InputID, BackgroundIntent, OriginalSHA256 string}`；`(*Service).PlateReady(payerSource string) (bool, string)`（原因码 `plate_lock_disabled | sample_set_unconfigured | sample_set_invalid | plate_org_not_supported | source_unconfigured`）、`CreatePlate(ctx, Actor, project string, PlateRequest) (si.Run, error)`、`DerivePlate(ctx, runID) (si.PlateDerivation, error)`、`LoadPlate(ctx, Actor, project, runID string) (PlateContent, error)`（`PlateContent{Run; Input; Derivation}`）；`Select` 对 plate 模式要求 `limited_candidate`（未派生 → `ErrConflict`，不可用 → `ErrNotUsable`）；`OpenOutput` 对 plate 模式恒 `ErrConflict`；`Confirm` 在 flag 关闭时拒绝**未确认**的 plate 报价（已确认的重放照常）。

- [ ] **Step 1: 先写失败的测试**

`sourceimage` 层（含**黄金摘要**：旧 `text_generate` 意图的指纹 `a4233dcb…4130` 是用改动**之前**的代码算出的，若变了，所有已落库的文本 run 都会读出 `ErrInvariant`）：

`server/internal/sourceimage/plate_test.go`

````go
package sourceimage

import (
	"strings"
	"testing"
)

func legacyIntent() Intent {
	return Intent{Scope: Scope{AppID: "product-image", TenantID: "acct_u", ProjectID: "prj_a", UserID: "usr_u", PrincipalAccountID: "acct_u", PayerAccountID: "acct_u"}, RequestKey: "request-a", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "蓝纸盒", PricingVersion: "test-price-v1", Quantity: 1}
}

func frozenPlate() PlateInput {
	h := strings.Repeat("a", 64)
	return PlateInput{CaseID: "carton-1024x1024", SetID: "frozen-sample-set", SetSHA256: h, InputID: "pin_a", OriginalSHA256: strings.Repeat("b", 64), MaskSHA256: strings.Repeat("c", 64), CoverageSHA256: strings.Repeat("d", 64), CanvasW: 1024, CanvasH: 1024, BackgroundIntent: "深蓝色背景", FitID: "fit-cover-center-catmullrom/v1", ComposeID: "compose-mask-lock/v1", ThresholdsVersion: "bg-lock-thresholds/v1"}
}

// This digest was produced by the code BEFORE the plate-lock mode existed. If it
// changes, every persisted text_generate run would fail its fingerprint check.
const legacyFingerprint = "a4233dcbcddd62323656cb34489b0b04e398f111191997f29eecc74073534130"

func TestLegacyTextIntentFingerprintIsByteStable(t *testing.T) {
	fp, err := Fingerprint(legacyIntent())
	if err != nil || fp != legacyFingerprint {
		t.Fatalf("legacy fingerprint changed: %s %v", fp, err)
	}
}

func TestFingerprintModeRules(t *testing.T) {
	plate := legacyIntent()
	plate.Mode, plate.Prompt, plate.PlateDigest = ModePlateLock, "只生成背景", PlateDigest(frozenPlate())
	if fp, err := Fingerprint(plate); err != nil || fp == legacyFingerprint {
		t.Fatalf("a plate run has its own fingerprint: %s %v", fp, err)
	}
	for name, mutate := range map[string]func(*Intent){
		"text mode carrying a plate digest":    func(i *Intent) { i.Mode, i.PlateDigest = ModeTextGenerate, PlateDigest(frozenPlate()) },
		"plate mode without a digest":          func(i *Intent) { i.PlateDigest = "" },
		"plate mode with a short digest":       func(i *Intent) { i.PlateDigest = "abc" },
		"plate mode with an upper-case digest": func(i *Intent) { i.PlateDigest = strings.ToUpper(i.PlateDigest) },
		"unknown mode":                         func(i *Intent) { i.Mode = "reference_edit" },
		"another model":                        func(i *Intent) { i.Model = "other" },
		"another size":                         func(i *Intent) { i.Size = "512*512" },
		"two images":                           func(i *Intent) { i.Quantity = 2 },
	} {
		in := plate
		mutate(&in)
		if _, err := Fingerprint(in); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestPlateInputValidityAndDigestBinding(t *testing.T) {
	p := frozenPlate()
	if !p.Valid() {
		t.Fatal("a complete frozen input must be valid")
	}
	if PlateDigest(p) != PlateDigest(frozenPlate()) {
		t.Fatal("digest must be deterministic")
	}
	other := p
	other.BackgroundIntent = "暖色原木桌面"
	if PlateDigest(other) == PlateDigest(p) {
		t.Fatal("the background words are part of the digest")
	}
	for name, mutate := range map[string]func(*PlateInput){
		"short set digest":      func(x *PlateInput) { x.SetSHA256 = "x" },
		"empty case":            func(x *PlateInput) { x.CaseID = "" },
		"zero canvas":           func(x *PlateInput) { x.CanvasW = 0 },
		"huge canvas":           func(x *PlateInput) { x.CanvasH = 5000 },
		"control character":     func(x *PlateInput) { x.BackgroundIntent = "a\nb" },
		"intent over 200 runes": func(x *PlateInput) { x.BackgroundIntent = strings.Repeat("界", 201) },
	} {
		x := p
		mutate(&x)
		if x.Valid() {
			t.Errorf("%s accepted", name)
		}
	}
	if !ValidIntentText(strings.Repeat("界", 200)) || ValidIntentText("") {
		t.Fatal("ValidIntentText bounds")
	}
}

func TestPlateQualityReportIsAPendingMarkerNotAVerdict(t *testing.T) {
	r := Run{Owner: Owner, Intent: legacyIntent()}
	r.Intent.Mode, r.Intent.Prompt, r.Intent.PlateDigest = ModePlateLock, "只生成背景", PlateDigest(frozenPlate())
	raw, err := QualityReport(r)
	if err != nil || string(raw) != `{"report_version":"product-source-quality/v2","state":"derivation_pending"}` {
		t.Fatalf("%s %v", raw, err)
	}
	for _, banned := range []string{"pass", "limited_candidate", "fidelity"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Fatalf("the run snapshot must not decide quality: %s", raw)
		}
	}
}

func TestValidatePlateDerivation(t *testing.T) {
	png := []byte("\x89PNG-x")
	report := `{"report_version":"product-source-quality/v2"}`
	good := PlateDerivation{RunID: "sir_a", State: DerivationDerived, PNG: png, SHA256: SHA256Hex(png), SizeBytes: int64(len(png)), Width: 10, Height: 10, ReportJSON: report, ReportSHA256: SHA256Hex([]byte(report)), CandidateState: CandidateLimited}
	if err := ValidatePlateDerivation(good); err != nil {
		t.Fatal(err)
	}
	noComposite := good
	noComposite.PNG, noComposite.SHA256, noComposite.SizeBytes, noComposite.CandidateState = nil, "", 0, CandidateNotUsable
	if err := ValidatePlateDerivation(noComposite); err != nil {
		t.Fatalf("an uncomposable plate is a legitimate failing record: %v", err)
	}
	blocked := PlateDerivation{RunID: "sir_a", State: DerivationBlocked, BlockedReason: BlockedSampleSetChanged, CandidateState: CandidateBlocked}
	if err := ValidatePlateDerivation(blocked); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*PlateDerivation){
		"digest mismatch":           func(d *PlateDerivation) { d.SHA256 = strings.Repeat("0", 64) },
		"size mismatch":             func(d *PlateDerivation) { d.SizeBytes++ },
		"report digest mismatch":    func(d *PlateDerivation) { d.ReportSHA256 = strings.Repeat("0", 64) },
		"unknown candidate":         func(d *PlateDerivation) { d.CandidateState = "great" },
		"blocked reason on derived": func(d *PlateDerivation) { d.BlockedReason = "x" },
		"limited without bytes":     func(d *PlateDerivation) { d.PNG, d.SHA256, d.SizeBytes = nil, "", 0 },
		"unknown state":             func(d *PlateDerivation) { d.State = "done" },
	} {
		d := good
		mutate(&d)
		if ValidatePlateDerivation(d) == nil {
			t.Errorf("%s accepted", name)
		}
	}
	bad := blocked
	bad.PNG = png
	if ValidatePlateDerivation(bad) == nil {
		t.Error("a blocked record must carry no bytes")
	}
}
````

`store` 层（真实 SQLite：冻结输入不可变 / 不可删、重放返回原 run、换背景同键 = 冲突、意图摘要必须匹配冻结输入、重开后输入仍在、派生写一次且读时校验字节、直接篡改磁盘字节被读时发现）：

`server/internal/store/plate_test.go`

````go
package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

func plateFrozen(intent string) si.PlateInput {
	h := strings.Repeat("a", 64)
	return si.PlateInput{CaseID: "carton-1024x1024", SetID: "frozen-sample-set", SetSHA256: h, InputID: "pin_a", OriginalSHA256: strings.Repeat("b", 64), MaskSHA256: strings.Repeat("c", 64), CoverageSHA256: strings.Repeat("d", 64), CanvasW: 1024, CanvasH: 1024, BackgroundIntent: intent, FitID: "fit-cover-center-catmullrom/v1", ComposeID: "compose-mask-lock/v1", ThresholdsVersion: "bg-lock-thresholds/v1"}
}

func plateIntent(project string, p si.PlateInput) si.Intent {
	in := sourceIntent()
	in.Scope.ProjectID = project
	in.Mode, in.Prompt, in.PlateDigest = si.ModePlateLock, "只生成背景："+p.BackgroundIntent, si.PlateDigest(p)
	return in
}

func openPlateStore(t *testing.T) (*Store, string, Project) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plate.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	proj, e := s.CreateProject(t.Context(), Project{TenantID: "acct_u", CreatedBy: "usr_u", Name: "plate", SourceType: "standalone"})
	if e != nil {
		t.Fatal(e)
	}
	return s, path, proj
}

func TestPlateRunInputIsFrozenImmutableAndSurvivesReopen(t *testing.T) {
	s, path, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	run, dup, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	if e != nil || dup || run.Intent.PlateDigest != si.PlateDigest(frozen) {
		t.Fatal(run, dup, e)
	}
	again, dup, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	if e != nil || !dup || again.ID != run.ID {
		t.Fatal("same key and same frozen input must replay the run", again.ID, run.ID, dup, e)
	}
	var n int
	if e := s.db.QueryRow(`SELECT count(*) FROM product_plate_inputs`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("a replay must not add a second frozen input: %d %v", n, e)
	}
	other := plateFrozen("暖色原木桌面")
	if _, _, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, other), other); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("same key, different background: %v", e)
	}
	bad := plateIntent(proj.ID, frozen)
	bad.PlateDigest = strings.Repeat("0", 64)
	if _, _, e := s.CreateSourcePlateRunForProject(t.Context(), bad, frozen); !errors.Is(e, si.ErrInvalid) {
		t.Fatalf("intent digest must match the frozen input: %v", e)
	}
	if _, e := s.db.Exec(`UPDATE product_plate_inputs SET digest=? WHERE run_id=?`, strings.Repeat("e", 64), run.ID); e == nil {
		t.Fatal("a frozen input must be immutable")
	}
	if _, e := s.db.Exec(`DELETE FROM product_plate_inputs WHERE run_id=?`, run.ID); e == nil {
		t.Fatal("a frozen input must not be deletable")
	}
	s.Close()
	s2, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close()
	reread, e := s2.GetSourceRunForRecovery(t.Context(), run.ID)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s2.GetPlateInput(t.Context(), reread)
	if e != nil || got != frozen {
		t.Fatalf("frozen input after reopen: %+v %v", got, e)
	}
	// Old rows stay byte stable: a text intent never gains a PlateDigest key.
	raw, _ := json.Marshal(sourceIntent())
	if strings.Contains(string(raw), "PlateDigest") {
		t.Fatalf("legacy intent JSON changed: %s", raw)
	}
	// A frozen input read against the wrong run fails closed.
	tampered := reread
	tampered.Intent.PlateDigest = strings.Repeat("f", 64)
	if _, e := s2.GetPlateInput(t.Context(), tampered); !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("digest mismatch must be an invariant failure: %v", e)
	}
}

func TestPlateDerivationIsWriteOnceAndVerifiedOnRead(t *testing.T) {
	s, _, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	run, _, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	if e != nil {
		t.Fatal(e)
	}
	png1 := []byte("\x89PNG-first")
	report := `{"report_version":"product-source-quality/v2"}`
	d := si.PlateDerivation{RunID: run.ID, State: si.DerivationDerived, PNG: png1, SHA256: si.SHA256Hex(png1), SizeBytes: int64(len(png1)), Width: 1024, Height: 1024, ReportJSON: report, ReportSHA256: si.SHA256Hex([]byte(report)), CandidateState: si.CandidateLimited}
	saved, e := s.SavePlateDerivation(t.Context(), d)
	if e != nil || saved.SHA256 != d.SHA256 {
		t.Fatal(saved, e)
	}
	png2 := []byte("\x89PNG-second")
	d2 := d
	d2.PNG, d2.SHA256, d2.SizeBytes = png2, si.SHA256Hex(png2), int64(len(png2))
	kept, e := s.SavePlateDerivation(t.Context(), d2)
	if e != nil || kept.SHA256 != d.SHA256 {
		t.Fatalf("the first derivation wins: %+v %v", kept, e)
	}
	if _, e := s.db.Exec(`UPDATE product_plate_derivations SET candidate_state='limited_candidate' WHERE run_id=?`, run.ID); e == nil {
		t.Fatal("a derivation is write-once")
	}
	for name, mutate := range map[string]func(*si.PlateDerivation){
		"digest not matching the bytes": func(x *si.PlateDerivation) { x.SHA256 = strings.Repeat("0", 64) },
		"report digest wrong":           func(x *si.PlateDerivation) { x.ReportSHA256 = strings.Repeat("0", 64) },
		"unknown candidate state":       func(x *si.PlateDerivation) { x.CandidateState = "great" },
		"limited without a composite":   func(x *si.PlateDerivation) { x.PNG, x.SHA256, x.SizeBytes = nil, "", 0 },
	} {
		bad := d
		bad.RunID = "sir_other"
		mutate(&bad)
		if _, e := s.SavePlateDerivation(t.Context(), bad); !errors.Is(e, si.ErrInvariant) {
			t.Fatalf("%s accepted: %v", name, e)
		}
	}
	// Corruption on disk is caught when read, never served.
	if _, e := s.db.Exec(`DROP TRIGGER product_plate_derivations_immutable`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`UPDATE product_plate_derivations SET derived_png=? WHERE run_id=?`, []byte("tampered"), run.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.GetPlateDerivation(t.Context(), run.ID); !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("tampered bytes must fail verification on read: %v", e)
	}
}

func TestF09SixteenConcurrentPlateCreatesYieldOneRunAndOneFrozenInput(t *testing.T) {
	s, _, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	ids := make([]string, 16)
	errs := make([]error, 16)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, e := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
			ids[i], errs[i] = r.ID, e
		}()
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("create %d: %q %v", i, ids[i], errs[i])
		}
	}
	var runs, inputs int
	if e := s.db.QueryRow(`SELECT count(*) FROM product_source_runs`).Scan(&runs); e != nil || runs != 1 {
		t.Fatal(runs, e)
	}
	if e := s.db.QueryRow(`SELECT count(*) FROM product_plate_inputs`).Scan(&inputs); e != nil || inputs != 1 {
		t.Fatal(inputs, e)
	}
}
````

`sourceflow` 层（真实 SQLite + 真实图像字节；Billing / Task / Upload 用既有的测试端口夹具，并新增 `plateAssets` 只下发测试给定的字节）。覆盖：原因码、创建冻结输入且只报价不派 Task、**拒绝发生在任何远端调用之前**（F04 / 控制字符 / 空背景 / 开关关）、一次派生（下载次数恒 1、无任何远端写）、F09 十六路并发派生只落一条记录且十六路并发确认 + 推进只派一个 Task（store 层另有十六路并发创建只落一个 run 与一份冻结输入）、F08 被篡改的下载 → 不落库且仍被欠着、F10 下载中断被重试且不扣费、F10 提交响应丢失后沿原键找回同一个 Task（不产生第二个 usage / quote / Task）、F15 同键换价格版本 = 冲突且超出 SKU 的模型不能建 run、F07 / F06 不可用但**记录并保留扣费事实且不重做**、选定门控、plate 原始资产不可打开、F17（样本集被换 → `blocked` 且不下载不扣费）、F18（重开后记录仍在；两个独立库用同输入派生出同字节）、恢复循环与显式 reconcile 都能驱动派生、两处意图规则一致、开关关闭只拒未确认报价：

`server/internal/sourceflow/plate_test.go`

````go
package sourceflow

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// plateAssets is the Upload authority for the paid plate. It serves exactly
// the bytes the test hands it; Verify reports the matching ready asset.
type plateAssets struct {
	mu        sync.Mutex
	output    si.Output
	body      []byte
	tamper    func([]byte) []byte
	failNext  int // the next N downloads fail as if the connection broke
	downloads int
	verifies  int
}

func (a *plateAssets) Verify(context.Context, si.Run) (si.Output, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.verifies++
	return a.output, nil
}
func (a *plateAssets) Reference(context.Context, si.Outbox) (string, error) { return "active", nil }
func (a *plateAssets) Release(context.Context, si.Outbox) error             { return nil }
func (a *plateAssets) Download(context.Context, si.Run) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.downloads++
	if a.failNext > 0 {
		a.failNext--
		return nil, si.ErrUnavailable
	}
	body := append([]byte(nil), a.body...)
	if a.tamper != nil {
		body = a.tamper(body)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func gradientPlate(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*60/w), uint8(50 + y*90/h), uint8(110 + (x+y)*40/(w+h)), 255})
		}
	}
	return pngBytes(img)
}

type plateFixture struct {
	*fixture
	set    *fs.Set
	c      *fs.LoadedCase
	assets *plateAssets
}

func newPlateFixture(t *testing.T, caseID string) *plateFixture {
	t.Helper()
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	var c *fs.LoadedCase
	for _, x := range set.Cases() {
		if x.CaseID == caseID {
			c = x
		}
	}
	if c == nil {
		t.Fatalf("case %s", caseID)
	}
	f := &plateFixture{fixture: flowFixture(t), set: set, c: c, assets: &plateAssets{}}
	f.enable()
	return f
}

// enable (re)installs plate wiring; open() builds a fresh Service on reopen.
func (f *plateFixture) enable() {
	f.svc.PlateEnabled = true
	f.svc.Samples = f.set
	f.svc.Assets = f.assets
	f.svc.Build = platelock.Build{VCSRevision: "test-rev"}
}

func (f *plateFixture) request(key string) PlateRequest {
	return PlateRequest{RequestKey: key, InputID: "pin_test", BackgroundIntent: f.c.Intents[0], OriginalSHA256: f.c.Original.SHA256}
}

// succeeded drives one plate run through quote, confirm, submit and a
// successful, charged Task whose output is exactly `plate`.
func (f *plateFixture) succeeded(t *testing.T, key string, plate []byte) si.Run {
	t.Helper()
	ctx := t.Context()
	r, e := f.svc.CreatePlate(ctx, f.actor, f.project.ID, f.request(key))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	r, e = f.svc.Confirm(ctx, f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil {
		t.Fatal(e)
	}
	if r, e = f.svc.Advance(ctx, r.ID); e != nil {
		t.Fatal(r, e)
	}
	task := f.task.facts[taskKey(r)]
	o := si.Output{AssetID: "asset-plate", ReferenceID: "ref-plate", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: si.SHA256Hex(plate), ContentType: "image/png", SizeBytes: int64(len(plate))}
	task.Status, task.Phase, task.HoldID, task.ChargeID, task.Output = "succeeded", "succeeded", "hold-plate", "charge-plate", &o
	f.task.facts[taskKey(r)] = task
	b := f.bill.facts[billKey(r)]
	n := int64(25)
	b.Status, b.HoldID, b.ChargeID, b.ChargedMinor = "charged", "hold-plate", "charge-plate", &n
	f.bill.facts[billKey(r)] = b
	ready := o
	ready.Status, ready.ReferenceState = "ready", "active"
	f.assets.output, f.assets.body = ready, plate
	// The coordinator observes the finished Task, then the output attaches.
	r, e = f.svc.Advance(ctx, r.ID)
	if e != nil {
		t.Fatal(r, e)
	}
	r, e = f.svc.ObserveOutput(ctx, r)
	if e != nil || r.Output == nil || r.Phase != "succeeded" || r.ChargedBill == nil {
		t.Fatal(r, e)
	}
	return r
}

func TestPlateReadyReasonCodes(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	if ok, why := f.svc.PlateReady("personal"); !ok || why != "" {
		t.Fatal(ok, why)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Service)
		source string
		want   string
	}{
		{"flag off", func(s *Service) { s.PlateEnabled = false }, "personal", "plate_lock_disabled"},
		{"no sample dir", func(s *Service) { s.Samples = nil }, "personal", "sample_set_unconfigured"},
		{"invalid sample set", func(s *Service) { s.Samples, s.SamplesReason = nil, "sample_set_invalid" }, "personal", "sample_set_invalid"},
		{"organization payer", func(*Service) {}, "delegation", "plate_org_not_supported"},
		{"source runtime not ready", func(s *Service) { s.OutputRecoveryReady = false }, "personal", "source_unconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newPlateFixture(t, "carton-1024x1024")
			g.svc.OutputRecoveryReady = true
			tc.mutate(g.svc)
			if ok, why := g.svc.PlateReady(tc.source); ok || why != tc.want {
				t.Fatalf("PlateReady = %v %q, want %q", ok, why, tc.want)
			}
		})
	}
}

func TestCreatePlateFreezesInputAndQuotesWithoutATask(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-a"))
	if e != nil || r.Quote == nil || r.Intent.Mode != si.ModePlateLock {
		t.Fatal(r, e)
	}
	if r.Intent.Prompt != bgreplace.ModelPlatePrompt(f.c.Intents[0]) || r.Intent.Size != "1024*1024" || r.Intent.Quantity != 1 || r.Intent.Model != "qwen-image-2.0" {
		t.Fatalf("the price profile and prompt are server frozen: %+v", r.Intent)
	}
	frozen, e := f.store.GetPlateInput(t.Context(), r)
	if e != nil || frozen.OriginalSHA256 != f.c.Original.SHA256 || frozen.MaskSHA256 != f.c.Mask.SHA256 || frozen.CaseID != f.c.CaseID || frozen.BackgroundIntent != f.c.Intents[0] || si.PlateDigest(frozen) != r.Intent.PlateDigest {
		t.Fatal(frozen, e)
	}
	if f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 || f.task.Submits != 0 {
		t.Fatalf("a quote must not dispatch: usage=%d quote=%d submits=%d", f.bill.UsageCommits, f.bill.QuoteCommits, f.task.Submits)
	}
	again, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-a"))
	if e != nil || again.ID != r.ID || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatal("same key must replay the same run", again.ID, r.ID, e)
	}
	other := f.request("plate-a")
	other.BackgroundIntent = f.c.Intents[1]
	if _, e = f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, other); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("same key, different background: %v", e)
	}
}

func TestCreatePlateRefusesBeforeAnyRemoteCall(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	notFrozen := f.request("plate-b")
	notFrozen.OriginalSHA256 = si.SHA256Hex([]byte("a customer photo"))
	badIntent := f.request("plate-c")
	badIntent.BackgroundIntent = "line\nbreak"
	empty := f.request("plate-d")
	empty.BackgroundIntent = ""
	for name, tc := range map[string]struct {
		req  PlateRequest
		want error
	}{"F04 photo is not a frozen sample": {notFrozen, si.ErrNotFrozen}, "control character": {badIntent, si.ErrInvalid}, "empty background": {empty, si.ErrInvalid}} {
		if _, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, tc.req); !errors.Is(e, tc.want) {
			t.Fatalf("%s: %v want %v", name, e, tc.want)
		}
	}
	f.svc.PlateEnabled = false
	if _, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-e")); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(e)
	}
	if f.bill.UsageAttempts != 0 || f.bill.QuoteAttempts != 0 || f.task.Submits != 0 {
		t.Fatalf("refusals must not touch Billing or Task: %+v", f.bill)
	}
	runs, _ := f.store.ListSourceRuns(t.Context(), mustScope(t, f))
	if len(runs) != 0 {
		t.Fatalf("a refused request left %d runs", len(runs))
	}
}

func mustScope(t *testing.T, f *plateFixture) si.Scope {
	t.Helper()
	auth, e := f.svc.Auth.ForProject(t.Context(), f.actor, f.project.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	return auth.Scope
}

func TestDerivePlateOncePaidLockedAndIdempotent(t *testing.T) {
	f := newPlateFixture(t, "handled_metal-1024x1280")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-ok", gradientPlate(1024, 1024))
	d, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || d.State != si.DerivationDerived || d.CandidateState != si.CandidateLimited || d.Width != 1024 || d.Height != 1280 {
		t.Fatal(d.State, d.CandidateState, e)
	}
	rep, e := platelock.DecodeReport([]byte(d.ReportJSON))
	if e != nil || rep.Facts.TaskID != r.TaskID || rep.Facts.UsageID != r.Quote.UsageID || rep.Facts.QuoteID != r.Quote.QuoteID || rep.Facts.OriginalChargeID != "charge-plate" || rep.Facts.Plate.SHA256 != r.Output.SHA256 || rep.Facts.CompositeSHA256 != d.SHA256 || rep.Build.VCSRevision != "test-rev" {
		t.Fatalf("%+v %v", rep.Facts, e)
	}
	if _, e := bgreplace.SubjectPixelChecks(f.c.OriginalBytes, d.PNG, f.c.MaskBytes); e != nil {
		t.Fatalf("the stored composite must keep every protected pixel: %v", e)
	}
	again, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || again.SHA256 != d.SHA256 || again.ReportSHA256 != d.ReportSHA256 || f.assets.downloads != 1 {
		t.Fatalf("a repeat must return the stored record without another download (downloads=%d): %v", f.assets.downloads, e)
	}
	if f.task.Submits != 1 || f.task.Commits != 1 || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatalf("derivation made a remote mutation: submits=%d usage=%d quote=%d", f.task.Submits, f.bill.UsageCommits, f.bill.QuoteCommits)
	}
}

func TestF09SixteenConcurrentDerivationsStoreOneRecord(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-race", gradientPlate(1024, 1024))
	var wg sync.WaitGroup
	shas := make([]string, 16)
	errs := make([]error, 16)
	for i := range shas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, e := f.svc.DerivePlate(context.Background(), r.ID)
			shas[i], errs[i] = d.SHA256, e
		}()
	}
	wg.Wait()
	for i := range shas {
		if errs[i] != nil || shas[i] == "" || shas[i] != shas[0] {
			t.Fatalf("derivation %d: %q %v", i, shas[i], errs[i])
		}
	}
	if f.assets.downloads != 1 {
		t.Fatalf("16 callers downloaded the plate %d times", f.assets.downloads)
	}
}

func TestF08TamperedDownloadStoresNothingAndKeepsRetrying(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-tamper", gradientPlate(1024, 1024))
	f.assets.tamper = func(b []byte) []byte { b[len(b)/2] ^= 0xFF; return b }
	if _, e := f.svc.DerivePlate(t.Context(), r.ID); !errors.Is(e, si.ErrInvariant) {
		t.Fatalf("a download that does not match the verified asset: %v", e)
	}
	if _, ok, _ := f.store.GetPlateDerivation(t.Context(), r.ID); ok {
		t.Fatal("a derivation was stored from bytes that failed verification")
	}
	waiting, e := f.store.ListPlateRunsAwaitingDerivation(t.Context(), 5)
	if e != nil || len(waiting) != 1 || waiting[0].ID != r.ID {
		t.Fatal("the run must still be owed a derivation", waiting, e)
	}
	f.assets.tamper = nil
	if d, e := f.svc.DerivePlate(t.Context(), r.ID); e != nil || d.CandidateState != si.CandidateLimited {
		t.Fatal(d.CandidateState, e)
	}
	if f.task.Submits != 1 || f.bill.UsageCommits != 1 {
		t.Fatal("retrying a derivation must not spend again")
	}
}

func TestF07F06UnusablePlatesAreRecordedNotRegenerated(t *testing.T) {
	for name, tc := range map[string]struct {
		caseID string
		plate  func(f *plateFixture) []byte
		axis   string
	}{
		"background unchanged (plate is the original)": {"carton-1024x1024", func(f *plateFixture) []byte { return f.c.OriginalBytes }, platelock.AxisBackground},
		"wrong plate size": {"carton-1024x1024", func(*plateFixture) []byte { return gradientPlate(512, 512) }, platelock.AxisPlateSize},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPlateFixture(t, tc.caseID)
			f.svc.OutputRecoveryReady = true
			r := f.succeeded(t, "plate-bad", tc.plate(f))
			d, e := f.svc.DerivePlate(t.Context(), r.ID)
			if e != nil || d.CandidateState != si.CandidateNotUsable {
				t.Fatal(d.CandidateState, e)
			}
			rep, e := platelock.DecodeReport([]byte(d.ReportJSON))
			if e != nil || rep.Facts.OriginalChargeID != "charge-plate" {
				t.Fatalf("the paid charge fact must stay on the record: %+v %v", rep.Facts, e)
			}
			failed := false
			for _, a := range rep.Axes {
				if a.Name == tc.axis && a.State == platelock.Fail {
					failed = true
				}
			}
			if !failed {
				t.Fatalf("%s should FAIL: %+v", tc.axis, rep.Axes)
			}
			if _, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrNotUsable) {
				t.Fatalf("select of an unusable candidate: %v", e)
			}
			if f.task.Submits != 1 || f.task.Commits != 1 || f.bill.UsageCommits != 1 {
				t.Fatal("an unusable result must not trigger a second generation")
			}
		})
	}
}

func TestSelectIsGatedByTheDerivedCandidate(t *testing.T) {
	f := newPlateFixture(t, "glass_bottle-768x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-select", gradientPlate(1024, 1024))
	if _, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("before derivation: %v", e)
	}
	if _, e := f.svc.DerivePlate(t.Context(), r.ID); e != nil {
		t.Fatal(e)
	}
	got, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID)
	if e != nil || !got.Selected {
		t.Fatal(got.Selected, e)
	}
}

func TestPlateRunsNeverExposeTheRawPlate(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-raw", gradientPlate(1024, 1024))
	if _, e := f.svc.OpenOutput(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("OpenOutput on a plate run must refuse: %v", e)
	}
}

func TestF17ChangedSampleSetBlocksWithoutSpendingOrDownloading(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-set", gradientPlate(1024, 1024))
	// A different, individually valid set: same original, a different mask.
	dir := sampletest.Copy(t)
	setPixel := func(path string) {
		raw := mustRead(t, path)
		img, _ := png.Decode(bytes.NewReader(raw))
		n := image.NewNRGBA(img.Bounds())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		n.SetNRGBA(5, 5, color.NRGBA{255, 255, 255, 255}) // a stray protected pixel: valid, but a different mask
		mustWrite(t, path, pngBytes(n))
	}
	setPixel(dir + "/masks/carton-1024x1024.png")
	sampletest.Resign(t, dir, nil)
	changed, e := fs.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	f.svc.Samples = changed
	d, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || d.State != si.DerivationBlocked || d.BlockedReason != si.BlockedSampleSetChanged || d.CandidateState != si.CandidateBlocked {
		t.Fatal(d, e)
	}
	if f.assets.downloads != 0 || f.task.Submits != 1 || f.bill.UsageCommits != 1 {
		t.Fatalf("a blocked derivation must not download or spend: downloads=%d", f.assets.downloads)
	}
	if _, e := f.svc.Select(t.Context(), f.actor, f.project.ID, r.ID); !errors.Is(e, si.ErrNotUsable) {
		t.Fatal(e)
	}
}

func TestF18ReopenKeepsRecordAndTwoRunsDeriveEqualBytes(t *testing.T) {
	a := newPlateFixture(t, "handled_metal-768x1024")
	a.svc.OutputRecoveryReady = true
	ra := a.succeeded(t, "plate-det", gradientPlate(1024, 1024))
	da, e := a.svc.DerivePlate(t.Context(), ra.ID)
	if e != nil {
		t.Fatal(e)
	}
	a.reopen(t)
	a.enable()
	stored, ok, e := a.store.GetPlateDerivation(t.Context(), ra.ID)
	if e != nil || !ok || stored.SHA256 != da.SHA256 || stored.ReportSHA256 != da.ReportSHA256 {
		t.Fatal("record lost across reopen", e)
	}
	b := newPlateFixture(t, "handled_metal-768x1024")
	b.svc.OutputRecoveryReady = true
	rb := b.succeeded(t, "plate-det", gradientPlate(1024, 1024))
	db, e := b.svc.DerivePlate(t.Context(), rb.ID)
	if e != nil || db.SHA256 != da.SHA256 {
		t.Fatalf("the same inputs must derive the same composite: %s vs %s (%v)", db.SHA256, da.SHA256, e)
	}
}

func TestReconcileAndRecoveryLoopDriveDerivation(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1280")
	f.svc.OutputRecoveryReady = true
	ctx := t.Context()
	r, e := f.svc.CreatePlate(ctx, f.actor, f.project.ID, f.request("plate-rec"))
	if e != nil {
		t.Fatal(e)
	}
	if r, e = f.svc.Confirm(ctx, f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e != nil {
		t.Fatal(e)
	}
	if r, e = f.svc.Advance(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	plate := gradientPlate(1024, 1024)
	task := f.task.facts[taskKey(r)]
	o := si.Output{AssetID: "asset-rec", ReferenceID: "ref-rec", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: si.SHA256Hex(plate), ContentType: "image/png", SizeBytes: int64(len(plate))}
	task.Status, task.Phase, task.HoldID, task.ChargeID, task.Output = "succeeded", "succeeded", "hold-rec", "charge-rec", &o
	f.task.facts[taskKey(r)] = task
	b := f.bill.facts[billKey(r)]
	n := int64(25)
	b.Status, b.HoldID, b.ChargeID, b.ChargedMinor = "charged", "hold-rec", "charge-rec", &n
	f.bill.facts[billKey(r)] = b
	ready := o
	ready.Status, ready.ReferenceState = "ready", "active"
	f.assets.output, f.assets.body = ready, plate
	f.now = f.now.Add(61 * 1e9) // past retry/lease windows

	// The coordinator alone carries the run from a finished Task to a derivation.
	if e = f.svc.recoveryPass(ctx); e != nil {
		t.Fatal(e)
	}
	d, ok, e := f.store.GetPlateDerivation(ctx, r.ID)
	if e != nil || !ok || d.CandidateState != si.CandidateLimited {
		t.Fatal("recovery loop did not derive", ok, d.CandidateState, e)
	}
	if f.task.Submits != 1 || f.bill.UsageCommits != 1 {
		t.Fatal("recovery spent again")
	}
	// An explicit reconcile of a finished run is idempotent and returns the same record.
	got, e := f.svc.Reconcile(ctx, f.actor, f.project.ID, r.ID)
	if e != nil || got.Phase != "succeeded" || f.assets.downloads != 1 {
		t.Fatal(got.Phase, f.assets.downloads, e)
	}
}

func TestIntentRuleAgreesBetweenSampleSetAndSourceImage(t *testing.T) {
	for _, s := range []string{"深蓝色背景", "", " x", "x ", "a\nb", "界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界界", "ok\t"} {
		if fs.ValidIntent(s) != si.ValidIntentText(s) {
			t.Fatalf("rules disagree on %q", s)
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := readFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := writeFile(p, b); e != nil {
		t.Fatal(e)
	}
}

func TestFlagOffRefusesAnUnconfirmedQuoteButReplaysAConfirmedOne(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-flag"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	f.svc.PlateEnabled = false
	if _, e = f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatalf("an unconfirmed plate quote must not start while the flag is off: %v", e)
	}
	f.svc.PlateEnabled = true
	confirmed, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || confirmed.ConfirmationHash == "" {
		t.Fatal(confirmed, e)
	}
	f.svc.PlateEnabled = false
	again, e := f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote))
	if e != nil || again.ConfirmationHash != confirmed.ConfirmationHash {
		t.Fatalf("a replay of an existing confirmation must keep working: %v", e)
	}
}

func TestF09SixteenConcurrentConfirmsAndAdvancesSubmitExactlyOnce(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-confirm-race"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	hash := si.QuoteHash(*r.Quote)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := f.svc.Confirm(context.Background(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, hash); e == nil {
				_, _ = f.svc.Advance(context.Background(), r.ID)
			}
		}()
	}
	wg.Wait()
	if f.task.Submits != 1 || f.task.Commits != 1 || f.task.Dispatches != 1 {
		t.Fatalf("16 racing confirms must dispatch once: submits=%d commits=%d", f.task.Submits, f.task.Commits)
	}
	runs, _ := f.store.ListSourceRuns(t.Context(), mustScope(t, f))
	if len(runs) != 1 || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatalf("runs=%d usage=%d quote=%d", len(runs), f.bill.UsageCommits, f.bill.QuoteCommits)
	}
}

func TestF10InterruptedDownloadIsRetriedWithoutSpending(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r := f.succeeded(t, "plate-dl", gradientPlate(1024, 1024))
	f.assets.failNext = 1
	if _, e := f.svc.DerivePlate(t.Context(), r.ID); !errors.Is(e, si.ErrUnavailable) {
		t.Fatalf("an interrupted download: %v", e)
	}
	if _, ok, _ := f.store.GetPlateDerivation(t.Context(), r.ID); ok {
		t.Fatal("nothing may be stored from an interrupted download")
	}
	d, e := f.svc.DerivePlate(t.Context(), r.ID)
	if e != nil || d.CandidateState != si.CandidateLimited || f.assets.downloads != 2 {
		t.Fatal(d.CandidateState, f.assets.downloads, e)
	}
	if f.task.Submits != 1 || f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatal("retrying a download must not spend")
	}
}

func TestF10LostSubmitResponseRecoversThePlateRunWithoutASecondTask(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	ctx := t.Context()
	r, e := f.svc.CreatePlate(ctx, f.actor, f.project.ID, f.request("plate-lost"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	if r, e = f.svc.Confirm(ctx, f.actor, f.project.ID, r.ID, r.Quote.QuoteID, si.QuoteHash(*r.Quote)); e != nil {
		t.Fatal(e)
	}
	f.task.AfterCommit = "submit" // the platform accepted the Task but the reply was lost
	if _, e = f.svc.Advance(ctx, r.ID); e == nil {
		t.Fatal("the lost reply must surface as an error")
	}
	f.reopen(t)
	f.enable()
	f.svc.OutputRecoveryReady = true
	again, e := f.svc.Advance(ctx, r.ID)
	if e != nil || again.TaskID == "" || f.task.Submits != 1 || f.task.Commits != 1 {
		t.Fatalf("recovery must find the original Task by its key: task=%q submits=%d commits=%d err=%v", again.TaskID, f.task.Submits, f.task.Commits, e)
	}
	if f.bill.UsageCommits != 1 || f.bill.QuoteCommits != 1 {
		t.Fatal("recovery must not create a second usage or quote")
	}
}

func TestF15ChangedPriceProfileCannotReuseAnOldPlateRun(t *testing.T) {
	f := newPlateFixture(t, "carton-1024x1024")
	f.svc.OutputRecoveryReady = true
	r, e := f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-price"))
	if e != nil || r.Quote == nil {
		t.Fatal(r, e)
	}
	if _, e = f.svc.Confirm(t.Context(), f.actor, f.project.ID, r.ID, r.Quote.QuoteID, "not-the-quoted-hash"); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("a confirmation that does not carry the quoted fingerprint: %v", e)
	}
	f.svc.Profile.PricingVersion = "test-price-v2"
	if _, e = f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-price")); !errors.Is(e, si.ErrConflict) {
		t.Fatalf("the same key under another price version is a different request: %v", e)
	}
	f.svc.Profile.PricingVersion = "test-price-v1"
	f.svc.Profile.Model = "another-model"
	if _, e = f.svc.CreatePlate(t.Context(), f.actor, f.project.ID, f.request("plate-price-2")); !errors.Is(e, si.ErrUnconfigured) {
		t.Fatalf("a model outside the priced SKU must not create runs: %v", e)
	}
	if f.task.Submits != 0 {
		t.Fatal("nothing may be dispatched")
	}
}
````

`server/internal/sourceflow/plate_io_test.go`

````go
package sourceflow

import "os"

func readFile(p string) ([]byte, error)  { return os.ReadFile(p) }
func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
````

- [ ] **Step 2: 运行，确认失败**

```bash
cd "$WT/server" && go test -count=1 ./internal/sourceimage ./internal/store ./internal/sourceflow 2>&1 | tail -12
```

期望：三个包都 `FAIL [build failed]`（`undefined: ModePlateLock`、`undefined: PlateInput`、`f.svc.PlateEnabled undefined` …）。

- [ ] **Step 3: 写实现——`sourceimage`**

补丁（新增模式常量与两个错误；`Intent.PlateDigest`；`Fingerprint` 的模式规则；`QualityReport` 对 plate 只给“待派生”标记而不下任何质量结论）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/sourceimage/types.go
+++ b/server/internal/sourceimage/types.go
@@ -5,6 +5,13 @@
 import "errors"
 
 const Owner = "product_source_v1"
+
+// Modes the source owner can run. Both share one provider, model, size,
+// quantity and price profile; only background_plate_lock adds a frozen input.
+const (
+	ModeTextGenerate = "text_generate"
+	ModePlateLock    = "background_plate_lock"
+)
 const TaskRequestBudgetSeconds int64 = 20
 
 var (
@@ -15,6 +22,10 @@
 	ErrInvariant    = errors.New("source image: persisted invariant")
 	ErrUnconfigured = errors.New("source image: unconfigured")
 	ErrForbidden    = errors.New("source image: forbidden")
+	// ErrNotFrozen: the registered input is not a frozen, approved sample.
+	ErrNotFrozen = errors.New("source image: input is not a frozen sample")
+	// ErrNotUsable: the derived result failed a required quality axis.
+	ErrNotUsable = errors.New("source image: candidate not usable")
 )
 
 type Scope struct{ AppID, TenantID, ProjectID, UserID, PrincipalAccountID, PayerAccountID string }
@@ -22,6 +33,9 @@
 	Scope                                                                       Scope
 	RequestKey, Mode, Provider, Model, Capability, Size, Prompt, PricingVersion string
 	Quantity                                                                    int64
+	// PlateDigest binds a background_plate_lock run to its frozen input record.
+	// Empty (and omitted from the JSON, keeping old rows byte-stable) otherwise.
+	PlateDigest string `json:",omitempty"`
 }
 type Quote struct {
 	UsageID, UsageKey, QuoteID, PricingVersion, BusinessRef, Currency  string
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/server/internal/sourceimage/validate.go
+++ b/server/internal/sourceimage/validate.go
@@ -31,11 +31,23 @@
 	return true
 }
 func Fingerprint(in Intent) (string, error) {
-	if !in.Scope.Valid() || !identifier(in.RequestKey) || !identifier(in.PricingVersion) || in.Mode != "text_generate" || in.Provider != "modelxing-qwen-image-2.0-v1" || in.Model != "qwen-image-2.0" || in.Capability != "image.generate" || in.Size != "1024*1024" || in.Quantity != 1 || !utf8.ValidString(in.Prompt) || len(in.Prompt) > 16<<10 || strings.TrimSpace(in.Prompt) == "" {
+	modeOK := in.Mode == ModeTextGenerate && in.PlateDigest == "" || in.Mode == ModePlateLock && validDigest(in.PlateDigest)
+	if !in.Scope.Valid() || !identifier(in.RequestKey) || !identifier(in.PricingVersion) || !modeOK || in.Provider != "modelxing-qwen-image-2.0-v1" || in.Model != "qwen-image-2.0" || in.Capability != "image.generate" || in.Size != "1024*1024" || in.Quantity != 1 || !utf8.ValidString(in.Prompt) || len(in.Prompt) > 16<<10 || strings.TrimSpace(in.Prompt) == "" {
 		return "", ErrInvalid
 	}
 	return digest(in), nil
 }
+func validDigest(s string) bool {
+	if len(s) != 64 {
+		return false
+	}
+	for _, c := range s {
+		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
+			return false
+		}
+	}
+	return true
+}
 func digest(v any) string {
 	raw, _ := json.Marshal(v)
 	sum := sha256.Sum256(raw)
PATCH2
git apply --whitespace=nowarn <<'PATCH3'
--- a/server/internal/sourceimage/quality.go
+++ b/server/internal/sourceimage/quality.go
@@ -8,7 +8,7 @@
 
 // QualityReport is ordinary-image evidence, never product fidelity or adoption.
 func QualityReport(r Run) ([]byte, error) {
-	if r.Owner != Owner || r.Intent.Mode != "text_generate" {
+	if r.Owner != Owner || r.Intent.Mode != ModeTextGenerate && r.Intent.Mode != ModePlateLock {
 		return nil, ErrInvalid
 	}
 	if _, e := Fingerprint(r.Intent); e != nil {
@@ -17,6 +17,11 @@
 	if r.Quote != nil && ValidateQuote(r, *r.Quote) != nil || r.Task != nil && (r.TaskID != r.Task.ID || ValidateTaskFact(r, *r.Task) != nil) || r.TaskID != "" && r.Task == nil || r.Bill != nil && ValidateSettlement(r, *r.Bill) != nil || r.ChargedBill != nil && ValidateChargedHistory(r, *r.ChargedBill) != nil || r.Output != nil && ValidateOutputAssociation(r, *r.Output) != nil {
 		return nil, ErrInvariant
 	}
+	if r.Intent.Mode == ModePlateLock {
+		// The real v2 report needs the derived composite and lives in the
+		// derivation record. This marks the run's own snapshot as not yet decided.
+		return []byte(`{"report_version":"product-source-quality/v2","state":"derivation_pending"}`), nil
+	}
 	sum := sha256.Sum256([]byte(r.Intent.Prompt))
 	report := map[string]any{"report_version": "product-source-quality/v1", "mode": r.Intent.Mode, "fidelity": "not_applicable", "visual_quality": "unknown", "human_adoption": "NOT_RUN", "run_id": r.ID, "model": r.Intent.Model, "provider": r.Intent.Provider, "pricing_version": r.Intent.PricingVersion, "prompt_sha256": hex.EncodeToString(sum[:]), "task_id": r.TaskID, "content_verification": "NOT_RUN", "limitations": []string{"ordinary conceptual image; product preservation is not verified", "no human adoption evidence; visual quality unknown"}}
 	if r.Quote != nil {
PATCH3
````

新文件（冻结输入、摘要、派生记录与其完整性校验）：

`server/internal/sourceimage/plate.go`

````go
package sourceimage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PlateInput is the immutable, server-derived input of a background_plate_lock
// run. Nothing here comes from the browser except InputID and BackgroundIntent,
// and both are checked against the frozen sample set before it is stored.
type PlateInput struct {
	CaseID            string `json:"case_id"`
	SetID             string `json:"set_id"`
	SetSHA256         string `json:"set_sha256"`
	InputID           string `json:"input_id"`
	OriginalSHA256    string `json:"original_sha256"`
	MaskSHA256        string `json:"mask_sha256"`
	CoverageSHA256    string `json:"coverage_sha256"`
	CanvasW           int    `json:"canvas_w"`
	CanvasH           int    `json:"canvas_h"`
	BackgroundIntent  string `json:"background_intent"`
	FitID             string `json:"fit_id"`
	ComposeID         string `json:"compose_id"`
	ThresholdsVersion string `json:"thresholds_version"`
}

// PlateDigest is stored in Intent.PlateDigest and re-checked on every read.
func PlateDigest(p PlateInput) string { return digest(p) }

func (p PlateInput) Valid() bool {
	for _, d := range []string{p.SetSHA256, p.OriginalSHA256, p.MaskSHA256, p.CoverageSHA256} {
		if !validDigest(d) {
			return false
		}
	}
	return identifier(p.CaseID) && identifier(p.SetID) && identifier(p.InputID) && p.CanvasW > 0 && p.CanvasH > 0 && p.CanvasW <= 4096 && p.CanvasH <= 4096 &&
		ValidIntentText(p.BackgroundIntent) && identifier(p.FitID) && identifier(p.ComposeID) && identifier(p.ThresholdsVersion)
}

// ValidIntentText is the one rule for a user's background direction: 1 to 200
// characters, trimmed, no control characters. fidelitysamples.ValidIntent must
// agree with it (pinned by a sourceflow test).
func ValidIntentText(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 200 || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

const (
	DerivationDerived = "derived"
	DerivationBlocked = "blocked"

	CandidateLimited   = "limited_candidate"
	CandidateNotUsable = "not_usable_candidate"
	CandidatePending   = "pending"
	CandidateBlocked   = "blocked"

	BlockedSampleSetChanged = "sample_set_changed"
)

// PlateDerivation is write-once. PNG is the derived composite (empty only when
// the plate could not be composed, which is always not_usable_candidate).
type PlateDerivation struct {
	RunID, State, BlockedReason string
	PNG                         []byte
	SHA256                      string
	SizeBytes                   int64
	Width, Height               int
	ReportJSON, ReportSHA256    string
	CandidateState              string
	CreatedAt                   int64
}

func SHA256Hex(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// ValidatePlateDerivation is the store's integrity gate for both write and read.
func ValidatePlateDerivation(d PlateDerivation) error {
	if !identifier(d.RunID) {
		return ErrInvariant
	}
	switch d.State {
	case DerivationBlocked:
		if d.BlockedReason == "" || d.CandidateState != CandidateBlocked || len(d.PNG) != 0 || d.SHA256 != "" || d.ReportJSON != "" || d.ReportSHA256 != "" {
			return ErrInvariant
		}
		return nil
	case DerivationDerived:
	default:
		return ErrInvariant
	}
	switch d.CandidateState {
	case CandidateLimited, CandidateNotUsable, CandidatePending:
	default:
		return ErrInvariant
	}
	if d.ReportJSON == "" || d.ReportSHA256 != SHA256Hex([]byte(d.ReportJSON)) || d.BlockedReason != "" {
		return ErrInvariant
	}
	if len(d.PNG) == 0 {
		if d.SHA256 != "" || d.SizeBytes != 0 || d.CandidateState != CandidateNotUsable {
			return ErrInvariant
		}
		return nil
	}
	if d.SHA256 != SHA256Hex(d.PNG) || d.SizeBytes != int64(len(d.PNG)) || d.Width <= 0 || d.Height <= 0 || d.SizeBytes > 16<<20 {
		return ErrInvariant
	}
	return nil
}
````

- [ ] **Step 4: 写实现——`store`（迁移 0020 + 函数）**

迁移（只加表 / 触发器；输入与派生都是写一次、不可删）：

`server/internal/store/migrations/0020_plate_lock.sql`

````sql
-- Additive. Frozen input and write-once derived output of background_plate_lock
-- runs. Old runs, tables and rows are untouched; rolling back means turning the
-- feature flag off, never downgrading the binary once such rows exist.
CREATE TABLE product_plate_inputs (
 run_id TEXT PRIMARY KEY REFERENCES product_source_runs(id) ON DELETE RESTRICT,
 frozen_json TEXT NOT NULL CHECK(json_valid(frozen_json)),
 digest TEXT NOT NULL CHECK(length(digest)=64),
 created_at INTEGER NOT NULL
);
CREATE TRIGGER product_plate_inputs_immutable BEFORE UPDATE ON product_plate_inputs
BEGIN SELECT RAISE(ABORT,'immutable plate input'); END;
CREATE TRIGGER product_plate_inputs_keep BEFORE DELETE ON product_plate_inputs
BEGIN SELECT RAISE(ABORT,'plate input cannot be deleted'); END;
CREATE TABLE product_plate_derivations (
 run_id TEXT PRIMARY KEY REFERENCES product_plate_inputs(run_id) ON DELETE RESTRICT,
 state TEXT NOT NULL CHECK(state IN('derived','blocked')),
 blocked_reason TEXT NOT NULL DEFAULT '',
 derived_png BLOB,
 derived_sha256 TEXT NOT NULL DEFAULT '',
 size_bytes INTEGER NOT NULL DEFAULT 0 CHECK(typeof(size_bytes)='integer' AND size_bytes>=0),
 width INTEGER NOT NULL DEFAULT 0 CHECK(typeof(width)='integer' AND width>=0),
 height INTEGER NOT NULL DEFAULT 0 CHECK(typeof(height)='integer' AND height>=0),
 report_json TEXT NOT NULL DEFAULT '',
 report_sha256 TEXT NOT NULL DEFAULT '',
 candidate_state TEXT NOT NULL CHECK(candidate_state IN('limited_candidate','pending','not_usable_candidate','blocked')),
 created_at INTEGER NOT NULL
);
CREATE TRIGGER product_plate_derivations_immutable BEFORE UPDATE ON product_plate_derivations
BEGIN SELECT RAISE(ABORT,'derivation is write-once'); END;
CREATE TRIGGER product_plate_derivations_keep BEFORE DELETE ON product_plate_derivations
BEGIN SELECT RAISE(ABORT,'derivation cannot be deleted'); END;
````

`createSourceRun` 增加同事务钩子（只在**真正插入新行**时执行，重放不执行）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/store/source_runs.go
+++ b/server/internal/store/source_runs.go
@@ -115,9 +115,12 @@
 	return scanSource(c.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE id=? AND `+sourceScopeSQL, args...))
 }
 func (s *Store) CreateSourceRun(ctx context.Context, in si.Intent) (si.Run, bool, error) {
-	return s.createSourceRun(ctx, in, false)
+	return s.createSourceRun(ctx, in, false, nil)
 }
-func (s *Store) createSourceRun(ctx context.Context, in si.Intent, projectCheck bool) (out si.Run, duplicate bool, err error) {
+
+// createSourceRun's extra hook runs inside the creating transaction, only when
+// a new run row was inserted (never for a duplicate request key).
+func (s *Store) createSourceRun(ctx context.Context, in si.Intent, projectCheck bool, extra func(c *sql.Conn, runID string, now int64) error) (out si.Run, duplicate bool, err error) {
 	fp, err := si.Fingerprint(in)
 	if err != nil {
 		return out, false, err
@@ -148,6 +151,11 @@
 		if e != nil {
 			return e
 		}
+		if extra != nil {
+			if e = extra(c, id, now); e != nil {
+				return e
+			}
+		}
 		out, e = sourceOnConn(ctx, c, in.Scope, id)
 		return e
 	})
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/server/internal/store/source_http.go
+++ b/server/internal/store/source_http.go
@@ -11,7 +11,7 @@
 // live Context proof; this owned transaction checks original project ownership
 // and archive/deletion again before even replaying or creating an intent.
 func (s *Store) CreateSourceRunForProject(ctx context.Context, in si.Intent) (si.Run, bool, error) {
-	return s.createSourceRun(ctx, in, true)
+	return s.createSourceRun(ctx, in, true, nil)
 }
 func sourceProjectActive(ctx context.Context, c *sql.Conn, scope si.Scope) error {
 	var tenant, creator, status string
PATCH2
````

`server/internal/store/plate.go`

````go
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// CreateSourcePlateRunForProject creates the run and its frozen input in one
// transaction. A repeated request_key with the same frozen input returns the
// original run; a different input (different digest, hence fingerprint) is a
// conflict. The original run's quote, Task and charge are never touched here.
func (s *Store) CreateSourcePlateRunForProject(ctx context.Context, in si.Intent, frozen si.PlateInput) (si.Run, bool, error) {
	if in.Mode != si.ModePlateLock || !frozen.Valid() || in.PlateDigest != si.PlateDigest(frozen) {
		return si.Run{}, false, si.ErrInvalid
	}
	raw, _ := json.Marshal(frozen)
	return s.createSourceRun(ctx, in, true, func(c *sql.Conn, runID string, now int64) error {
		_, e := c.ExecContext(ctx, `INSERT INTO product_plate_inputs(run_id,frozen_json,digest,created_at) VALUES(?,?,?,?)`, runID, string(raw), in.PlateDigest, now)
		return e
	})
}

// GetPlateInput returns the frozen input after proving it still hashes to the
// digest bound into the run's immutable intent.
func (s *Store) GetPlateInput(ctx context.Context, run si.Run) (si.PlateInput, error) {
	var raw, digest string
	e := s.db.QueryRowContext(ctx, `SELECT frozen_json,digest FROM product_plate_inputs WHERE run_id=?`, run.ID).Scan(&raw, &digest)
	if errors.Is(e, sql.ErrNoRows) {
		return si.PlateInput{}, si.ErrInvariant
	}
	if e != nil {
		return si.PlateInput{}, e
	}
	var p si.PlateInput
	if json.Unmarshal([]byte(raw), &p) != nil || !p.Valid() {
		return si.PlateInput{}, si.ErrInvariant
	}
	again, _ := json.Marshal(p)
	if string(again) != raw || si.PlateDigest(p) != digest || digest != run.Intent.PlateDigest || run.Intent.Mode != si.ModePlateLock {
		return si.PlateInput{}, si.ErrInvariant
	}
	return p, nil
}

const plateDerivationColumns = `run_id,state,blocked_reason,derived_png,derived_sha256,size_bytes,width,height,report_json,report_sha256,candidate_state,created_at`

func scanPlateDerivation(row interface{ Scan(...any) error }) (si.PlateDerivation, error) {
	var d si.PlateDerivation
	e := row.Scan(&d.RunID, &d.State, &d.BlockedReason, &d.PNG, &d.SHA256, &d.SizeBytes, &d.Width, &d.Height, &d.ReportJSON, &d.ReportSHA256, &d.CandidateState, &d.CreatedAt)
	if e != nil {
		return d, e
	}
	if si.ValidatePlateDerivation(d) != nil {
		return si.PlateDerivation{}, si.ErrInvariant
	}
	return d, nil
}

// GetPlateDerivation returns (record, true) or (zero, false). Bytes are
// re-verified against their stored digests on every read.
func (s *Store) GetPlateDerivation(ctx context.Context, runID string) (si.PlateDerivation, bool, error) {
	d, e := scanPlateDerivation(s.db.QueryRowContext(ctx, `SELECT `+plateDerivationColumns+` FROM product_plate_derivations WHERE run_id=?`, runID))
	if errors.Is(e, sql.ErrNoRows) {
		return si.PlateDerivation{}, false, nil
	}
	return d, e == nil, e
}

// SavePlateDerivation inserts the write-once record. When a record already
// exists (a concurrent or repeated derivation) the existing one wins; the
// caller's differing bytes are never stored.
func (s *Store) SavePlateDerivation(ctx context.Context, d si.PlateDerivation) (si.PlateDerivation, error) {
	if e := si.ValidatePlateDerivation(d); e != nil {
		return si.PlateDerivation{}, e
	}
	err := s.withSourceWrite(ctx, func(c *sql.Conn) error {
		var one int
		if e := c.QueryRowContext(ctx, `SELECT 1 FROM product_plate_inputs WHERE run_id=?`, d.RunID).Scan(&one); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return si.ErrInvariant
			}
			return e
		}
		_, e := c.ExecContext(ctx, `INSERT OR IGNORE INTO product_plate_derivations(`+plateDerivationColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.RunID, d.State, d.BlockedReason, nullBytes(d.PNG), d.SHA256, d.SizeBytes, d.Width, d.Height, d.ReportJSON, d.ReportSHA256, d.CandidateState, Now().Unix())
		return e
	})
	if err != nil {
		return si.PlateDerivation{}, err
	}
	stored, ok, e := s.GetPlateDerivation(ctx, d.RunID)
	if e != nil || !ok {
		return si.PlateDerivation{}, si.ErrInvariant
	}
	return stored, nil
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// ListPlateRunsAwaitingDerivation lists plate runs whose original output is
// attached but that have no derivation record yet.
func (s *Store) ListPlateRunsAwaitingDerivation(ctx context.Context, limit int) ([]si.Run, error) {
	if limit <= 0 || limit > 10 {
		return nil, si.ErrInvalid
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE deleted=0 AND phase='succeeded' AND json_extract(observation_json,'$.Output') IS NOT NULL AND id IN (SELECT run_id FROM product_plate_inputs) AND id NOT IN (SELECT run_id FROM product_plate_derivations) ORDER BY updated_at,id LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []si.Run{}
	for rows.Next() {
		r, e := scanSource(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
````

- [ ] **Step 5: 写实现——`sourceflow`**

补丁：`Service` 新增接线字段；恢复循环在每轮末尾执行 `derivePending`；显式 `Reconcile` 在 `Advance` 之后调用 `settlePlate`；`Select` 对 plate 加门控；`OpenOutput` 拒绝 plate；`Confirm` 对未确认的 plate 报价在 flag 关闭时拒绝：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/sourceflow/types.go
+++ b/server/internal/sourceflow/types.go
@@ -3,10 +3,13 @@
 import (
 	"context"
 	"github.com/bianjiefilm/product-image-engine/server/internal/config"
+	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
+	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
 	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
 	"github.com/bianjiefilm/product-image-engine/server/internal/store"
 	"io"
 	"strings"
+	"sync"
 	"sync/atomic"
 	"time"
 )
@@ -50,6 +53,15 @@
 	Assets               AssetPort
 	Profile              Profile
 	Now                  func() time.Time
+
+	// Plate-lock wiring. All of it is assembled by cmd/server and immutable
+	// while serving; nothing here can be changed by a request.
+	PlateEnabled  bool                 // FEATURE_BG_PLATE_LOCK
+	Samples       *fidelitysamples.Set // nil unless the frozen set loaded and verified
+	SamplesReason string               // why Samples is nil: sample_set_unconfigured | sample_set_invalid
+	Build         platelock.Build      // recorded in every report
+	plateLocks    sync.Map             // run id -> *sync.Mutex (in-process derive single flight)
+	plateRetry    sync.Map             // run id -> plateBackoff
 }
 type Profile struct {
 	Enabled, OrgEnabled                               bool
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/server/internal/sourceflow/quote.go
+++ b/server/internal/sourceflow/quote.go
@@ -169,6 +169,11 @@
 	if r.Quote == nil || quoteID != r.Quote.QuoteID || hash != si.QuoteHash(*r.Quote) || r.Deleted || r.CancelRequested {
 		return r, si.ErrConflict
 	}
+	// Turning the plate-lock flag off closes NEW commitments. A run already
+	// confirmed keeps recovering and replaying; an unconfirmed quote cannot start.
+	if r.Intent.Mode == si.ModePlateLock && r.ConfirmationHash == "" && !s.PlateEnabled {
+		return r, si.ErrUnconfigured
+	}
 	if r.ConfirmationHash == "" && (s.now().Unix() < r.Quote.QuotedAtUnix || s.now().Unix() >= r.Quote.ExpiresAtUnix) {
 		return r, si.ErrConflict
 	}
PATCH2
git apply --whitespace=nowarn <<'PATCH3'
--- a/server/internal/sourceflow/recover.go
+++ b/server/internal/sourceflow/recover.go
@@ -30,6 +30,8 @@
 			_, _ = s.ObserveOutput(ctx, observed)
 		}
 	}
+	// Plate-lock derivation is local work on an already attached, paid output.
+	s.derivePending(ctx)
 	if ctx.Err() != nil {
 		return ctx.Err()
 	}
PATCH3
git apply --whitespace=nowarn <<'PATCH4'
--- a/server/internal/sourceflow/task.go
+++ b/server/internal/sourceflow/task.go
@@ -255,5 +255,12 @@
 			return r, e
 		}
 	}
-	return s.Advance(ctx, r.ID)
+	out, e := s.Advance(ctx, r.ID)
+	if e == nil {
+		s.settlePlate(ctx, out)
+		if again, readErr := s.Store.GetSourceRun(ctx, out.Intent.Scope, out.ID); readErr == nil {
+			out = again
+		}
+	}
+	return out, e
 }
PATCH4
git apply --whitespace=nowarn <<'PATCH5'
--- a/server/internal/sourceflow/output.go
+++ b/server/internal/sourceflow/output.go
@@ -107,6 +107,11 @@
 	if r.Deleted || r.Output == nil {
 		return r, si.ErrConflict
 	}
+	if r.Intent.Mode == si.ModePlateLock {
+		if e = s.requireLimitedCandidate(ctx, r); e != nil {
+			return r, e
+		}
+	}
 	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
 	defer cancel()
 	r, e = s.claimOutput(ctx, r)
@@ -151,7 +156,9 @@
 	if e != nil {
 		return nil, e
 	}
-	if r.Deleted || r.Output == nil || si.ValidateReadyOutput(r, *r.Output) != nil {
+	// A plate-lock run's own Upload asset is the unverified model plate. Only the
+	// derived, quality-judged composite may ever be shown (LoadPlate).
+	if r.Intent.Mode == si.ModePlateLock || r.Deleted || r.Output == nil || si.ValidateReadyOutput(r, *r.Output) != nil {
 		return nil, si.ErrConflict
 	}
 	if nilPort(s.Bill) || nilPort(s.Assets) {
PATCH5
````

新文件（`CreatePlate` / `DerivePlate` / `LoadPlate` / `settlePlate` / `derivePending`；派生用冻结集合里的原图字节、对每个 run 进程内单飞、下载必须与已核验的 Upload 资产大小与 SHA-256 完全一致，否则 `ErrInvariant` 且**什么都不存**）：

`server/internal/sourceflow/plate.go`

````go
package sourceflow

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// PlateRequest is everything a browser may influence: which registered input,
// which words describe the background. OriginalSHA256 is the digest of that
// input's bytes as already proven against Upload by the HTTP layer; the service
// still only accepts a digest that names a frozen sample.
type PlateRequest struct{ RequestKey, InputID, BackgroundIntent, OriginalSHA256 string }

// PlateReady reports whether new plate-lock runs may be created and why not.
// Reason codes are stable API: plate_lock_disabled, sample_set_unconfigured,
// sample_set_invalid, plate_org_not_supported, source_unconfigured.
func (s *Service) PlateReady(payerSource string) (bool, string) {
	switch {
	case s == nil || !s.PlateEnabled:
		return false, "plate_lock_disabled"
	case s.Samples == nil:
		if s.SamplesReason == "sample_set_invalid" {
			return false, "sample_set_invalid"
		}
		return false, "sample_set_unconfigured"
	case payerSource != "personal":
		return false, "plate_org_not_supported"
	}
	if _, _, ready := s.CreationCapabilities(payerSource); !ready {
		return false, "source_unconfigured"
	}
	return true, ""
}

// CreatePlate freezes the sample binding, prompt and price profile, then runs
// the normal usage and quote flow. It returns a quote and never a Task.
func (s *Service) CreatePlate(ctx context.Context, actor Actor, project string, in PlateRequest) (si.Run, error) {
	if s == nil || s.Store == nil || s.Auth == nil || nilPort(s.Bill) {
		return si.Run{}, si.ErrUnconfigured
	}
	auth, e := s.Auth.ForProject(ctx, actor, project, true)
	if e != nil {
		return si.Run{}, e
	}
	if ready, _ := s.PlateReady(auth.PayerSource); !ready || !s.Profile.CanCreate(auth.PayerSource) {
		return si.Run{}, si.ErrUnconfigured
	}
	if !si.ValidIntentText(in.BackgroundIntent) || !fs.ValidIntent(in.BackgroundIntent) {
		return si.Run{}, si.ErrInvalid
	}
	c, ok := s.Samples.BySHA(in.OriginalSHA256)
	if !ok {
		return si.Run{}, si.ErrNotFrozen
	}
	frozen := si.PlateInput{
		CaseID: c.CaseID, SetID: s.Samples.ID(), SetSHA256: s.Samples.SHA256(), InputID: in.InputID,
		OriginalSHA256: c.Original.SHA256, MaskSHA256: c.Mask.SHA256, CoverageSHA256: c.Evidence.CoverageSHA256,
		CanvasW: c.Canvas.W, CanvasH: c.Canvas.H, BackgroundIntent: in.BackgroundIntent,
		FitID: platelock.FitID, ComposeID: platelock.ComposeID, ThresholdsVersion: fs.ThresholdsVersion,
	}
	if !frozen.Valid() {
		return si.Run{}, si.ErrInvalid
	}
	intent := si.Intent{
		Scope: auth.Scope, RequestKey: in.RequestKey, Mode: si.ModePlateLock, Prompt: bgreplace.ModelPlatePrompt(in.BackgroundIntent),
		Size: s.Profile.Size, Provider: s.Profile.Provider, Model: s.Profile.Model, Capability: s.Profile.Capability,
		PricingVersion: s.Profile.PricingVersion, Quantity: s.Profile.Quantity, PlateDigest: si.PlateDigest(frozen),
	}
	r, _, e := s.Store.CreateSourcePlateRunForProject(ctx, intent, frozen)
	if e != nil {
		return r, e
	}
	if r.Quote != nil {
		return r, nil
	}
	return s.RecoverQuote(ctx, r)
}

type plateBackoff struct {
	until time.Time
	tries int
}

func (s *Service) plateLock(id string) *sync.Mutex {
	m, _ := s.plateLocks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// DerivePlate produces (or returns) the run's single derivation record. It
// makes no Task, Usage, Quote or charge call: the only remote action is the
// verified download of the already paid plate. Repeating it, concurrently or
// after a restart, returns the same stored record.
func (s *Service) DerivePlate(ctx context.Context, id string) (si.PlateDerivation, error) {
	if s == nil || s.Store == nil {
		return si.PlateDerivation{}, si.ErrUnconfigured
	}
	mu := s.plateLock(id)
	mu.Lock()
	defer mu.Unlock()
	if d, ok, e := s.Store.GetPlateDerivation(ctx, id); e != nil || ok {
		return d, e
	}
	r, e := s.Store.GetSourceRunForRecovery(ctx, id)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	if r.Intent.Mode != si.ModePlateLock || r.Deleted || r.Output == nil || r.Task == nil || r.ChargedBill == nil || r.Quote == nil || si.ValidateReadyOutput(r, *r.Output) != nil {
		return si.PlateDerivation{}, si.ErrConflict
	}
	frozen, e := s.Store.GetPlateInput(ctx, r)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	if bgreplace.ModelPlatePrompt(frozen.BackgroundIntent) != r.Intent.Prompt {
		return si.PlateDerivation{}, si.ErrInvariant
	}
	if s.Samples == nil {
		return si.PlateDerivation{}, si.ErrUnconfigured
	}
	c, ok := s.Samples.BySHA(frozen.OriginalSHA256)
	if !ok || c.CaseID != frozen.CaseID || c.Mask.SHA256 != frozen.MaskSHA256 || c.Evidence.CoverageSHA256 != frozen.CoverageSHA256 || c.Canvas.W != frozen.CanvasW || c.Canvas.H != frozen.CanvasH {
		// The approved set changed after this run was priced. Nothing may be
		// regenerated or charged again; the paid plate reference stays.
		return s.Store.SavePlateDerivation(ctx, si.PlateDerivation{RunID: id, State: si.DerivationBlocked, BlockedReason: si.BlockedSampleSetChanged, CandidateState: si.CandidateBlocked})
	}
	plate, e := s.readPlate(ctx, r)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	out := platelock.Derive(platelock.Input{
		Case: c.Case, Original: c.OriginalBytes, Mask: c.MaskBytes, CoverageSHA256: c.Evidence.CoverageSHA256, Plate: plate,
		Facts: platelock.Facts{
			Mode: r.Intent.Mode, Provider: r.Intent.Provider, Model: r.Intent.Model, PricingVersion: r.Intent.PricingVersion,
			UsageID: r.Quote.UsageID, QuoteID: r.Quote.QuoteID, TaskID: r.TaskID, OriginalChargeID: r.ChargedBill.ChargeID,
			PlateAssetID: r.Output.AssetID, PlateReferenceID: r.Output.ReferenceID, PlateSHA256: r.Output.SHA256,
			TaskSucceeded: r.Task.Status == "succeeded", PlateHashVerified: true, ChargeSeen: r.ChargedBill.Status == "charged",
		},
		Build: s.Build,
	})
	report, e := platelock.ReportJSON(out.Report)
	if e != nil {
		return si.PlateDerivation{}, e
	}
	d := si.PlateDerivation{RunID: id, State: si.DerivationDerived, PNG: out.Composite, ReportJSON: string(report), ReportSHA256: si.SHA256Hex(report), CandidateState: out.Report.CandidateState}
	if len(out.Composite) > 0 {
		d.SHA256, d.SizeBytes, d.Width, d.Height = si.SHA256Hex(out.Composite), int64(len(out.Composite)), c.Canvas.W, c.Canvas.H
	}
	return s.Store.SavePlateDerivation(ctx, d)
}

// readPlate downloads the paid plate and proves it is the verified Upload
// asset: exact size and SHA-256. Anything else is an invariant failure that
// stores nothing, so a swapped byte stream can never become a candidate.
func (s *Service) readPlate(ctx context.Context, r si.Run) ([]byte, error) {
	if nilPort(s.Assets) {
		return nil, si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, e := s.Assets.Download(ctx, r)
	if e != nil {
		return nil, e
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, 16<<20+1))
	if e != nil {
		return nil, si.ErrUnavailable
	}
	if int64(len(raw)) != r.Output.SizeBytes || si.SHA256Hex(raw) != r.Output.SHA256 {
		return nil, si.ErrInvariant
	}
	return raw, nil
}

// PlateContent is a derived composite read back for display or export.
type PlateContent struct {
	Run        si.Run
	Input      si.PlateInput
	Derivation si.PlateDerivation
}

// LoadPlate returns the run with its frozen input and derivation, proving the
// run still has a charged original bill (a later refund hides the result).
func (s *Service) LoadPlate(ctx context.Context, actor Actor, project, id string) (PlateContent, error) {
	r, e := s.LoadRun(ctx, actor, project, id, false)
	if e != nil {
		return PlateContent{}, e
	}
	if r.Intent.Mode != si.ModePlateLock || r.Deleted {
		return PlateContent{Run: r}, si.ErrConflict
	}
	frozen, e := s.Store.GetPlateInput(ctx, r)
	if e != nil {
		return PlateContent{Run: r}, e
	}
	d, ok, e := s.Store.GetPlateDerivation(ctx, r.ID)
	if e != nil || !ok {
		if e == nil {
			e = si.ErrConflict // not derived yet
		}
		return PlateContent{Run: r, Input: frozen}, e
	}
	if nilPort(s.Bill) {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, si.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	bill, found, e := s.Bill.Get(ctx, r)
	if e != nil {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, e
	}
	if !found {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, si.ErrUnavailable
	}
	if si.ValidateSettlement(r, bill) != nil || bill.Status != "charged" {
		return PlateContent{Run: r, Input: frozen, Derivation: d}, si.ErrConflict
	}
	return PlateContent{Run: r, Input: frozen, Derivation: d}, nil
}

// requireLimitedCandidate gates select, export and return.
func (s *Service) requireLimitedCandidate(ctx context.Context, r si.Run) error {
	d, ok, e := s.Store.GetPlateDerivation(ctx, r.ID)
	if e != nil {
		return e
	}
	if !ok {
		return si.ErrConflict
	}
	if d.CandidateState != si.CandidateLimited {
		return si.ErrNotUsable
	}
	return nil
}

// settlePlate lets an explicit reconcile (and the recovery loop) push a plate
// run from "Task finished" to "derived" without any new remote mutation.
func (s *Service) settlePlate(ctx context.Context, observed si.Run) {
	if observed.Intent.Mode != si.ModePlateLock || observed.ConfirmationHash == "" || observed.Task == nil || observed.Deleted {
		return
	}
	if observed.Output == nil && (observed.Phase == "asset_pending" || observed.Phase == "review_required") && (observed.Task.Status == "succeeded" || observed.Task.Status == "failed" || observed.Task.Status == "canceled") {
		next, _ := s.ObserveOutput(ctx, observed)
		observed = next
	}
	if observed.Output != nil && observed.Phase == "succeeded" {
		_, _ = s.DerivePlate(ctx, observed.ID)
	}
}

// derivePending retries derivations the loop owes, with per-run backoff so a
// persistently failing download is not hammered once a second.
func (s *Service) derivePending(ctx context.Context) {
	runs, e := s.Store.ListPlateRunsAwaitingDerivation(ctx, 3)
	if e != nil {
		return
	}
	for _, r := range runs {
		if ctx.Err() != nil {
			return
		}
		if v, ok := s.plateRetry.Load(r.ID); ok && s.now().Before(v.(plateBackoff).until) {
			continue
		}
		if _, e := s.DerivePlate(ctx, r.ID); e != nil {
			b, _ := s.plateRetry.Load(r.ID)
			tries := 1
			if prev, ok := b.(plateBackoff); ok {
				tries = prev.tries + 1
			}
			delay := time.Duration(min(1<<uint(min(tries, 6)), 60)) * time.Second
			s.plateRetry.Store(r.ID, plateBackoff{until: s.now().Add(delay), tries: tries})
			if errors.Is(e, context.Canceled) {
				return
			}
			continue
		}
		s.plateRetry.Delete(r.ID)
	}
}
````

- [ ] **Step 6: 运行，确认通过**

```bash
cd "$WT/server" && gofmt -l internal | grep -v entry_test.go; go vet ./internal/sourceimage ./internal/store ./internal/sourceflow
"$RUN/bin/heavy" -- go test -count=1 ./internal/sourceimage ./internal/store ./internal/sourceflow 2>&1 | tail -6
```

期望：三个包全 `ok`（`sourceflow` 约 20–40 秒，因为每个用例做真实的 1MP PNG 解码 / 合成）；此外**既有**的 `sourceimage` / `store` / `sourceflow` 测试一条不改、一条不挂（尤其 `TestLegacyTextIntentFingerprintIsByteStable`）。

- [ ] **Step 7: 竞态与全仓回归**

```bash
cd "$WT/server" && "$RUN/bin/heavy" -- go test -count=1 -race -run 'F09|F08|F10|F15|F17|DerivePlate|FlagOff' ./internal/sourceflow ./internal/store 2>&1 | tail -3      # 期望：两个包 ok（含 -race 约 5–8 分钟，是预期的慢）
"$RUN/bin/heavy" -- go test -p 2 -count=1 ./... 2>&1 | tee "$EV/task3-go-test.log" | grep -v "^ok" | head   # 期望：无输出（即全部 ok）
go vet ./...                                                                                           # 期望：无输出
```

- [ ] **Step 8: 提交**

```bash
cd "$WT" && git add server/internal/sourceimage server/internal/store server/internal/sourceflow
git status --short      # 只能出现 sourceimage / store / sourceflow 下的文件
git commit -m "feat(source): run background_plate_lock through the priced source Task

冻结输入（样本/蒙版/覆盖摘要/背景文字）绑进 Intent.PlateDigest；底板 Task 成功且
已扣费后，由恢复循环或显式 reconcile 触发写一次的派生（下载必须与已核验资产
大小与 SHA 一致 → platelock.Derive → 派生记录）。选定要求 limited_candidate，
plate 的 Upload 资产永不作为结果展示；flag 关闭只拒未确认的新报价。
迁移 0020 只加表，旧 text_generate 行与指纹字节不变（黄金摘要测试）。

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


---

## Task 4：HTTP 面、旧直调路由退役、回流质量绑定与进程接线

**交付物：** 浏览器可用的限定保真 API（创建 / 能力 / 视图 / 内容 / 导出 / 选定 / 回传）、对用户输入的服务端封闭校验、旧 `model-plate` 直调路由永久 410、回流质量绑定（只有 `limited_candidate` 能回传，回执只引用原 charge）、`/readyz` 增加构建身份与 `plate_lock` 门、`cmd/server` 装载样本集（失败只关本模式）。对应 spec §4.3（运行时部分）、§4.4 第 6–8 点、§4.7、R6、R12、R18；反例 F04、F05、F11–F16、F19–F21。

**Files:**
- Create: `server/internal/store/migrations/0021_plate_output_quality.sql`、`server/internal/store/{plate_output.go,plate_output_test.go}`
- Create: `server/internal/httpapi/{source_plate.go,source_plate_test.go}`
- Modify: `server/internal/httpapi/{server.go,source_image.go,source_image_content.go,source_image_view.go,handoff.go,bgreplace.go,bgreplace_test.go,source_image_test.go}`、`server/cmd/server/{source_runtime.go,source_runtime_test.go}`

**Interfaces:**
- Consumes（Task 1–3）：`sourceflow.Service` 的 `PlateReady/CreatePlate/LoadPlate/Select/PlateEnabled/Samples/SamplesReason/Build`、`sourceflow.PlateRequest`、`sourceflow.PlateContent`；`store` 的 `GetPlateInput/GetPlateDerivation`；`platelock.DecodeReport/Claim`；`fidelitysamples.Load/Scope/LoadedCase`；`buildinfo.Read`；`si.ErrNotFrozen/ErrNotUsable/SHA256Hex/ValidIntentText/ModePlateLock`。
- Produces（Task 5 / 6 的契约，字段名逐字使用）：
  - `POST /api/v1/projects/{id}/source-image-runs`：当 body 的 `mode == "background_plate_lock"`，封闭键集 `{request_key, mode, input_id, background_intent}`（全是字符串，多一个 / 少一个 / 重复 / 非字符串 → 400）。错误：`422 sample_not_frozen`、`503 <plate_lock_disabled|sample_set_unconfigured|sample_set_invalid|plate_org_not_supported|source_unconfigured|…>`、`409 original_missing`。
  - `GET …/source-image-capabilities` 的 `background_plate_lock` 项：`{mode, ready, disabled, reason, can_quote, can_confirm, size:"1024*1024", model, claim:"frozen_synthetic_sample_only", fidelity:"frozen_sample_only", visual_quality:"unknown", supported_input_ids:[…], supported_inputs:[{input_id, kind, canvas:{width_px,height_px}}]}`（纯本地读，不访问远端、不写库）。
  - plate run 的视图：`mode:"background_plate_lock"`、`output:null`（原始底板绝不展示）、`fidelity:"frozen_sample_only"`、`plate:{result_kind:"derived_composite", claim, human_usefulness:"NOT_RUN", derivation_state:"pending|derived|blocked|invalid", candidate_state:"limited_candidate|pending|not_usable_candidate|blocked", sample_set_id, case_id, background_intent, canvas, blocked_reason?, plate_task?, verdicts?, axes?, limits?, report_sha256?, composite_sha256?, result?}`；`text_generate` 的视图**没有** `plate` 键、`fidelity` 仍是 `not_applicable`。
  - `GET …/{runId}/content`：plate → 派生合成 PNG（`X-Quality-Verdict: limited|fail|pending`，读时再核对 Bill 仍 `charged`）；`GET …/{runId}/export`：plate → 仅 `limited_candidate`，zip 含 `image.png`（派生合成）+ `quality.json`（冻结的 v2 报告，必须规范且与合成绑定）+ `snapshot.json`。
  - `POST …/{runId}/select`：不可用候选 → `409 not_usable_candidate`。`POST …/{runId}/return`（空 body）：要求已选定的 `limited_candidate` 且工程有来源绑定，把**派生合成字节**经既有登记事实路径登记为工程输出并写质量绑定 → `201 {output, quality, duplicate}`，错误 `409 not_selected | no_source_binding | not_usable_candidate`。
  - 通用 `POST /projects/{id}/outputs` 对任何 plate 派生合成字节 → `409 source_output_owned`（唯一合法入口是上面的 `return`）。`GET /projects/{id}/outputs` 与 `GET /bindings/{id}/return-target` 增加 `quality_bindings:[{output_id, run_id, candidate_state, scope, limits, report_sha256, result_sha256, receipt_includes_limits:false}]`。回执：存在质量绑定时 `run_id` = source run 号、`billing_fact_refs = [原 charge id]`；未绑定的派生合成 → `409 not_usable_candidate`。
  - `POST /projects/{id}/background-replacements/{jobId}/model-plate` → `410 direct_supplier_route_retired`（不读 body、不调模型、不写库）。
  - `GET /readyz` 增加 `build:{vcs_revision, vcs_modified}` 与 `gates.plate_lock:{enabled, usable, reason, samples_loaded}`（样本集被拒不让 `/readyz` 变 503）。
  - Go：`(*Server).ReadOriginal func(ctx, platform.Principal, tenant, project, input string) ([]byte, error)`（测试接缝，生产为 nil 走 Upload）、`store.OutputQuality` 与 `SaveOutputQuality/GetOutputQuality/ListOutputQualities/PlateDerivationOwnsSHA`；`Server.PlateCoverage` 字段**删除**。

- [ ] **Step 1: 先写失败的测试**

`store`：回流质量绑定只接受“与输出行 / 派生记录完全一致的 `limited_candidate`”、幂等、不可改：

`server/internal/store/plate_output_test.go`

````go
package store

import (
	"strings"
	"testing"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

func TestOutputQualityBindsOnlyAMatchingLimitedDerivation(t *testing.T) {
	s, _, proj := openPlateStore(t)
	frozen := plateFrozen("深蓝色背景")
	run, _, _ := s.CreateSourcePlateRunForProject(t.Context(), plateIntent(proj.ID, frozen), frozen)
	composite := []byte("\x89PNG-composite")
	report := `{"report_version":"product-source-quality/v2"}`
	if _, e := s.SavePlateDerivation(t.Context(), si.PlateDerivation{RunID: run.ID, State: si.DerivationDerived, PNG: composite, SHA256: si.SHA256Hex(composite), SizeBytes: int64(len(composite)), Width: 1024, Height: 1024, ReportJSON: report, ReportSHA256: si.SHA256Hex([]byte(report)), CandidateState: si.CandidateLimited}); e != nil {
		t.Fatal(e)
	}
	owner, hit, e := s.PlateDerivationOwnsSHA(t.Context(), "acct_u", proj.ID, si.SHA256Hex(composite))
	if e != nil || !hit || owner != run.ID {
		t.Fatalf("composite digest must resolve to its run: %q %v %v", owner, hit, e)
	}
	if _, hit, _ := s.PlateDerivationOwnsSHA(t.Context(), "acct_other", proj.ID, si.SHA256Hex(composite)); hit {
		t.Fatal("another tenant must not resolve it")
	}
	out, e := s.CreateOutput(t.Context(), ProjectOutput{TenantScope: "acct_u", ProjectID: proj.ID, PlatformAssetID: "asset_out", ResultSHA256: si.SHA256Hex(composite), ResultSize: int64(len(composite)), MediaType: "image/png", FileName: "x.png"})
	if e != nil {
		t.Fatal(e)
	}
	q := OutputQuality{OutputID: out.ID, TenantScope: "acct_u", ProjectID: proj.ID, RunID: run.ID, ResultSHA256: out.ResultSHA256, CandidateState: si.CandidateLimited, OriginalChargeID: "charge_a", ScopeJSON: `{"claim":"frozen_synthetic_sample_only"}`, LimitsJSON: `["limit"]`, ReportSHA256: si.SHA256Hex([]byte(report))}
	saved, e := s.SaveOutputQuality(t.Context(), q)
	if e != nil || saved.OriginalChargeID != "charge_a" {
		t.Fatal(saved, e)
	}
	if again, e := s.SaveOutputQuality(t.Context(), q); e != nil || again != saved {
		t.Fatalf("saving twice returns the stored binding: %v", e)
	}
	for name, mutate := range map[string]func(*OutputQuality){
		"another output digest": func(x *OutputQuality) { x.ResultSHA256 = strings.Repeat("9", 64) },
		"a failing candidate":   func(x *OutputQuality) { x.CandidateState = si.CandidateNotUsable },
		"another report":        func(x *OutputQuality) { x.ReportSHA256 = strings.Repeat("8", 64) },
		"no original charge":    func(x *OutputQuality) { x.OriginalChargeID = "" },
	} {
		bad := q
		bad.OutputID = "out_missing"
		mutate(&bad)
		if _, e := s.SaveOutputQuality(t.Context(), bad); e == nil {
			t.Fatalf("%s was bound", name)
		}
	}
	if _, e := s.db.Exec(`UPDATE project_output_quality SET candidate_state='limited_candidate'`); e == nil {
		t.Fatal("a quality binding is immutable")
	}
	list, e := s.ListOutputQualities(t.Context(), "acct_u", proj.ID)
	if e != nil || len(list) != 1 || list[0].OutputID != out.ID {
		t.Fatal(list, e)
	}
}
````

`httpapi`：真实 Router + 真实 SQLite + 真实 PNG，Billing / Task / Upload 为既有的测试端口夹具（`newSourceHTTPFixture` 先做一个小重构以便注入配置，补丁见下）。覆盖：能力端点的真值（已冻结输入被列出、普通照片不被列出、各原因码、纯读零变更）、**创建在任何付费调用之前的全部拒绝**（浏览器夹带 `mask / sample_id / passed / prompt / amount`、缺 / 重复键、控制字符、201 字、前后空格、普通照片 422、冻结登记但字节差一个像素的 F05、开关关 / 样本集被拒）、端到端快乐路径（内容 = 派生合成而不是底板、每个受保护像素与原图相同、导出 = `image.png` + 规范报告且二者绑定、usage / quote / submit 各恰好 1 次、底板只下载 1 次）、不可用候选可看但不可选 / 导出 / 回传（F13）且字节推不进通用 `/outputs`（F19）、跨账号全 404 且不泄漏（F11）、旧直调路由 410 且零模型调用（F21）、回流绑定质量并对回执门控（回执引用原 charge）、旧需求版本回流被拒（F12）、未绑定质量的派生合成不能回执、普通文字 run 永不带保真字样且 `reference_edit` 恒关闭（F20）、`/readyz`、关开关只关新工作不影响历史 / 恢复 / 删除：

`server/internal/httpapi/source_plate_test.go`

````go
package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	fs "github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

type plateHTTPFixture struct {
	*sourceHTTPFixture
	eco       *ecoStub
	set       *fs.Set
	c         *fs.LoadedCase
	inputID   string
	reads     atomic.Int64 // calls to the Upload original-read seam
	servedPNG []byte       // what the seam returns; defaults to the frozen original
}

func gradientPNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(20 + x*60/w), uint8(50 + y*90/h), uint8(110 + (x+y)*40/(w+h)), 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

func newPlateHTTPFixture(t *testing.T, caseID string) *plateHTTPFixture {
	t.Helper()
	eco := newEcoStub(t)
	sf := newSourceHTTPFixtureWith(t, func(c *config.Config) {
		c.UploadBaseURL, c.UploadToken = eco.srv.URL, "up-tok"
		c.ReceiptKeys = "orders:" + testSecret
		c.BgPlateLockEnabled = true
	})
	attachRegistry(t, sf.fixture, mustRegistry(t, eco.srv.URL+"/internal/v1/handoff/receipts"))
	set, err := fs.Load(sampletest.Dir(t))
	if err != nil {
		t.Fatal(err)
	}
	var c *fs.LoadedCase
	for _, x := range set.Cases() {
		if x.CaseID == caseID {
			c = x
		}
	}
	f := &plateHTTPFixture{sourceHTTPFixture: sf, eco: eco, set: set, c: c, servedPNG: c.OriginalBytes}
	svc := f.srv.Source
	svc.PlateEnabled, svc.Samples, svc.OutputRecoveryReady = true, set, true
	f.srv.ReadOriginal = func(context.Context, platform.Principal, string, string, string) ([]byte, error) {
		f.reads.Add(1)
		return f.servedPNG, nil
	}
	f.inputID = f.addInput(t, "asset_frozen", c.Original.SHA256)
	return f
}

// addInput attaches a project input whose local photo registry row carries sha.
func (f *plateHTTPFixture) addInput(t *testing.T, asset, sha string) string {
	t.Helper()
	in, err := f.st.AddInput(t.Context(), store.ProjectInput{ProjectID: f.project.ID, TenantID: f.actor.AccountID, PlatformAssetID: asset, SnapshotName: asset, SnapshotSize: 100, SnapshotContentType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.st.UpsertPhotoAsset(t.Context(), store.PhotoAsset{TenantID: f.actor.AccountID, SHA256: sha, SizeBytes: 100, MediaType: "image/png", WidthPx: 100, HeightPx: 100, Purpose: "product_photo", PlatformAssetID: asset, OriginalName: asset + ".png", CreatedBy: f.actor.UserID}); err != nil {
		t.Fatal(err)
	}
	return in.ID
}

func (f *plateHTTPFixture) createBody(key, input, intent string) string {
	b, _ := json.Marshal(map[string]string{"request_key": key, "mode": si.ModePlateLock, "input_id": input, "background_intent": intent})
	return string(b)
}

func (f *plateHTTPFixture) create(t *testing.T, key string) (int, map[string]any) {
	t.Helper()
	w := f.raw(t, "POST", f.path(""), f.token, f.createBody(key, f.inputID, f.c.Intents[0]))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (f *plateHTTPFixture) plateOf(t *testing.T, view map[string]any) map[string]any {
	t.Helper()
	plate, ok := view["plate"].(map[string]any)
	if !ok {
		t.Fatalf("a plate-lock run view must carry plate: %v", view)
	}
	return plate
}

// finish confirms the quote, lets the fake platform finish and charge the Task
// with `plate` as its output, and drives the run to a derivation through the
// ordinary reconcile route.
func (f *plateHTTPFixture) finish(t *testing.T, key string, plate []byte) (string, map[string]any) {
	t.Helper()
	code, view := f.create(t, key)
	if code != 200 {
		t.Fatal(code, view)
	}
	runID, _ := view["run_id"].(string)
	quote, _ := view["quote"].(map[string]any)
	body, _ := json.Marshal(map[string]string{"quote_id": quote["quote_id"].(string), "quote_fingerprint": quote["quote_fingerprint"].(string)})
	if w := f.raw(t, "POST", f.path("/"+runID+"/confirm"), f.token, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	n := int64(25)
	o := si.Output{AssetID: "asset_plate", ReferenceID: "reference_plate", ProjectID: f.project.ID, AppID: "product-image", PrincipalType: "user", PrincipalID: f.actor.UserID, SHA256: si.SHA256Hex(plate), ContentType: "image/png", Status: "ready", ReferenceState: "active", SizeBytes: int64(len(plate))}
	f.ports.mu.Lock()
	f.ports.payload = string(plate)
	f.ports.task.Status, f.ports.task.Phase, f.ports.task.Output, f.ports.task.HoldID, f.ports.task.ChargeID = "succeeded", "succeeded", &o, "hold_plate", "charge_plate"
	f.ports.bill.Status, f.ports.bill.HoldID, f.ports.bill.ChargeID, f.ports.bill.ChargedMinor = "charged", "hold_plate", "charge_plate", &n
	f.ports.mu.Unlock()
	w := f.raw(t, "POST", f.path("/"+runID+"/reconcile"), f.token, `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return runID, out
}

func TestPlateCapabilitiesAreTruthfulAndReadOnly(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	other := f.addInput(t, "asset_customer", si.SHA256Hex([]byte("a customer photo")))
	before := f.ports.mutations()
	read := func() map[string]any {
		w := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var body struct {
			Modes []map[string]any `json:"modes"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		for _, m := range body.Modes {
			if m["mode"] == si.ModePlateLock {
				return m
			}
		}
		t.Fatal("no background_plate_lock entry")
		return nil
	}
	m := read()
	ids, _ := m["supported_input_ids"].([]any)
	if m["ready"] != true || m["reason"] != "" || len(ids) != 1 || ids[0] != f.inputID || m["claim"] != "frozen_synthetic_sample_only" {
		t.Fatalf("a frozen input is supported and an ordinary photo is not: %v (other=%s)", m, other)
	}
	for _, tc := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{"flag off", func() { f.srv.Source.PlateEnabled = false }, "plate_lock_disabled"},
		{"no sample directory", func() { f.srv.Source.PlateEnabled, f.srv.Source.Samples = true, nil }, "sample_set_unconfigured"},
		{"rejected sample set", func() { f.srv.Source.SamplesReason = "sample_set_invalid" }, "sample_set_invalid"},
	} {
		tc.mutate()
		if got := read(); got["ready"] != false || got["disabled"] != true || got["reason"] != tc.want {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
	f.srv.Source.PlateEnabled, f.srv.Source.Samples, f.srv.Source.SamplesReason = true, f.set, ""
	if f.reads.Load() != 0 || f.ports.mutations() != before {
		t.Fatalf("capabilities is a pure local read: reads=%d mutations=%v", f.reads.Load(), f.ports.mutations())
	}
}

func TestPlateCreateRefusesBeforeAnyPaidCall(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	customer := f.addInput(t, "asset_customer", si.SHA256Hex([]byte("a customer photo")))
	good := f.createBody("k1", f.inputID, f.c.Intents[0])
	cases := []struct {
		name, body string
		want       int
		code       string
		reads      int64
	}{
		{"F14 browser sends a mask", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","mask_png_base64":"AAAA"}`, 400, "invalid_request", 0},
		{"F14 browser sends sample_id", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","sample_id":"carton-1024x1024"}`, 400, "invalid_request", 0},
		{"F14 browser claims passed", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","passed":"true"}`, 400, "invalid_request", 0},
		{"F14 browser sends a prompt", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","prompt":"draw"}`, 400, "invalid_request", 0},
		{"F14 browser sends an amount", `{"request_key":"k","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x","amount_minor":"1"}`, 400, "invalid_request", 0},
		{"missing input_id", `{"request_key":"k","mode":"background_plate_lock","background_intent":"x"}`, 400, "invalid_request", 0},
		{"duplicate key", `{"request_key":"k","request_key":"j","mode":"background_plate_lock","input_id":"` + f.inputID + `","background_intent":"x"}`, 400, "invalid_request", 0},
		{"control character in the background", f.createBody("k", f.inputID, "a\nb"), 400, "invalid_request", 0},
		{"background over 200 characters", f.createBody("k", f.inputID, strings.Repeat("界", 201)), 400, "invalid_request", 0},
		{"background with surrounding spaces", f.createBody("k", f.inputID, " 深蓝 "), 400, "invalid_request", 0},
		{"F04 an ordinary photo", f.createBody("k", customer, "深蓝背景"), 422, "sample_not_frozen", 0},
		{"another project's input", f.createBody("k", "pin_does_not_exist", "深蓝背景"), 404, "not_found", 0},
		{"F05 registry says frozen but the bytes differ by one pixel", f.createBody("k", f.inputID, "深蓝背景"), 422, "sample_not_frozen", 1},
	}
	f.servedPNG = func() []byte {
		img, _ := png.Decode(bytes.NewReader(f.c.OriginalBytes))
		n := image.NewNRGBA(img.Bounds())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				n.Set(x, y, img.At(x, y))
			}
		}
		n.SetNRGBA(0, 0, color.NRGBA{9, 9, 9, 255})
		var b bytes.Buffer
		_ = png.Encode(&b, n)
		return b.Bytes()
	}()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := f.reads.Load()
			w := f.raw(t, "POST", f.path(""), f.token, tc.body)
			if w.Code != tc.want || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			if got := f.reads.Load() - before; got != tc.reads {
				t.Fatalf("Upload reads = %d, want %d", got, tc.reads)
			}
			if f.ports.mutations() != [6]int{} {
				t.Fatalf("a refused request reached Billing/Task: %v", f.ports.mutations())
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{"flag off", func() { f.srv.Source.PlateEnabled = false }, "plate_lock_disabled"},
		{"F16 rejected sample set", func() {
			f.srv.Source.PlateEnabled, f.srv.Source.Samples, f.srv.Source.SamplesReason = true, nil, "sample_set_invalid"
		}, "sample_set_invalid"},
	} {
		tc.mutate()
		w := f.raw(t, "POST", f.path(""), f.token, good)
		if w.Code != 503 || !strings.Contains(w.Body.String(), tc.want) || f.ports.mutations() != [6]int{} {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
}

func TestPlateHappyPathEndToEndOverHTTP(t *testing.T) {
	f := newPlateHTTPFixture(t, "handled_metal-1024x1280")
	plate := gradientPNG(1024, 1024)
	runID, view := f.finish(t, "plate-http", plate)
	if view["mode"] != si.ModePlateLock || view["output"] != nil || view["fidelity"] != "frozen_sample_only" {
		t.Fatalf("the raw plate must never be the advertised output: %v", view)
	}
	pl := f.plateOf(t, view)
	if pl["candidate_state"] != "limited_candidate" || pl["derivation_state"] != "derived" || pl["result_kind"] != "derived_composite" || pl["human_usefulness"] != "NOT_RUN" {
		t.Fatalf("%v", pl)
	}
	if verdicts, _ := pl["verdicts"].(map[string]any); verdicts["fidelity"] != "PASS" || verdicts["generation"] != "PASS" || verdicts["billing"] != "PASS" {
		t.Fatalf("%v", pl["verdicts"])
	}
	if limits, _ := pl["limits"].([]any); len(limits) < 4 {
		t.Fatalf("a limited candidate must state its limits: %v", pl["limits"])
	}
	if raw, _ := json.Marshal(view); strings.Contains(string(raw), "score") || strings.Contains(string(raw), "similarity") {
		t.Fatalf("no score may appear in a plate view: %s", raw)
	}
	// content: the derived composite, not the paid plate.
	w := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Quality-Verdict") != "limited" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(w.Code, w.Header())
	}
	composite := w.Body.Bytes()
	if si.SHA256Hex(composite) != pl["composite_sha256"] || bytes.Equal(composite, plate) {
		t.Fatal("content must be the stored derived composite")
	}
	img, err := png.Decode(bytes.NewReader(composite))
	if err != nil || img.Bounds().Dx() != 1024 || img.Bounds().Dy() != 1280 {
		t.Fatalf("composite must be the case canvas: %v %v", img.Bounds(), err)
	}
	if _, err := bgreplace.SubjectPixelChecks(f.c.OriginalBytes, composite, f.c.MaskBytes); err != nil {
		t.Fatalf("every protected pixel must equal the original: %v", err)
	}
	// select, then export.
	if w := f.raw(t, "POST", f.path("/"+runID+"/select"), f.token, `{}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"selected":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "GET", f.path("/"+runID+"/export"), f.token, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(w.Code, w.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, zf := range zr.File {
		rc, _ := zf.Open()
		files[zf.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	if len(files) != 3 || !bytes.Equal(files["image.png"], composite) {
		t.Fatalf("export is image.png + quality.json + snapshot.json of the same composite: %v", len(files))
	}
	if rep, err := platelock.DecodeReport(files["quality.json"]); err != nil || rep.Facts.CompositeSHA256 != si.SHA256Hex(composite) || rep.Facts.OriginalChargeID != "charge_plate" {
		t.Fatalf("quality.json must be the frozen canonical report bound to this composite: %v", err)
	}
	// One run, one Task submit, one plate download; nothing was charged twice.
	if got := f.ports.mutations(); got[0] != 1 || got[1] != 1 || got[2] != 1 {
		t.Fatalf("usage/quote/submit must each be exactly one: %v", got)
	}
	if f.ports.downloads != 1 {
		t.Fatalf("the paid plate is downloaded once for derivation, never for display: %d", f.ports.downloads)
	}
}

func TestPlateUnusableCandidateIsVisibleButNeverSelectableExportableOrReturnable(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	// The "model" returned the original image as the background: F07.
	runID, view := f.finish(t, "plate-bad", f.c.OriginalBytes)
	pl := f.plateOf(t, view)
	if pl["candidate_state"] != "not_usable_candidate" {
		t.Fatalf("%v", pl)
	}
	failed := false
	for _, a := range pl["axes"].([]any) {
		if am := a.(map[string]any); am["name"] == platelock.AxisBackground && am["state"] == "FAIL" {
			failed = true
		}
	}
	if !failed {
		t.Fatal("background_changed must be FAIL and visible")
	}
	w := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, "")
	if w.Code != 200 || w.Header().Get("X-Quality-Verdict") != "fail" {
		t.Fatal(w.Code, w.Header())
	}
	composite := w.Body.Bytes()
	for _, tc := range []struct{ method, suffix, body string }{{"POST", "/select", `{}`}, {"GET", "/export", ""}, {"POST", "/return", `{}`}} {
		w := f.raw(t, tc.method, f.path("/"+runID+tc.suffix), f.token, tc.body)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"not_usable_candidate"`) {
			t.Fatalf("F13 %s: %d %s", tc.suffix, w.Code, w.Body.String())
		}
	}
	// F19: the failing composite's bytes cannot be pushed in through generic outputs.
	body, _ := json.Marshal(map[string]any{"file_name": "x.png", "content_type": "image/png", "data_b64": b64(composite)})
	w = f.raw(t, "POST", "/api/v1/projects/"+f.project.ID+"/outputs", f.token, string(body))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "source_output_owned") {
		t.Fatalf("F19: %d %s", w.Code, w.Body.String())
	}
	if f.eco.uploads.Load() != 0 {
		t.Fatalf("nothing may be registered upstream: %d", f.eco.uploads.Load())
	}
	if got := f.ports.mutations(); got[2] != 1 {
		t.Fatalf("an unusable result must not trigger regeneration: %v", got)
	}
}

func TestPlateStaysInsideItsOwnAccount(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	runID, _ := f.finish(t, "plate-own", gradientPNG(1024, 1024))
	stranger := f.stub.mint("stranger@example.com", "product-image")
	for _, tc := range []struct{ method, suffix, body string }{
		{"GET", "", ""}, {"GET", "/content", ""}, {"GET", "/export", ""}, {"POST", "/select", `{}`}, {"POST", "/return", `{}`}, {"DELETE", "/output", `{}`},
	} {
		w := f.raw(t, tc.method, f.path("/"+runID+tc.suffix), stranger, tc.body)
		if w.Code != 404 {
			t.Fatalf("F11 %s %s: %d %s", tc.method, tc.suffix, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "carton") || strings.Contains(w.Body.String(), f.c.Original.SHA256) {
			t.Fatalf("a stranger learned something: %s", w.Body.String())
		}
	}
}

func TestF21RetiredModelPlateRouteNeverCallsTheModel(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, func(c *config.Config) {
		c.BgReplaceEnabled = true
		c.BgModelCredential, c.BgModelURL, c.BgModelName = "cred", "https://model.example.invalid/v1", "m"
	})
	f.srv.ImageHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		panic("the retired route reached an image model")
	})}
	f.rearm(t)
	tok := f.stub.mint("source@example.com", "product-image")
	for _, body := range []string{"", `{"mask_png_base64":"AAAA"}`, `not json`} {
		st, m := f.do(t, "POST", "/api/v1/projects/prj_x/background-replacements/job_x/model-plate", tok, json.RawMessage(nil))
		_ = body
		if st != http.StatusGone {
			t.Fatalf("model-plate must be 410, got %d %v", st, m)
		}
		if code, _ := errCodeOf(m); code != "direct_supplier_route_retired" {
			t.Fatalf("%v", m)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("image model calls = %d", calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPlateReturnBindsQualityGatesReceiptAndKeepsVersions(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	tenant := f.actor.AccountID
	doc := handoffDoc("h-plate-1", "orders", "order", tenant, "src-plate", "bind-plate-1", "rev-1", "brief-1")
	w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(doc, "主图")))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	projID := accepted["project"].(map[string]any)["id"].(string)
	// Run the whole plate flow inside the order-bound project.
	f.project.ID = projID
	f.inputID = f.addInput(t, "asset_frozen", f.c.Original.SHA256) // same registered photo, this project
	runID, _ := f.finish(t, "plate-return", gradientPNG(1024, 1024))
	// Not selected yet: refuse.
	if w := f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`); w.Code != 409 || !strings.Contains(w.Body.String(), "not_selected") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := f.raw(t, "POST", f.path("/"+runID+"/select"), f.token, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var ret struct {
		Output  store.ProjectOutput `json:"output"`
		Quality map[string]any      `json:"quality"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ret)
	if ret.Quality["candidate_state"] != "limited_candidate" || ret.Quality["receipt_includes_limits"] != false || ret.Quality["run_id"] != runID {
		t.Fatalf("%v", ret.Quality)
	}
	content := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, "").Body.Bytes()
	if ret.Output.ResultSHA256 != si.SHA256Hex(content) {
		t.Fatal("the returned output must be exactly the displayed composite")
	}
	// Idempotent: a second return registers nothing new.
	again := f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`)
	if again.Code != 200 || f.eco.uploads.Load() != 1 {
		t.Fatalf("second return: %d uploads=%d", again.Code, f.eco.uploads.Load())
	}
	// The listing carries the quality binding (state, scope, limits, report digest).
	listed := f.raw(t, "GET", "/api/v1/projects/"+projID+"/outputs", f.token, "")
	if !strings.Contains(listed.Body.String(), `"quality_bindings"`) || !strings.Contains(listed.Body.String(), "frozen_synthetic_sample_only") {
		t.Fatalf("%s", listed.Body.String())
	}
	// Receipt: cites the original charge and the source run; delivered once.
	r := f.raw(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+ret.Output.ID+"/receipt", f.token, `{}`)
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	f.eco.mu.Lock()
	ev := f.eco.events[0]
	f.eco.mu.Unlock()
	refs, _ := ev["billing_fact_refs"].([]any)
	if ev["run_id"] != runID || len(refs) != 1 || refs[0] != "charge_plate" {
		t.Fatalf("receipt must cite the source run and the original charge: %v", ev)
	}
	assets, _ := ev["assets"].([]any)
	if a0, _ := assets[0].(map[string]any); a0["sha256"] != ret.Output.ResultSHA256 {
		t.Fatalf("receipt asset must be the returned composite: %v", a0)
	}
}

func TestPlateReturnOfAnOldRequirementVersionIsRefused(t *testing.T) {
	f := newPlateHTTPFixture(t, "glass_bottle-1024x1024")
	tenant := f.actor.AccountID
	w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(handoffDoc("h-plate-v1", "orders", "order", tenant, "src-plate-v", "bind-plate-v1", "rev-1", "brief-1"), "主图")))
	var accepted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	projID := accepted["project"].(map[string]any)["id"].(string)
	f.project.ID = projID
	f.inputID = f.addInput(t, "asset_frozen", f.c.Original.SHA256)
	runID, _ := f.finish(t, "plate-stale", gradientPNG(1024, 1024))
	if w := f.raw(t, "POST", f.path("/"+runID+"/select"), f.token, `{}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.raw(t, "POST", f.path("/"+runID+"/return"), f.token, `{}`)
	var ret struct {
		Output store.ProjectOutput `json:"output"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ret)
	if w.Code != 201 || ret.Output.ID == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	// A newer requirement arrives before the receipt is sent.
	if w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(handoffDoc("h-plate-v2", "orders", "order", tenant, "src-plate-v", "bind-plate-v2", "rev-2", "brief-2"), "主图"))); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before, hadBefore, _ := f.st.GetOutputQuality(t.Context(), tenant, ret.Output.ID)
	r := f.raw(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+ret.Output.ID+"/receipt", f.token, `{}`)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "stale_requirement_version") || f.eco.eventCount() != 0 {
		t.Fatalf("F12: %d %s events=%d", r.Code, r.Body.String(), f.eco.eventCount())
	}
	after, hasAfter, _ := f.st.GetOutputQuality(t.Context(), tenant, ret.Output.ID)
	if !hadBefore || !hasAfter || before != after {
		t.Fatal("the quality binding must survive unchanged")
	}
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestPlateReceiptRefusesAnUnboundDerivedComposite(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	tenant := f.actor.AccountID
	doc := handoffDoc("h-plate-u", "orders", "order", tenant, "src-plate-u", "bind-plate-u", "rev-1", "brief-1")
	w := f.raw(t, "POST", "/api/v1/handoffs/accept", f.token, jsonString(acceptBody(doc, "主图")))
	var accepted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	projID := accepted["project"].(map[string]any)["id"].(string)
	f.project.ID = projID
	f.inputID = f.addInput(t, "asset_frozen", f.c.Original.SHA256)
	runID, _ := f.finish(t, "plate-unbound", gradientPNG(1024, 1024))
	d, ok, err := f.st.GetPlateDerivation(t.Context(), runID)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	// An interrupted return left an output row without its quality binding.
	out, err := f.st.CreateOutput(t.Context(), store.ProjectOutput{TenantScope: tenant, ProjectID: projID, PlatformAssetID: "asset_x", ResultSHA256: d.SHA256, ResultSize: d.SizeBytes, MediaType: "image/png", FileName: "x.png"})
	if err != nil {
		t.Fatal(err)
	}
	r := f.raw(t, "POST", "/api/v1/projects/"+projID+"/outputs/"+out.ID+"/receipt", f.token, `{}`)
	if r.Code != 409 || !strings.Contains(r.Body.String(), "not_usable_candidate") || f.eco.eventCount() != 0 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
}

// F20 and the mode closure: an ordinary text run never carries a fidelity claim,
// and modes without real success evidence stay closed whatever else is enabled.
func TestOrdinaryTextRunNeverCarriesAFidelityClaimAndClosedModesStayClosed(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	// Text run in the same fixture, through the ordinary text request body.
	w := f.raw(t, "POST", f.path(""), f.token, `{"request_key":"request-text","mode":"text_generate","prompt":"蓝色纸盒","size":"1024*1024"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var view map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	if _, has := view["plate"]; has || view["fidelity"] != "not_applicable" || view["mode"] != "text_generate" {
		t.Fatalf("a text run must stay a text run: %v", view)
	}
	var runID, _ = view["run_id"].(string)
	got := f.raw(t, "GET", f.path("/"+runID), f.token, "")
	if strings.Contains(got.Body.String(), `"plate"`) || strings.Contains(got.Body.String(), "frozen_sample_only") || strings.Contains(got.Body.String(), "axes") {
		t.Fatalf("text view must not mention plate quality: %s", got.Body.String())
	}
	caps := f.raw(t, "GET", "/api/v1/projects/"+f.project.ID+"/source-image-capabilities", f.token, "")
	var body struct {
		Modes []map[string]any `json:"modes"`
	}
	_ = json.Unmarshal(caps.Body.Bytes(), &body)
	closed := 0
	for _, m := range body.Modes {
		if m["mode"] == "reference_edit" {
			closed++
			if m["ready"] != false || m["disabled"] != true || m["reason"] != "formal_contract_unconfigured" {
				t.Fatalf("reference_edit must stay closed: %v", m)
			}
		}
		if m["mode"] == "text_generate" && m["fidelity"] != "not_applicable" {
			t.Fatalf("text_generate must never claim fidelity: %v", m)
		}
	}
	if closed != 1 {
		t.Fatalf("modes = %v", body.Modes)
	}
}

func TestReadyzReportsBuildIdentityAndThePlateGate(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	read := func(t *testing.T, srv *plateHTTPFixture) map[string]any {
		t.Helper()
		w := srv.raw(t, "GET", "/readyz", "", "")
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body
	}
	body := read(t, f)
	build, _ := body["build"].(map[string]any)
	if _, ok := build["vcs_revision"]; !ok {
		t.Fatalf("readyz must carry build identity: %v", body["build"])
	}
	if _, ok := build["vcs_modified"].(bool); !ok {
		t.Fatalf("vcs_modified must be a boolean: %v", build)
	}
	gate, _ := body["gates"].(map[string]any)["plate_lock"].(map[string]any)
	if gate["enabled"] != true || gate["usable"] != true || gate["samples_loaded"] != true || gate["reason"] != "" {
		t.Fatalf("%v", gate)
	}
	f.srv.Source.SamplesReason, f.srv.Source.Samples = "sample_set_invalid", nil
	gate, _ = read(t, f)["gates"].(map[string]any)["plate_lock"].(map[string]any)
	if gate["usable"] != false || gate["reason"] != "sample_set_invalid" || gate["samples_loaded"] != false {
		t.Fatalf("a rejected sample set must show up in readyz without failing it: %v", gate)
	}
	plain := newFixture(t, nil)
	_, ready := plain.do(t, "GET", "/readyz", "", nil)
	pg, _ := ready["gates"].(map[string]any)["plate_lock"].(map[string]any)
	if pg["reason"] != "plate_lock_disabled" || pg["usable"] != false {
		t.Fatalf("without a source runtime the gate reads %v", pg)
	}
}

// Rollback is "turn the flag off", never "downgrade the binary". Off closes new
// work only; history, display, export, selection and deletion keep working.
func TestPlateFlagOffClosesNewWorkButKeepsHistoryAndRecovery(t *testing.T) {
	f := newPlateHTTPFixture(t, "carton-1024x1024")
	code, view := f.create(t, "plate-flag")
	if code != 200 {
		t.Fatal(code, view)
	}
	runID, _ := view["run_id"].(string)
	quote, _ := view["quote"].(map[string]any)
	confirm, _ := json.Marshal(map[string]string{"quote_id": quote["quote_id"].(string), "quote_fingerprint": quote["quote_fingerprint"].(string)})
	f.srv.Source.PlateEnabled = false
	if w := f.raw(t, "POST", f.path("/"+runID+"/confirm"), f.token, string(confirm)); w.Code != 503 {
		t.Fatalf("an unconfirmed quote must not start while the flag is off: %d %s", w.Code, w.Body.String())
	}
	if f.ports.mutations()[2] != 0 {
		t.Fatal("nothing may be submitted")
	}
	f.srv.Source.PlateEnabled = true
	runID, _ = f.finish(t, "plate-flag", gradientPNG(1024, 1024))
	f.srv.Source.PlateEnabled = false
	for _, tc := range []struct{ method, suffix, body string }{{"GET", "", ""}, {"GET", "/content", ""}, {"GET", "/export", ""}, {"POST", "/select", `{}`}, {"POST", "/reconcile", `{}`}} {
		if w := f.raw(t, tc.method, f.path("/"+runID+tc.suffix), f.token, tc.body); w.Code != 200 {
			t.Fatalf("%s %s must keep working with the flag off: %d %s", tc.method, tc.suffix, w.Code, w.Body.String())
		}
	}
	if w := f.raw(t, "POST", f.path(""), f.token, f.createBody("plate-new", f.inputID, f.c.Intents[1])); w.Code != 503 || !strings.Contains(w.Body.String(), "plate_lock_disabled") {
		t.Fatalf("a new request must be closed: %d %s", w.Code, w.Body.String())
	}
	if w := f.raw(t, "DELETE", f.path("/"+runID+"/output"), f.token, `{}`); w.Code != 200 {
		t.Fatalf("deleting an existing result must keep working: %d %s", w.Code, w.Body.String())
	}
	if w := f.raw(t, "GET", f.path("/"+runID+"/content"), f.token, ""); w.Code == 200 {
		t.Fatal("a deleted result must no longer be displayed")
	}
}
````

既有测试夹具的小重构（`newSourceHTTPFixtureWith(t, mutate)`，原 `newSourceHTTPFixture(t)` 行为不变）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/httpapi/source_image_test.go
+++ b/server/internal/httpapi/source_image_test.go
@@ -134,8 +134,17 @@
 	token   string
 }
 
-func newSourceHTTPFixture(t *testing.T) *sourceHTTPFixture {
-	f := newFixture(t, func(c *config.Config) { c.TextToImageEnabled = true })
+func newSourceHTTPFixture(t *testing.T) *sourceHTTPFixture { return newSourceHTTPFixtureWith(t, nil) }
+
+// newSourceHTTPFixtureWith lets a test add config (for example an Upload stub)
+// on top of the shared source-image wiring.
+func newSourceHTTPFixtureWith(t *testing.T, mutate func(*config.Config)) *sourceHTTPFixture {
+	f := newFixture(t, func(c *config.Config) {
+		c.TextToImageEnabled = true
+		if mutate != nil {
+			mutate(c)
+		}
+	})
 	sourceDB := filepath.Join(t.TempDir(), "source-http.db")
 	if e := f.st.Close(); e != nil {
 		t.Fatal(e)
PATCH1
````

进程装配测试补丁（默认关、缺目录、样本集被拒、样本集通过；以及“未启动联合恢复运行时时永远不 ready”）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/cmd/server/source_runtime_test.go
+++ b/server/cmd/server/source_runtime_test.go
@@ -8,6 +8,7 @@
 	"net"
 	"net/http"
 	"net/http/httptest"
+	"os"
 	"path/filepath"
 	"strings"
 	"sync"
@@ -15,6 +16,7 @@
 	"time"
 
 	"github.com/bianjiefilm/product-image-engine/server/internal/config"
+	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
 	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
 	"github.com/bianjiefilm/product-image-engine/server/internal/store"
 	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
@@ -618,4 +620,40 @@
 	close(release)
 	sourceAwait(t, handlerDone, "fixture handler not released")
 	sourceAwait(t, requestDone, "timeout did not close HTTP connection")
+}
+
+func TestPlateLockAssemblyIsOffByDefaultAndFailsClosed(t *testing.T) {
+	good := sampletest.Dir(t)
+	broken := sampletest.Copy(t)
+	if e := os.Remove(filepath.Join(broken, "masks", "carton-1024x1024.png")); e != nil {
+		t.Fatal(e)
+	}
+	for _, tc := range []struct {
+		name        string
+		flag        bool
+		dir         string
+		wantEnabled bool
+		wantSet     bool
+		wantReason  string
+	}{
+		{"flag off ignores a valid directory", false, good, false, false, ""},
+		{"flag on without a directory", true, "", true, false, "sample_set_unconfigured"},
+		{"flag on with a broken set", true, broken, true, false, "sample_set_invalid"},
+		{"flag on with the frozen set", true, good, true, true, ""},
+	} {
+		t.Run(tc.name, func(t *testing.T) {
+			c := sourceRuntimeConfig()
+			c.BgPlateLockEnabled, c.FidelitySamplesDir = tc.flag, tc.dir
+			svc, e := newSourceRuntime(c, sourceRuntimeStore(t))
+			if e != nil || svc == nil {
+				t.Fatal(svc, e)
+			}
+			if svc.PlateEnabled != tc.wantEnabled || (svc.Samples != nil) != tc.wantSet || svc.SamplesReason != tc.wantReason {
+				t.Fatalf("enabled=%v set=%v reason=%q", svc.PlateEnabled, svc.Samples != nil, svc.SamplesReason)
+			}
+			if ready, _ := svc.PlateReady("personal"); ready {
+				t.Fatal("the joined recovery runtime is not started here, so the mode can never be ready")
+			}
+		})
+	}
 }
PATCH1
````

- [ ] **Step 2: 运行，确认失败**

```bash
cd "$WT/server" && go test -count=1 ./internal/store ./internal/httpapi ./cmd/server 2>&1 | tail -12
```

期望：三者 `FAIL [build failed]`（`undefined: OutputQuality`、`f.srv.ReadOriginal undefined`、`svc.PlateEnabled` 有但 `assemblePlateLock` 相关断言失败 …）。

- [ ] **Step 3: 写实现——迁移 0021 与 store**

`server/internal/store/migrations/0021_plate_output_quality.sql`

````sql
-- Additive. Binds a returned project output to the quality verdict of the plate
-- derivation it came from. Only a limited_candidate can ever be bound.
CREATE TABLE project_output_quality (
 output_id TEXT PRIMARY KEY REFERENCES project_outputs(id) ON DELETE RESTRICT,
 tenant_scope TEXT NOT NULL,
 project_id TEXT NOT NULL,
 run_id TEXT NOT NULL UNIQUE REFERENCES product_plate_derivations(run_id) ON DELETE RESTRICT,
 result_sha256 TEXT NOT NULL CHECK(length(result_sha256)=64),
 candidate_state TEXT NOT NULL CHECK(candidate_state='limited_candidate'),
 original_charge_id TEXT NOT NULL CHECK(original_charge_id!=''),
 scope_json TEXT NOT NULL CHECK(json_valid(scope_json)),
 limits_json TEXT NOT NULL CHECK(json_valid(limits_json)),
 report_sha256 TEXT NOT NULL CHECK(length(report_sha256)=64),
 created_at INTEGER NOT NULL
);
CREATE INDEX project_output_quality_project ON project_output_quality(tenant_scope,project_id);
CREATE TRIGGER project_output_quality_immutable BEFORE UPDATE ON project_output_quality
BEGIN SELECT RAISE(ABORT,'output quality binding is immutable'); END;
CREATE TRIGGER project_output_quality_keep BEFORE DELETE ON project_output_quality
BEGIN SELECT RAISE(ABORT,'output quality binding cannot be deleted'); END;
````

`server/internal/store/plate_output.go`

````go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// OutputQuality ties a returned project output to its plate derivation.
type OutputQuality struct {
	OutputID, TenantScope, ProjectID, RunID, ResultSHA256 string
	CandidateState, OriginalChargeID                      string
	ScopeJSON, LimitsJSON, ReportSHA256                   string
	CreatedAt                                             int64
}

const outputQualityColumns = `output_id,tenant_scope,project_id,run_id,result_sha256,candidate_state,original_charge_id,scope_json,limits_json,report_sha256,created_at`

func scanOutputQuality(row interface{ Scan(...any) error }) (OutputQuality, error) {
	var q OutputQuality
	e := row.Scan(&q.OutputID, &q.TenantScope, &q.ProjectID, &q.RunID, &q.ResultSHA256, &q.CandidateState, &q.OriginalChargeID, &q.ScopeJSON, &q.LimitsJSON, &q.ReportSHA256, &q.CreatedAt)
	return q, e
}

// SaveOutputQuality is idempotent: a second save for the same output returns
// the stored binding and never replaces it. It refuses anything but a binding
// that exactly matches the output row and its derivation.
func (s *Store) SaveOutputQuality(ctx context.Context, q OutputQuality) (OutputQuality, error) {
	if q.CandidateState != si.CandidateLimited || q.OutputID == "" || q.RunID == "" || q.OriginalChargeID == "" {
		return OutputQuality{}, si.ErrNotUsable
	}
	err := s.withSourceWrite(ctx, func(c *sql.Conn) error {
		var sha, tenant, project string
		e := c.QueryRowContext(ctx, `SELECT result_sha256,tenant_scope,project_id FROM project_outputs WHERE id=?`, q.OutputID).Scan(&sha, &tenant, &project)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		var dsha, dstate, dreport string
		e = c.QueryRowContext(ctx, `SELECT derived_sha256,candidate_state,report_sha256 FROM product_plate_derivations WHERE run_id=?`, q.RunID).Scan(&dsha, &dstate, &dreport)
		if errors.Is(e, sql.ErrNoRows) {
			return si.ErrInvariant
		}
		if e != nil {
			return e
		}
		if sha != q.ResultSHA256 || dsha != q.ResultSHA256 || dstate != si.CandidateLimited || dreport != q.ReportSHA256 || tenant != q.TenantScope || project != q.ProjectID {
			return si.ErrInvariant
		}
		_, e = c.ExecContext(ctx, `INSERT OR IGNORE INTO project_output_quality(`+outputQualityColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			q.OutputID, q.TenantScope, q.ProjectID, q.RunID, q.ResultSHA256, q.CandidateState, q.OriginalChargeID, q.ScopeJSON, q.LimitsJSON, q.ReportSHA256, Now().Unix())
		return e
	})
	if err != nil {
		return OutputQuality{}, err
	}
	got, ok, e := s.GetOutputQuality(ctx, q.TenantScope, q.OutputID)
	if e != nil || !ok {
		return OutputQuality{}, fmt.Errorf("store: output quality not readable after save: %w", si.ErrInvariant)
	}
	return got, nil
}

func (s *Store) GetOutputQuality(ctx context.Context, tenant, outputID string) (OutputQuality, bool, error) {
	q, e := scanOutputQuality(s.db.QueryRowContext(ctx, `SELECT `+outputQualityColumns+` FROM project_output_quality WHERE tenant_scope=? AND output_id=?`, tenant, outputID))
	if errors.Is(e, sql.ErrNoRows) {
		return OutputQuality{}, false, nil
	}
	return q, e == nil, e
}

func (s *Store) ListOutputQualities(ctx context.Context, tenant, project string) ([]OutputQuality, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+outputQualityColumns+` FROM project_output_quality WHERE tenant_scope=? AND project_id=? ORDER BY created_at DESC,output_id`, tenant, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []OutputQuality{}
	for rows.Next() {
		q, e := scanOutputQuality(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// PlateDerivationOwnsSHA reports the run whose derived composite has this
// digest in this project. Such bytes may only enter project outputs through
// the server-side return action.
func (s *Store) PlateDerivationOwnsSHA(ctx context.Context, tenant, project, sha string) (string, bool, error) {
	var run string
	e := s.db.QueryRowContext(ctx, `SELECT d.run_id FROM product_plate_derivations d JOIN product_source_runs r ON r.id=d.run_id WHERE r.tenant_id=? AND r.project_id=? AND d.derived_sha256=? AND d.derived_sha256!=''`, tenant, project, sha).Scan(&run)
	if errors.Is(e, sql.ErrNoRows) {
		return "", false, nil
	}
	return run, e == nil, e
}
````

- [ ] **Step 4: 写实现——HTTP 层**

新文件（能力项、创建分支、`frozenCaseFor` 只用本地登记、`plateView`、内容 / 导出 / 回传、`qualityBindingView`）：

`server/internal/httpapi/source_plate.go`

````go
package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sourceflow"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
)

// Stable HTTP reason codes for the limited-fidelity mode. They are API.
const plateScope = fidelitysamples.Scope

// frozenCaseFor resolves a project input to a frozen sample from LOCAL facts
// only (the product's own photo registry). It never calls a remote service,
// so capabilities stay a pure read and a miss costs nothing.
func (s *Server) frozenCaseFor(ctx context.Context, in store.ProjectInput, principalTenant string) (*fidelitysamples.LoadedCase, bool) {
	if s.Source == nil || s.Source.Samples == nil || in.PlatformAssetID == "" {
		return nil, false
	}
	seen := map[string]bool{}
	for _, tenant := range []string{in.TenantID, principalTenant} {
		if tenant == "" || seen[tenant] {
			continue
		}
		seen[tenant] = true
		if pa, err := s.St.GetPhotoAssetByAssetID(ctx, tenant, in.PlatformAssetID); err == nil {
			if c, ok := s.Source.Samples.BySHA(pa.SHA256); ok {
				return c, true
			}
		}
	}
	return nil, false
}

func canvasView(c fidelitysamples.Canvas) map[string]any {
	return map[string]any{"width_px": c.W, "height_px": c.H}
}

// plateCapability is the capabilities entry for background_plate_lock. It
// reports readiness and which of THIS project's inputs are frozen samples.
func (s *Server) plateCapability(ctx context.Context, auth sourceflow.Authorization, project store.Project) map[string]any {
	entry := map[string]any{"mode": si.ModePlateLock, "size": "1024*1024", "model": "qwen-image-2.0", "claim": plateScope, "fidelity": "frozen_sample_only", "visual_quality": "unknown", "supported_input_ids": []string{}, "supported_inputs": []any{}}
	ready, reason := s.Source.PlateReady(auth.PayerSource)
	switch {
	case project.Status == "archived":
		ready, reason = false, "project_archived"
	case !auth.CanWrite:
		ready, reason = false, "project_write_forbidden"
	case !ready && reason == "source_unconfigured" && !s.Source.OutputRecoveryReady:
		reason = s.sourceRuntimeReason()
	}
	entry["ready"], entry["disabled"], entry["reason"] = ready, !ready, reason
	entry["can_quote"], entry["can_confirm"] = ready, ready
	if ready {
		ids, views := []string{}, []any{}
		inputs, err := s.St.ListInputs(ctx, auth.Scope.TenantID, auth.Scope.ProjectID)
		if err == nil {
			p, _ := principalFrom(ctx)
			for _, in := range inputs {
				if c, ok := s.frozenCaseFor(ctx, in, p.Tenant()); ok {
					ids = append(ids, in.ID)
					views = append(views, map[string]any{"input_id": in.ID, "kind": c.Kind, "canvas": canvasView(c.Canvas)})
				}
			}
		}
		entry["supported_input_ids"], entry["supported_inputs"] = ids, views
	}
	return entry
}

// readOriginal reads the bytes behind a project input from Upload. Tests
// replace the seam; production uses the same verified read as background jobs.
func (s *Server) readOriginal(ctx context.Context, p platform.Principal, tenant, project, input string) ([]byte, error) {
	if s.ReadOriginal != nil {
		return s.ReadOriginal(ctx, p, tenant, project, input)
	}
	return s.readInputOriginal(ctx, p, tenant, project, input)
}

func (s *Server) handleSourcePlateCreate(w http.ResponseWriter, r *http.Request, body map[string]string) {
	actor, _ := sourceActor(r)
	p, _ := principalFrom(r.Context())
	project := r.PathValue("id")
	auth, e := s.Source.Auth.ForProject(r.Context(), actor, project, true)
	if e != nil {
		if errors.Is(e, si.ErrNotFound) {
			if current, re := s.Source.Auth.ForProject(r.Context(), actor, project, false); re == nil && !current.CanWrite {
				e = si.ErrForbidden
			}
		}
		s.sourceResult(w, r, si.Run{}, e)
		return
	}
	if ready, reason := s.Source.PlateReady(auth.PayerSource); !ready {
		writeErr(w, 503, reason, "限定保真换背景当前不可用")
		return
	}
	inputID := body["input_id"]
	if !sourceSegment(inputID) || !si.ValidIntentText(body["background_intent"]) {
		writeErr(w, 400, "invalid_request", "素材编号或背景描述不合法")
		return
	}
	in, err := s.St.GetInput(r.Context(), auth.Scope.TenantID, project, inputID)
	if err != nil {
		s.sourceResult(w, r, si.Run{}, si.ErrNotFound)
		return
	}
	c, ok := s.frozenCaseFor(r.Context(), in, p.Tenant())
	if !ok {
		s.sourceResult(w, r, si.Run{}, si.ErrNotFrozen)
		return
	}
	// The registry says it is a frozen sample; prove it against the real bytes
	// before anything is priced.
	original, err := s.readOriginal(r.Context(), p, auth.Scope.TenantID, project, inputID)
	if err != nil {
		writeErr(w, 409, "original_missing", "原图缺失，无法核对")
		return
	}
	if si.SHA256Hex(original) != c.Original.SHA256 {
		s.sourceResult(w, r, si.Run{}, si.ErrNotFrozen)
		return
	}
	run, e := s.Source.CreatePlate(r.Context(), actor, project, sourceflow.PlateRequest{
		RequestKey: body["request_key"], InputID: inputID, BackgroundIntent: body["background_intent"], OriginalSHA256: c.Original.SHA256,
	})
	s.sourceResult(w, r, run, e)
}

// plateView is the public projection of a plate-lock run's derived result.
// Nothing here is a score; states are the four-valued axes of the frozen report.
func (s *Server) plateView(ctx context.Context, run si.Run) map[string]any {
	out := map[string]any{"result_kind": "derived_composite", "claim": plateScope, "human_usefulness": "NOT_RUN", "derivation_state": "pending", "candidate_state": si.CandidatePending}
	if s.Source == nil || s.Source.Store == nil {
		return out
	}
	frozen, err := s.Source.Store.GetPlateInput(ctx, run)
	if err != nil {
		out["derivation_state"], out["candidate_state"] = "invalid", si.CandidateBlocked
		return out
	}
	out["sample_set_id"], out["case_id"], out["background_intent"] = frozen.SetID, frozen.CaseID, frozen.BackgroundIntent
	out["canvas"] = map[string]any{"width_px": frozen.CanvasW, "height_px": frozen.CanvasH}
	if run.Output != nil && !run.Deleted {
		out["plate_task"] = map[string]any{"task_id": run.TaskID, "asset_id": run.Output.AssetID, "reference_id": run.Output.ReferenceID, "sha256": run.Output.SHA256}
	}
	d, found, err := s.Source.Store.GetPlateDerivation(ctx, run.ID)
	if err != nil {
		out["derivation_state"], out["candidate_state"] = "invalid", si.CandidateBlocked
		return out
	}
	if !found {
		return out
	}
	out["derivation_state"], out["candidate_state"] = d.State, d.CandidateState
	if d.State == si.DerivationBlocked {
		out["blocked_reason"] = d.BlockedReason
		return out
	}
	rep, err := platelock.DecodeReport([]byte(d.ReportJSON))
	if err != nil {
		out["derivation_state"], out["candidate_state"] = "invalid", si.CandidateBlocked
		return out
	}
	axes := []any{}
	for _, a := range rep.Axes {
		axes = append(axes, map[string]any{"name": a.Name, "state": a.State, "machine_checked": a.MachineChecked, "evidence": a.Evidence})
	}
	out["verdicts"] = map[string]any{"fidelity": rep.Verdicts.Fidelity, "generation": rep.Verdicts.Generation, "billing": rep.Verdicts.Billing, "human_usefulness": rep.Verdicts.HumanUsefulness}
	out["axes"], out["limits"] = axes, rep.Limits
	out["report_sha256"], out["composite_sha256"] = d.ReportSHA256, d.SHA256
	if len(d.PNG) > 0 && !run.Deleted {
		out["result"] = map[string]any{"content_type": "image/png", "size_bytes": strconv.FormatInt(d.SizeBytes, 10), "width_px": d.Width, "height_px": d.Height, "sha256": d.SHA256}
	}
	return out
}

func qualityHeader(candidate string) string {
	switch candidate {
	case si.CandidateLimited:
		return "limited"
	case si.CandidateNotUsable:
		return "fail"
	}
	return "pending"
}

// handlePlateContent streams the derived composite from the product's own
// store (digest re-checked on read). A failed candidate may be looked at, with
// a verdict header, so the user can see why it is unusable; it is never
// selectable or exportable.
func (s *Server) handlePlateContent(w http.ResponseWriter, r *http.Request) {
	actor, _ := sourceActor(r)
	pc, e := s.Source.LoadPlate(r.Context(), actor, r.PathValue("id"), r.PathValue("runId"))
	if e != nil {
		s.sourceResult(w, r, pc.Run, e)
		return
	}
	if pc.Derivation.State != si.DerivationDerived || len(pc.Derivation.PNG) == 0 {
		writeErr(w, 409, "no_composite", "没有可显示的合成结果")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(pc.Derivation.PNG)))
	w.Header().Set("Content-Disposition", `inline; filename="background-replaced.png"`)
	w.Header().Set("X-Quality-Verdict", qualityHeader(pc.Derivation.CandidateState))
	_, _ = w.Write(pc.Derivation.PNG)
}

// plateReport returns the frozen report only if it is canonical and bound to
// the stored composite (the "recompute and compare" gate).
func plateReport(d si.PlateDerivation) (platelock.Report, error) {
	rep, err := platelock.DecodeReport([]byte(d.ReportJSON))
	if err != nil || rep.Facts.CompositeSHA256 != d.SHA256 || rep.CandidateState != d.CandidateState || si.SHA256Hex([]byte(d.ReportJSON)) != d.ReportSHA256 {
		return platelock.Report{}, si.ErrInvariant
	}
	return rep, nil
}

func (s *Server) handlePlateExport(w http.ResponseWriter, r *http.Request) {
	actor, _ := sourceActor(r)
	pc, e := s.Source.LoadPlate(r.Context(), actor, r.PathValue("id"), r.PathValue("runId"))
	if e != nil {
		s.sourceResult(w, r, pc.Run, e)
		return
	}
	if pc.Derivation.CandidateState != si.CandidateLimited {
		s.sourceResult(w, r, pc.Run, si.ErrNotUsable)
		return
	}
	if _, e = plateReport(pc.Derivation); e != nil {
		s.sourceResult(w, r, pc.Run, e)
		return
	}
	snapshot, _ := json.Marshal(map[string]any{"report_version": "product-source-export/v2", "run": s.sourceRuntimeView(r.Context(), pc.Run), "this_download_billing_check": "charged", "result_sha256": pc.Derivation.SHA256, "human_usefulness": "NOT_RUN"})
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, entry := range []struct {
		name string
		raw  []byte
	}{{"image.png", pc.Derivation.PNG}, {"quality.json", []byte(pc.Derivation.ReportJSON)}, {"snapshot.json", snapshot}} {
		f, err := archive.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Store})
		if err != nil {
			writeErr(w, 500, "internal", "导出失败")
			return
		}
		if _, err = f.Write(entry.raw); err != nil {
			writeErr(w, 500, "internal", "导出失败")
			return
		}
	}
	if archive.Close() != nil {
		writeErr(w, 500, "internal", "导出失败")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="background-replaced.zip"`)
	_, _ = w.Write(buf.Bytes())
}

// handleSourceReturn registers the selected, limited-candidate composite as a
// project output so it can be returned to an order or campaign. It is the only
// door for derived bytes into project outputs; it never generates or charges.
func (s *Server) handleSourceReturn(w http.ResponseWriter, r *http.Request) {
	if !sourceMethod(w, r, http.MethodPost) {
		return
	}
	if _, ok := sourceBody(w, r); !ok || !s.sourceService(w, r) {
		return
	}
	run, ok := s.sourceLoad(w, r, true)
	if !ok {
		return
	}
	actor, _ := sourceActor(r)
	p, _ := principalFrom(r.Context())
	pc, e := s.Source.LoadPlate(r.Context(), actor, r.PathValue("id"), run.ID)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	if pc.Derivation.CandidateState != si.CandidateLimited {
		s.sourceResult(w, r, run, si.ErrNotUsable)
		return
	}
	if !pc.Run.Selected {
		writeErr(w, 409, "not_selected", "请先选定这张结果")
		return
	}
	rep, e := plateReport(pc.Derivation)
	if e != nil {
		s.sourceResult(w, r, run, e)
		return
	}
	projID := r.PathValue("id")
	if _, err := s.St.GetProject(r.Context(), p.Tenant(), projID); err != nil {
		writeStoreErr(w, err)
		return
	}
	if _, err := s.St.GetBindingByProject(r.Context(), p.Tenant(), projID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusConflict, "no_source_binding", "该工程无来源绑定(standalone 完整保留在本应用,不回传)")
		return
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}
	out, dup, aerr := s.registerOutputBytesFor(r.Context(), p.Tenant(), projID, "background-replaced.png", "image/png", pc.Derivation.PNG, s.bindingOutputContext, run.ID)
	if aerr != nil {
		aerr.write(w)
		return
	}
	scope, _ := json.Marshal(rep.Scope)
	limits, _ := json.Marshal(rep.Limits)
	q, err := s.St.SaveOutputQuality(r.Context(), store.OutputQuality{
		OutputID: out.ID, TenantScope: p.Tenant(), ProjectID: projID, RunID: run.ID, ResultSHA256: out.ResultSHA256,
		CandidateState: pc.Derivation.CandidateState, OriginalChargeID: rep.Facts.OriginalChargeID,
		ScopeJSON: string(scope), LimitsJSON: string(limits), ReportSHA256: pc.Derivation.ReportSHA256,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"output": out, "quality": qualityBindingView(q), "duplicate": dup})
}

func qualityBindingView(q store.OutputQuality) map[string]any {
	var scope map[string]any
	var limits []string
	_ = json.Unmarshal([]byte(q.ScopeJSON), &scope)
	_ = json.Unmarshal([]byte(q.LimitsJSON), &limits)
	return map[string]any{"output_id": q.OutputID, "run_id": q.RunID, "candidate_state": q.CandidateState, "scope": scope, "limits": limits, "report_sha256": q.ReportSHA256, "result_sha256": q.ResultSHA256, "receipt_includes_limits": false}
}
````

既有文件补丁：`sourceBody` 拆成 `sourceBodyFor`（让封闭键集可依赖 `mode`，仍拒绝所有未列键 / 重复键 / 非字符串）、路由分发 `return`、`ErrNotFrozen → 422`、`ErrNotUsable → 409`、`sourceRuntimeView` 带 ctx 并附 `plate`、能力项换成真实实现；`Server.ReadOriginal` 接缝、`return` 路由、`/readyz` 构建身份与 `plate_lock`；内容 / 导出对 plate 分流；plate 视图隐藏原始底板；`registerOutputBytesFor`（派生合成字节只许经 `return` 登记）、回执对质量绑定门控、`quality_bindings`：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/internal/httpapi/server.go
+++ b/server/internal/httpapi/server.go
@@ -13,8 +13,8 @@
 	"sync"
 
 	"github.com/bianjiefilm/product-image-engine/server/internal/appregistry"
-	"github.com/bianjiefilm/product-image-engine/server/internal/bgreplace"
 	"github.com/bianjiefilm/product-image-engine/server/internal/billassemble"
+	"github.com/bianjiefilm/product-image-engine/server/internal/buildinfo"
 	"github.com/bianjiefilm/product-image-engine/server/internal/config"
 	"github.com/bianjiefilm/product-image-engine/server/internal/imagetmpl"
 	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
@@ -51,9 +51,9 @@
 	// ImageHTTP 只发给已配置的图像端点。nil 时由调用方自建超时客户端。
 	// 测试用它替换传输，避免真的拨号。
 	ImageHTTP *http.Client
-	// PlateCoverage 是服务端冻结的主体覆盖样本，不从浏览器读取。
-	// 为空表示没有已授权样本，模型背景不会出图。
-	PlateCoverage *bgreplace.CoverageSample
+	// ReadOriginal replaces the Upload read of a project input's bytes. Tests set
+	// it; production leaves it nil and uses the verified Upload read.
+	ReadOriginal func(ctx context.Context, p platform.Principal, tenant, project, input string) ([]byte, error)
 
 	// batchSubmitSem 批量提交进程内信号量(HUI-1704 拍板:并发上限 4,超出排队)。
 	// 惰性初始化;semMu 仅保护初始化。
@@ -104,7 +104,7 @@
 	mux.Handle("POST /api/v1/projects/{id}/first-image/refresh", s.guard(true, s.handleRefreshFirstImage))
 
 	// Source ownership is independent of legacy creation flags.
-	for _, path := range []string{"source-image-capabilities", "source-image-runs", "source-image-runs/by-request", "source-image-runs/{runId}", "source-image-runs/{runId}/confirm", "source-image-runs/{runId}/reconcile", "source-image-runs/{runId}/cancel", "source-image-runs/{runId}/select", "source-image-runs/{runId}/output", "source-image-runs/{runId}/content", "source-image-runs/{runId}/export"} {
+	for _, path := range []string{"source-image-capabilities", "source-image-runs", "source-image-runs/by-request", "source-image-runs/{runId}", "source-image-runs/{runId}/confirm", "source-image-runs/{runId}/reconcile", "source-image-runs/{runId}/cancel", "source-image-runs/{runId}/select", "source-image-runs/{runId}/output", "source-image-runs/{runId}/content", "source-image-runs/{runId}/export", "source-image-runs/{runId}/return"} {
 		mux.Handle("/api/v1/projects/{id}/"+path, s.guard(true, s.handleSourceResource))
 	}
 
@@ -335,10 +335,21 @@
 	if s.TplBuiltins != nil {
 		tplBuiltins = len(s.TplBuiltins.List())
 	}
+	bi := buildinfo.Read()
+	plateReady, plateReason := false, "plate_lock_disabled"
+	if s.Source != nil {
+		plateReady, plateReason = s.Source.PlateReady("personal")
+	}
 	body := map[string]any{
 		"ok":    len(fatal) == 0,
 		"fatal": fatal,
+		// The code identity behind this response, recorded in acceptance evidence.
+		"build": map[string]any{"vcs_revision": bi.VCSRevision, "vcs_modified": bi.VCSModified},
 		"gates": map[string]any{
+			"plate_lock": map[string]any{
+				"enabled": s.Cfg.BgPlateLockEnabled, "usable": plateReady, "reason": plateReason,
+				"samples_loaded": s.Source != nil && s.Source.Samples != nil,
+			},
 			"identity":   map[string]any{"configured": true, "base_url_configured": s.Cfg.IdentityBaseURL != ""},
 			"generation": map[string]any{"enabled": s.Cfg.GenerationEnabled, "usable": genSt == 0, "reason": genMsg},
 			"billing":    map[string]any{"enabled": s.Cfg.BillingEnabled, "usable": billSt == 0, "reason": billMsg},
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/server/internal/httpapi/source_image.go
+++ b/server/internal/httpapi/source_image.go
@@ -119,6 +119,12 @@
 // The HTTP contract is a recursively closed object: this segment accepts only
 // exact scalar strings (or an empty action object), never arbitrary params.
 func sourceBody(w http.ResponseWriter, r *http.Request, keys ...string) (map[string]string, bool) {
+	return sourceBodyFor(w, r, func(map[string]json.RawMessage) []string { return keys })
+}
+
+// sourceBodyFor lets the closed key set depend on the body's own "mode" while
+// still rejecting every unlisted key, duplicate key and non-string value.
+func sourceBodyFor(w http.ResponseWriter, r *http.Request, pick func(map[string]json.RawMessage) []string) (map[string]string, bool) {
 	if _, e := sourceQuery(r); e != nil {
 		writeErr(w, 400, "invalid_request", "查询参数不支持")
 		return nil, false
@@ -128,16 +134,24 @@
 		writeErr(w, 400, "invalid_request", "请求体超过限制或读取失败")
 		return nil, false
 	}
-	if len(raw) == 0 && len(keys) == 0 {
-		return map[string]string{}, true
-	}
-	typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
-	if e != nil || typ != "application/json" {
-		writeErr(w, 400, "invalid_request", "请求体必须是 JSON")
-		return nil, false
-	}
 	var obj map[string]json.RawMessage
-	if platform.DecodeSourceJSON(raw, &obj) != nil || obj == nil || len(obj) != len(keys) {
+	if len(raw) == 0 {
+		if keys := pick(nil); len(keys) == 0 {
+			return map[string]string{}, true
+		}
+	} else {
+		typ, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
+		if e != nil || typ != "application/json" {
+			writeErr(w, 400, "invalid_request", "请求体必须是 JSON")
+			return nil, false
+		}
+		if platform.DecodeSourceJSON(raw, &obj) != nil || obj == nil {
+			writeErr(w, 400, "invalid_request", "请求体字段不合法")
+			return nil, false
+		}
+	}
+	keys := pick(obj)
+	if len(obj) != len(keys) {
 		writeErr(w, 400, "invalid_request", "请求体字段不合法")
 		return nil, false
 	}
@@ -189,13 +203,15 @@
 		s.handleSourceContent(w, r)
 	case "export":
 		s.handleSourceExport(w, r)
+	case "return":
+		s.handleSourceReturn(w, r)
 	default:
 		s.handleSourceGet(w, r)
 	}
 }
 func (s *Server) sourceResult(w http.ResponseWriter, r *http.Request, run si.Run, e error) {
 	if e == nil {
-		writeJSON(w, 200, s.sourceRuntimeView(run))
+		writeJSON(w, 200, s.sourceRuntimeView(r.Context(), run))
 		return
 	}
 	status, code, msg := 503, "source_unavailable", "正式服务暂时不可用，原请求状态待核实"
@@ -206,6 +222,10 @@
 		status, code, msg = 403, "forbidden", "工程当前不允许此操作"
 	case errors.Is(e, si.ErrInvalid):
 		status, code, msg = 400, "invalid_request", "请求不合法"
+	case errors.Is(e, si.ErrNotFrozen):
+		status, code, msg = 422, "sample_not_frozen", "这张照片不在已验证范围内，不能使用限定保真换背景"
+	case errors.Is(e, si.ErrNotUsable):
+		status, code, msg = 409, unusableCandidateCode, unusableCandidateMsg
 	case errors.Is(e, si.ErrConflict):
 		status, code, msg = 409, "conflict", "原请求事实与本次操作冲突"
 	case errors.Is(e, si.ErrUnconfigured):
@@ -273,16 +293,25 @@
 	} else if !ready {
 		reason = runtimeReason
 	}
-	writeJSON(w, 200, map[string]any{"modes": []any{map[string]any{"mode": "text_generate", "can_quote": canQuote, "can_confirm": canConfirm, "authorization_reason": authorizationReason, "runtime_reason": runtimeReason, "ready": ready, "disabled": !ready, "reason": reason, "size": "1024*1024", "model": "qwen-image-2.0", "fidelity": "not_applicable", "visual_quality": "unknown"}, map[string]any{"mode": "background_plate_lock", "ready": false, "disabled": true, "reason": "formal_contract_unconfigured"}, map[string]any{"mode": "reference_edit", "ready": false, "disabled": true, "reason": "formal_contract_unconfigured"}}, "historical_read": true, "automatic_output_recovery_ready": s.Source.OutputRecoveryReady})
+	writeJSON(w, 200, map[string]any{"modes": []any{map[string]any{"mode": "text_generate", "can_quote": canQuote, "can_confirm": canConfirm, "authorization_reason": authorizationReason, "runtime_reason": runtimeReason, "ready": ready, "disabled": !ready, "reason": reason, "size": "1024*1024", "model": "qwen-image-2.0", "fidelity": "not_applicable", "visual_quality": "unknown"}, s.plateCapability(r.Context(), auth, project), map[string]any{"mode": "reference_edit", "ready": false, "disabled": true, "reason": "formal_contract_unconfigured"}}, "historical_read": true, "automatic_output_recovery_ready": s.Source.OutputRecoveryReady})
 }
 func (s *Server) handleSourceCreate(w http.ResponseWriter, r *http.Request) {
 	if !sourceMethod(w, r, http.MethodPost) {
 		return
 	}
-	body, ok := sourceBody(w, r, "request_key", "mode", "prompt", "size")
+	body, ok := sourceBodyFor(w, r, func(obj map[string]json.RawMessage) []string {
+		if string(obj["mode"]) == `"`+si.ModePlateLock+`"` {
+			return []string{"request_key", "mode", "input_id", "background_intent"}
+		}
+		return []string{"request_key", "mode", "prompt", "size"}
+	})
 	if !ok || !s.sourceService(w, r) {
 		return
 	}
+	if body["mode"] == si.ModePlateLock {
+		s.handleSourcePlateCreate(w, r, body)
+		return
+	}
 	actor, _ := sourceActor(r)
 	run, e := s.Source.Create(r.Context(), actor, r.PathValue("id"), sourceflow.NewRunRequest{RequestKey: body["request_key"], Mode: body["mode"], Prompt: body["prompt"], Size: body["size"]})
 	if errors.Is(e, si.ErrNotFound) {
@@ -364,7 +393,7 @@
 	}
 	views := []any{}
 	for _, run := range runs {
-		views = append(views, s.sourceRuntimeView(run))
+		views = append(views, s.sourceRuntimeView(r.Context(), run))
 	}
 	cursor := ""
 	if nextID != "" {
@@ -478,9 +507,12 @@
 }
 
 // Readiness is transport metadata, never a Run observation or stored fact.
-func (s *Server) sourceRuntimeView(run si.Run) map[string]any {
+func (s *Server) sourceRuntimeView(ctx context.Context, run si.Run) map[string]any {
 	view := sourceView(run)
 	view["automatic_output_recovery_ready"] = s.Source != nil && s.Source.OutputRecoveryReady
 	view["automatic_output_recovery_reason"] = s.sourceRuntimeReason()
+	if run.Intent.Mode == si.ModePlateLock {
+		view["plate"] = s.plateView(ctx, run)
+	}
 	return view
 }
PATCH2
git apply --whitespace=nowarn <<'PATCH3'
--- a/server/internal/httpapi/source_image_content.go
+++ b/server/internal/httpapi/source_image_content.go
@@ -46,6 +46,10 @@
 	if !ok {
 		return
 	}
+	if run.Intent.Mode == si.ModePlateLock {
+		s.handlePlateContent(w, r)
+		return
+	}
 	actor, _ := sourceActor(r)
 	stream, e := s.Source.OpenOutput(r.Context(), actor, r.PathValue("id"), run.ID)
 	if e != nil {
@@ -68,6 +72,10 @@
 	if !ok {
 		return
 	}
+	if run.Intent.Mode == si.ModePlateLock {
+		s.handlePlateExport(w, r)
+		return
+	}
 	if run.Deleted || run.Output == nil {
 		s.sourceResult(w, r, run, si.ErrConflict)
 		return
@@ -84,7 +92,7 @@
 		s.sourceResult(w, r, run, e)
 		return
 	}
-	snapshot, _ := json.Marshal(map[string]any{"report_version": "product-source-export/v1", "run": s.sourceRuntimeView(run), "this_download_content_verification": "verified", "this_download_billing_check": "charged", "frozen_report_content_verification": "NOT_RUN", "visual_quality": "unknown", "human_adoption": "NOT_RUN"})
+	snapshot, _ := json.Marshal(map[string]any{"report_version": "product-source-export/v1", "run": s.sourceRuntimeView(r.Context(), run), "this_download_content_verification": "verified", "this_download_billing_check": "charged", "frozen_report_content_verification": "NOT_RUN", "visual_quality": "unknown", "human_adoption": "NOT_RUN"})
 	w.Header().Set("Content-Type", "application/zip")
 	w.Header().Set("Content-Disposition", `attachment; filename="source-image.zip"`)
 	archive := zip.NewWriter(w)
PATCH3
git apply --whitespace=nowarn <<'PATCH4'
--- a/server/internal/httpapi/source_image_view.go
+++ b/server/internal/httpapi/source_image_view.go
@@ -48,6 +48,12 @@
 	if r.Output != nil && !r.Deleted {
 		view["output"] = map[string]any{"asset_id": r.Output.AssetID, "reference_id": r.Output.ReferenceID, "sha256": r.Output.SHA256, "content_type": r.Output.ContentType, "size_bytes": strconv.FormatInt(r.Output.SizeBytes, 10), "width_px": 1024, "height_px": 1024, "association_verified": true}
 	}
+	if r.Intent.Mode == si.ModePlateLock {
+		// The Upload asset is only the unverified model plate. The result is the
+		// derived composite under "plate"; never advertise the raw asset.
+		view["output"] = nil
+		view["fidelity"] = "frozen_sample_only"
+	}
 	return view
 }
 
PATCH4
git apply --whitespace=nowarn <<'PATCH5'
--- a/server/internal/httpapi/handoff.go
+++ b/server/internal/httpapi/handoff.go
@@ -30,6 +30,7 @@
 	"github.com/bianjiefilm/product-image-engine/server/internal/handoff"
 	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
 	"github.com/bianjiefilm/product-image-engine/server/internal/receiptdoc"
+	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
 	"github.com/bianjiefilm/product-image-engine/server/internal/store"
 )
 
@@ -329,7 +330,16 @@
 	if list == nil {
 		list = []store.ProjectOutput{}
 	}
-	writeJSON(w, http.StatusOK, map[string]any{"outputs": list})
+	bindings, err := s.St.ListOutputQualities(r.Context(), proj.TenantID, projID)
+	if err != nil {
+		writeErr(w, http.StatusInternalServerError, "internal", "读取成果质量绑定失败")
+		return
+	}
+	views := []map[string]any{}
+	for _, q := range bindings {
+		views = append(views, qualityBindingView(q))
+	}
+	writeJSON(w, http.StatusOK, map[string]any{"outputs": list, "quality_bindings": views})
 }
 
 // handleBindingReturnTarget 解析"返回来源"跳转地址(登记表白名单内的 launch target)。
@@ -360,7 +370,13 @@
 			"来源应用未登记可返回的跳转目标("+err.Error()+");来源不可用不影响本工程继续编辑")
 		return
 	}
-	writeJSON(w, http.StatusOK, map[string]any{"url": url})
+	views := []map[string]any{}
+	if bound, err := s.St.ListOutputQualities(r.Context(), p.Tenant(), binding.ProjectID); err == nil {
+		for _, q := range bound {
+			views = append(views, qualityBindingView(q))
+		}
+	}
+	writeJSON(w, http.StatusOK, map[string]any{"url": url, "quality_bindings": views})
 }
 
 func (s *Server) handleGetBinding(w http.ResponseWriter, r *http.Request) {
@@ -540,6 +556,13 @@
 // RegisterAsset 平台登记 → 落 kind=result 行(唯一键冲突时重读幂等返回)。
 // 返回 (输出, 是否幂等命中既有)。调用方负责工程租户门控;enrich 可为 nil。
 func (s *Server) registerOutputBytes(ctx context.Context, tenant, projID, fileName, contentType string, data []byte, enrich outputContextEnricher) (store.ProjectOutput, bool, *apiErr) {
+	return s.registerOutputBytesFor(ctx, tenant, projID, fileName, contentType, data, enrich, "")
+}
+
+// registerOutputBytesFor is registerOutputBytes plus one extra door: bytes that
+// are a plate-lock derived composite are refused here unless the caller is the
+// return action of exactly the run that derived them (plateRun).
+func (s *Server) registerOutputBytesFor(ctx context.Context, tenant, projID, fileName, contentType string, data []byte, enrich outputContextEnricher, plateRun string) (store.ProjectOutput, bool, *apiErr) {
 	if len(data) == 0 {
 		return store.ProjectOutput{}, false, &apiErr{http.StatusBadRequest, "invalid_request", "data_b64 必须是非空 base64"}
 	}
@@ -571,6 +594,14 @@
 	} else if hit {
 		return store.ProjectOutput{}, false, &apiErr{http.StatusConflict, "source_output_owned", "该素材属于原生成记录，请从原记录选择或导出"}
 	}
+	// A plate-lock derived composite only enters outputs through the server-side
+	// return action of its own run, which also records its quality binding.
+	if owner, hit, err := s.St.PlateDerivationOwnsSHA(ctx, tenant, projID, shaHex); err != nil {
+		log.Printf("store error: %v", err)
+		return store.ProjectOutput{}, false, &apiErr{http.StatusInternalServerError, "internal", "服务内部错误"}
+	} else if hit && owner != plateRun {
+		return store.ProjectOutput{}, false, &apiErr{http.StatusConflict, "source_output_owned", "该素材属于原生成记录，请从原记录选择、导出或回传"}
+	}
 	// 同一份未保真模型字节不能改登记成可回执成果。
 	if hit, err := s.St.UndeliverableModelSHA(ctx, tenant, projID, shaHex); err != nil {
 		log.Printf("store error: %v", err)
@@ -639,6 +670,26 @@
 		writeJSON(w, http.StatusOK, map[string]any{"receipt": existing, "duplicate": true})
 		return
 	}
+	quality, hasQuality, qerr := s.St.GetOutputQuality(ctx, p.Tenant(), outputID)
+	if qerr != nil {
+		writeStoreErr(w, qerr)
+		return
+	}
+	if hasQuality && (quality.CandidateState != si.CandidateLimited || quality.ResultSHA256 != out.ResultSHA256) {
+		rejectUnusableCandidate(w)
+		return
+	}
+	if !hasQuality {
+		// A derived composite without its quality binding (an interrupted return)
+		// must be completed through the return action first, never receipted.
+		if _, owned, oerr := s.St.PlateDerivationOwnsSHA(ctx, p.Tenant(), projID, out.ResultSHA256); oerr != nil {
+			writeStoreErr(w, oerr)
+			return
+		} else if owned {
+			rejectUnusableCandidate(w)
+			return
+		}
+	}
 	if reports, rerr := s.St.ListFidelityReportsByOutput(ctx, p.Tenant(), outputID); rerr != nil {
 		writeStoreErr(w, rerr)
 		return
@@ -696,6 +747,9 @@
 	// 构造 order-receipt/v1(费用事实只读既有账本,绝不触发扣费)。
 	eventID := "wr-" + hexSha(tenantEventSeed(p.Tenant(), projID, outputID, out.ResultSHA256))
 	runID := out.PlatformTaskID
+	if hasQuality {
+		runID = quality.RunID
+	}
 	if runID == "" {
 		runID = "manual-" + out.ID
 	}
@@ -709,7 +763,7 @@
 			AssetRef: out.PlatformAssetID, SHA256: out.ResultSHA256,
 			SizeBytes: out.ResultSize, MediaType: out.MediaType,
 		}},
-		BillingFactRefs: s.billingFactRefs(ctx, out),
+		BillingFactRefs: s.receiptBillingRefs(ctx, out, quality, hasQuality),
 		OccurredAt:      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
 	}
 	if err := receipt.Validate(); err != nil {
@@ -776,6 +830,15 @@
 	return refs
 }
 
+// receiptBillingRefs: a quality-bound output cites the one original charge it
+// was derived from; anything else keeps the read-only ledger lookup.
+func (s *Server) receiptBillingRefs(ctx context.Context, out store.ProjectOutput, q store.OutputQuality, bound bool) []string {
+	if bound {
+		return []string{q.OriginalChargeID}
+	}
+	return s.billingFactRefs(ctx, out)
+}
+
 func tenantEventSeed(parts ...string) string {
 	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
 	return hex.EncodeToString(h[:])[:32]
PATCH5
````

旧直调路由退役（`handleModelPlate` 改成 410，删除它独占的 `plateHoldMinor / plateSubmissionID / supplierHTTPRejected / claimModelPlate / replyPlateNotClaimed / stopPlateAttempt`；`readBgOriginal` 抽出共享的 `readInputOriginal`；同时删除描述已不存在行为的 7 个 `TestFidelityModelPlate*` 与其辅助 `armModelPlate`）。这是一次**脚本化编辑**，已逐字节验证会产出与计划作者树完全相同的两个文件：

````bash
cd "$WT"
python3 - <<'PY'
# 1) bgreplace.go: retire the direct-supplier route and share the Upload read.
p = 'server/internal/httpapi/bgreplace.go'
s = open(p).read()
a = s.index('func (s *Server) handleModelPlate(')
b = s.index('func decodeLockedPNG(')
stub = '''// handleModelPlate is permanently retired. It called the image model directly,
// outside Billing hold/charge and the Task owner, so it must never become
// reachable. Limited-fidelity background replacement now runs through the
// priced source-image-runs flow (mode background_plate_lock).
func (s *Server) handleModelPlate(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusGone, "direct_supplier_route_retired", "该入口已停用：限定保真换背景改走报价、确认后的正式生成流程")
}

'''
s = s[:a] + stub + s[b:]
old_start = s.index('func (s *Server) readBgOriginal(')
old_end = s.index('func (s *Server) handleLockBgReplace(')
body = s[old_start:old_end]
new = body.replace('func (s *Server) readBgOriginal(ctx context.Context, p platform.Principal, job store.BgJob) ([]byte, error) {\n\tin, err := s.St.GetInput(ctx, p.Tenant(), job.ProjectID, job.InputID)',
'''func (s *Server) readBgOriginal(ctx context.Context, p platform.Principal, job store.BgJob) ([]byte, error) {
	return s.readInputOriginal(ctx, p, p.Tenant(), job.ProjectID, job.InputID)
}

// readInputOriginal reads the bytes behind a project input from Upload and
// proves them against the asset's ready status, size and SHA-256.
func (s *Server) readInputOriginal(ctx context.Context, p platform.Principal, tenant, project, input string) ([]byte, error) {
	in, err := s.St.GetInput(ctx, tenant, project, input)''')
new = new.replace('local, lerr := s.St.GetPhotoAssetByAssetID(ctx, p.Tenant(), in.PlatformAssetID)', 'local, lerr := s.St.GetPhotoAssetByAssetID(ctx, tenant, in.PlatformAssetID)')
assert new != body and 'readInputOriginal' in new
s = s[:old_start] + new + s[old_end:]
for imp in ['\\t"bytes"\\n', '\\t"image/png"\\n', '\\t"strconv"\\n']:
    s = s.replace(imp.encode().decode('unicode_escape'), '', 1)
open(p, 'w').write(s)

# 2) bgreplace_test.go: the direct-route tests describe behavior that no longer exists.
p = 'server/internal/httpapi/bgreplace_test.go'
s = open(p).read()
a = s.index('func TestFidelityModelPlateLocksSubjectFromLiveHost(')
b = s.index('func coverProtectedColumn(')
s = s[:a] + s[b:]
for imp in ['\\t"errors"\\n', '\\t"time"\\n', '\\t"github.com/bianjiefilm/product-image-engine/server/internal/store"\\n']:
    s = s.replace(imp.encode().decode('unicode_escape'), '', 1)
open(p, 'w').write(s)
PY
````

- [ ] **Step 5: 写实现——进程接线**

`assemblePlateLock`：读 `FEATURE_BG_PLATE_LOCK` / `PRODUCT_FIDELITY_SAMPLES_DIR`，装载样本集；缺目录 → `sample_set_unconfigured`，被拒 → `sample_set_invalid`（只写日志、**不让进程 fatal**）；同时把 `buildinfo` 写进 `Service.Build`：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/server/cmd/server/source_runtime.go
+++ b/server/cmd/server/source_runtime.go
@@ -4,14 +4,19 @@
 	"context"
 	"errors"
 	"io"
+	"log"
 	"net"
 	"net/http"
 	"net/url"
 	"strconv"
+	"strings"
 	"sync"
 	"time"
 
+	"github.com/bianjiefilm/product-image-engine/server/internal/buildinfo"
 	"github.com/bianjiefilm/product-image-engine/server/internal/config"
+	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
+	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
 	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
 	"github.com/bianjiefilm/product-image-engine/server/internal/sourceflow"
 	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
@@ -47,9 +52,35 @@
 	if cfg.SourceDownloadHosts == "" {
 		reason = "source_download_hosts_unconfigured"
 	}
-	return &sourceflow.Service{OutputRecoveryReason: reason, Store: st, Auth: &sourceflow.Authorizer{Store: st, Context: clients.Context, AppID: cfg.AppID}, Bill: clients.Bill, Tasks: clients.Tasks, Assets: sourceRuntimeAssets{AssetPort: clients.Assets, outputConfigured: cfg.SourceDownloadHosts != ""}, Profile: sourceflow.NewProfile(cfg)}, nil
+	svc := &sourceflow.Service{OutputRecoveryReason: reason, Store: st, Auth: &sourceflow.Authorizer{Store: st, Context: clients.Context, AppID: cfg.AppID}, Bill: clients.Bill, Tasks: clients.Tasks, Assets: sourceRuntimeAssets{AssetPort: clients.Assets, outputConfigured: cfg.SourceDownloadHosts != ""}, Profile: sourceflow.NewProfile(cfg)}
+	assemblePlateLock(svc, cfg)
+	return svc, nil
 }
 
+// assemblePlateLock wires the optional limited-fidelity mode. It never makes the
+// process fatal: a missing or rejected sample set only closes the mode, with a
+// stable reason that capabilities report. The flag and set are immutable while
+// serving.
+func assemblePlateLock(svc *sourceflow.Service, cfg config.Config) {
+	bi := buildinfo.Read()
+	svc.Build = platelock.Build{VCSRevision: bi.VCSRevision, VCSModified: bi.VCSModified}
+	svc.PlateEnabled = cfg.BgPlateLockEnabled
+	if !cfg.BgPlateLockEnabled {
+		return
+	}
+	if strings.TrimSpace(cfg.FidelitySamplesDir) == "" {
+		svc.SamplesReason = "sample_set_unconfigured"
+		return
+	}
+	set, err := fidelitysamples.Load(cfg.FidelitySamplesDir)
+	if err != nil {
+		svc.SamplesReason = "sample_set_invalid"
+		log.Printf("product-image-server: frozen sample set rejected, background_plate_lock stays closed: %v", err)
+		return
+	}
+	svc.Samples = set
+}
+
 type sourceRuntimeAssets struct {
 	sourceflow.AssetPort
 	outputConfigured bool
PATCH1
````

- [ ] **Step 6: 运行，确认通过**

```bash
cd "$WT/server" && gofmt -l internal cmd | grep -v entry_test.go; go vet ./...
"$RUN/bin/heavy" -- go test -count=1 ./internal/store ./internal/httpapi ./cmd/server 2>&1 | tail -6
```

期望：三者 `ok`（`httpapi` 约 25–35 秒）；**既有**的 `httpapi` 测试（含全部 text_generate 的 source 测试、bgreplace 其余测试、handoff 测试）一条不改、一条不挂。

- [ ] **Step 7: 真实进程冒烟（不是 mock）——构建身份与 `/readyz` 的真实形状**

用本任务的代码构建**真实二进制**（显式 `-ldflags` 构建身份），以占位配置起进程（不连任何平台；所有 token 都是占位值，不是秘密），读 `/readyz`；再用一份**被破坏的**样本集重启，确认只关闭本模式而进程仍 `ok:true`。端口只用 `32320`（server）与不监听的 `32325–32329`（占位 base，启动期不发请求）：

```bash
cd "$WT/server" && mkdir -p "$RUN/caches/HUI-2232/smoke" && SM="$RUN/caches/HUI-2232/smoke"
REV=$(git -C "$WT" rev-parse HEAD); MOD=$([ -n "$(git -C "$WT" status --porcelain)" ] && echo true || echo false)
go build -buildvcs=false -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=$REV -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=$MOD" -o "$SM/product-server" ./cmd/server
run_smoke() { # $1 = samples dir
  rm -f "$SM"/product.db*
  PRODUCT_SERVER_ADDR=127.0.0.1:32320 PRODUCT_DB_PATH="$SM/product.db" PRODUCT_INTERNAL_TOKEN=smoke-internal \
  PLATFORM_IDENTITY_BASE_URL=http://127.0.0.1:32329 PLATFORM_IDENTITY_APP_ID=product-image PLATFORM_IDENTITY_TOKEN=smoke-id \
  PLATFORM_BILLING_BASE_URL=http://127.0.0.1:32327 PLATFORM_BILLING_TOKEN=smoke-bill PLATFORM_TASK_BASE_URL=http://127.0.0.1:32326 PLATFORM_TASK_TOKEN=smoke-task \
  PLATFORM_UPLOAD_BASE_URL=http://127.0.0.1:32325 PLATFORM_UPLOAD_TOKEN=smoke-up \
  FEATURE_SOURCE_IMAGES=1 FEATURE_GENERATION_ENABLED=1 ECO_BILLING_ENABLED=1 PRODUCT_SOURCE_PRICING_VERSION=smoke-v1 PRODUCT_SOURCE_DOWNLOAD_HOSTS=fixture.oss-cn-hangzhou.aliyuncs.com \
  FEATURE_BG_PLATE_LOCK=1 PRODUCT_FIDELITY_SAMPLES_DIR="$1" "$SM/product-server" > "$SM/server.log" 2>&1 &
  echo $! > "$SM/pid"; sleep 3
  curl -s http://127.0.0.1:32320/readyz | python3 -c "import json,sys; d=json.load(sys.stdin); print(json.dumps({'ok':d['ok'],'build':d['build'],'plate_lock':d['gates']['plate_lock']}))"
  kill "$(cat "$SM/pid")"; sleep 1
}
run_smoke "$WT/fixtures/frozen-samples/v1"
rm -rf "$SM/broken" && cp -R "$WT/fixtures/frozen-samples/v1" "$SM/broken" && rm "$SM/broken/masks/carton-1024x1024.png"
run_smoke "$SM/broken"
```

期望（第一条）：`{"ok": true, "build": {"vcs_revision": "<你的 HEAD>", "vcs_modified": <true 或 false>}, "plate_lock": {"enabled": true, "reason": "", "samples_loaded": true, "usable": true}}`——`vcs_revision` 必须等于 `git rev-parse HEAD`（工作区有未提交改动时 `vcs_modified` 为 `true`，**正式验收构建要求为 `false`**）；期望（第二条）：`ok` 仍为 `true`，`plate_lock` 为 `{"enabled": true, "reason": "sample_set_invalid", "samples_loaded": false, "usable": false}`，`$SM/server.log` 里有一行 `frozen sample set rejected, background_plate_lock stays closed: … case "carton-1024x1024": mask: not a regular file`。结束后 `rm -rf "$SM"`，确认没有残留进程（`lsof -nP -iTCP:32320 -sTCP:LISTEN` 无输出）。

- [ ] **Step 8: 全仓回归与提交**

```bash
cd "$WT/server" && "$RUN/bin/heavy" -- go test -p 2 -count=1 ./... 2>&1 | tee "$EV/task4-go-test.log" | grep -v "^ok" | head        # 期望：无输出
"$RUN/bin/heavy" -- go test -count=1 -race -run 'Plate|F21|Readyz|FlagOff' ./internal/httpapi 2>&1 | tail -3                                        # 期望：ok
cd "$WT" && git add server && git status --short                                                                                           # 只能出现 server/ 下的文件
git commit -m "feat(http): expose limited-fidelity runs, retire the direct model route, bind quality to returns

plate 模式的创建只接受封闭键集（input_id + 背景文字），拒绝发生在任何付费调用之前；
能力端点只读本地登记；plate 视图/内容/导出/选定/回传全部由派生记录门控，
原始底板永不展示；派生合成字节只能经 return 登记并写质量绑定，回执引用原 charge。
旧 model-plate 路由 410（零模型调用）。/readyz 增加构建身份与 plate_lock 门，
样本集被拒只关闭本模式。迁移 0021 只加表。

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


---

## Task 5：Web——真实上传、限定保真面板、能力矩阵、人话文案与浏览器取证（fixture 栈）

**交付物：** `/start` 个人路径换成“真实上传（`<input type=file>`，不再是禁用按钮）→ 限定保真换背景”，旧 first-image 表单与开发者字段退出个人路径；`/projects/[id]` 同样挂载；source BFF 放行限定保真的封闭请求体 / `return` 动作 / 稳定错误码；新的无障碍小原语（Field / Alert / Skeleton / Stepper）与语义变量层；能力矩阵如实显示每个模式；旧 `model-plate` 按钮与 BFF 动作删除；Playwright（`playwright-core@1.63.0`，本机 Chrome 二进制 + 独立 profile + 端口 `32321/32324`）在 fixture 栈上取证。对应 spec §4.8、§4.10 与 A6、A9；**所有浏览器证据标 `fixture`，不当验收**。

**Files:**
- Create: `web/src/lib/plate-lock.ts`、`web/src/components/plate-lock/{PlateLockScreen.tsx,PhotoUpload.tsx}`、`web/src/components/ui/{alert,field,skeleton,stepper}.tsx`
- Create: `web/tests/{plate-lock-bff.test.ts,plate-lock-controller.test.ts,plate-lock-screen.test.tsx,plate-lock-upload.test.ts}`
- Create: `web/e2e/{zip.mjs,fixture-api.mjs,fixture-run.mjs}`
- Modify: `web/src/lib/source-image.ts`、`web/src/app/api/projects/[id]/source-image-runs/proxy.ts`、`web/src/app/api/projects/[id]/background-replacements/[jobId]/[action]/route.ts`、`web/src/components/{source-image/Panel.tsx,first-image/StartScreen.tsx,background-replace/Panel.tsx}`、`web/src/app/{start/ui.tsx,projects/[id]/page.tsx,globals.css}`、`web/src/components/ui/ui.module.css`、`web/tests/{source-image-fixture.ts,source-image-mounts.test.tsx,bg-replace.test.ts}`、`web/package.json`、`web/package-lock.json`

**Interfaces:**
- Consumes（Task 4 的 HTTP 契约，字段名逐字）：见 Task 4 Interfaces（创建封闭键集、能力项、`plate` 视图、`content` / `export` / `return`、错误码 `sample_not_frozen | candidate_not_usable | not_selected | no_source_binding | no_composite | original_missing | plate_lock_disabled | sample_set_unconfigured | sample_set_invalid | plate_org_not_supported`）。
- Produces：
  - `SourceImagePanel({projectId?, mode?: "text_generate"|"background_plate_lock", returnEnabled?, reloadKey?, uploadSlot?, onReturned?})`（`mode` 默认 `text_generate`，文字面板行为不变；恢复参数分别是 `?source_run=` 与 `?plate_run=`；待核实请求键在 sessionStorage 里按模式分键）。
  - `SourceImageController(api, uuid?, now?, mode?)` 新增 `quotePlate({projectID, inputID, background})` 与 `returnToSource(): Promise<string>`；`confirm()` / `action()` 按**当前 run 的模式**读能力；`remember()` 拒绝另一种模式的 run（两个面板不会互换视图）。
  - 纯展示组件 `PlateLockScreen(props)`（可用 `renderToStaticMarkup` 测），含 `CapabilityMatrix`；`PhotoUpload` 与 `uploadAndAttach(file, projectId, fetcher)`；`web/src/lib/plate-lock.ts` 的文案常量、`TECHNICAL_ONLY_TERMS`、`canSelectPlate/canExportPlate/canReturnPlate` 等。
  - `/start?project=<id>`：个人工程在刷新 / 重登后靠地址找回；`?plate_run=<runId>` 找回同一 run。
  - 证据：`$EV/browser/fixture/fixture-*.png` 与 `fixture-run-record.json`（`layer:"fixture"`）。

- [ ] **Step 1: 先写失败的测试，并更新三处既有测试**

BFF（封闭请求体按字节原样转发、夹带任何质量 / 样本 / 蒙版 / 价格 / prompt 字段先于上游就 400、`return` 只许 POST 且空 body、稳定错误码**只在各自状态码下**透出且其它一律掩码、畸形 plate 能力项 / 偷带原始底板输出的视图被拒、分数字段不回显、小数分数不是合法线上 JSON、`model-plate` 动作在 BFF 层 404）：

`web/tests/plate-lock-bff.test.ts`

````ts
import { describe, it, expect, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";
import { GET as caps } from "@/app/api/projects/[id]/source-image-capabilities/route";
import { POST as create } from "@/app/api/projects/[id]/source-image-runs/route";
import { GET, POST } from "@/app/api/projects/[id]/source-image-runs/[runId]/[action]/route";
import { POST as bgAction } from "@/app/api/projects/[id]/background-replacements/[jobId]/[action]/route";
import { plateCapabilities, plateDone, plateQuoted } from "./source-image-fixture";

const base = "http://localhost:3000";
const path = "/api/projects/proj_a/source-image-runs";
const ctx = (action?: string) => ({ params: Promise.resolve({ id: "proj_a", runId: "sir_plate", action: action ?? "" }) });
type ReqInit = NonNullable<ConstructorParameters<typeof NextRequest>[1]>;
const request = (suffix = "", init: ReqInit = {}) => new NextRequest(base + path + suffix, { ...init, headers: { cookie: "pia_access=good-token", origin: base, "content-type": "application/json", ...init.headers } });
const plateBody = { request_key: "request-plate", mode: "background_plate_lock", input_id: "pin_a", background_intent: "深蓝色渐变摄影棚背景" };
beforeEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("limited-fidelity BFF", () => {
  it("forwards the closed plate create body byte for byte", async () => {
    const calls: Array<[unknown, RequestInit]> = [];
    vi.stubGlobal("fetch", async (u: unknown, i: RequestInit) => { calls.push([u, i]); return Response.json(plateQuoted()); });
    const body = JSON.stringify(plateBody);
    const res = await create(request("", { method: "POST", body }), { params: Promise.resolve({ id: "proj_a" }) });
    expect(res.status).toBe(200);
    expect(calls[0][0]).toBe("http://127.0.0.1:18220/api/v1/projects/proj_a/source-image-runs");
    expect(calls[0][1].body).toBe(body);
    expect((await res.json()).plate.derivation_state).toBe("pending");
  });

  it("rejects every browser-supplied quality, sample, mask, price or prompt field before upstream", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(plateQuoted()); });
    const bad: Array<Record<string, unknown>> = [
      { ...plateBody, sample_id: "carton-1024x1024" }, { ...plateBody, mask_png_base64: "AAAA" }, { ...plateBody, passed: "true" },
      { ...plateBody, quality: "pass" }, { ...plateBody, coverage: "x" }, { ...plateBody, supplier_authorized: "true" }, { ...plateBody, amount_minor: "1" },
      { ...plateBody, prompt: "draw" }, { ...plateBody, size: "1024*1024" },
      { request_key: "request-plate", mode: "background_plate_lock", input_id: "pin_a" },
      { ...plateBody, input_id: "../x" }, { ...plateBody, input_id: 7 },
      { ...plateBody, background_intent: "" }, { ...plateBody, background_intent: " leading" }, { ...plateBody, background_intent: "a\nb" }, { ...plateBody, background_intent: "界".repeat(201) },
      { ...plateBody, request_key: "a\u0000b" },
    ];
    for (const b of bad) expect((await create(request("", { method: "POST", body: JSON.stringify(b) }), { params: Promise.resolve({ id: "proj_a" }) })).status, JSON.stringify(b)).toBe(400);
    expect(calls).toEqual([]);
  });

  it("text creation stays closed to plate fields", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json(plateQuoted()); });
    const body = JSON.stringify({ request_key: "r", mode: "text_generate", prompt: "x", size: "1024*1024", input_id: "pin_a" });
    expect((await create(request("", { method: "POST", body }), { params: Promise.resolve({ id: "proj_a" }) })).status).toBe(400);
    expect(calls).toEqual([]);
  });

  it("the return action is POST only with an empty body, and select/confirm stay as before", async () => {
    const calls: Array<[unknown, RequestInit]> = [];
    vi.stubGlobal("fetch", async (u: unknown, i: RequestInit) => { calls.push([u, i]); return Response.json({ output: { id: "out_a" } }); });
    expect((await GET(request("/sir_plate/return"), ctx("return"))).status).toBe(405);
    expect((await POST(request("/sir_plate/return", { method: "POST", body: '{"output_id":"out_x"}' }), ctx("return"))).status).toBe(400);
    expect(calls).toEqual([]);
  });

  it("passes the stable limited-fidelity error codes, each only with its own status, and masks everything else", async () => {
    const reply = (status: number, code: string) => { vi.stubGlobal("fetch", async () => Response.json({ error: { code, message: "internal detail https://private.invalid?token=secret" }, run_id: "sir_plate" }, { status })); };
    for (const [status, code] of [[422, "sample_not_frozen"], [409, "candidate_not_usable"], [409, "not_selected"], [409, "no_source_binding"], [503, "plate_lock_disabled"], [503, "sample_set_invalid"]] as const) {
      reply(status, code);
      const res = await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"));
      const text = await res.text();
      expect(res.status, code).toBe(status);
      expect(JSON.parse(text).error.code).toBe(code);
      expect(text).not.toContain("private.invalid");
      expect(text).not.toContain("secret");
    }
    reply(409, "sample_not_frozen"); // right code, wrong status: masked to the generic conflict
    expect(JSON.parse(await (await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).text()).error.code).toBe("conflict");
    reply(500, "plate_lock_disabled");
    expect(JSON.parse(await (await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).text()).error.code).toBe("source_unavailable");
  });

  it("decodes plate capabilities and refuses a malformed plate entry", async () => {
    vi.stubGlobal("fetch", async () => Response.json(plateCapabilities(true)));
    const ok = await caps(new NextRequest(base + "/api/projects/proj_a/source-image-capabilities", { headers: { cookie: "pia_access=good-token" } }), { params: Promise.resolve({ id: "proj_a" }) });
    expect(ok.status).toBe(200);
    expect((await ok.json()).modes.find((m: { mode: string }) => m.mode === "background_plate_lock").supported_input_ids).toEqual(["pin_a"]);
    const broken = plateCapabilities(true);
    (broken.modes.find(m => m.mode === "background_plate_lock") as { supported_input_ids: unknown }).supported_input_ids = "pin_a";
    vi.stubGlobal("fetch", async () => Response.json(broken));
    const bad = await caps(new NextRequest(base + "/api/projects/proj_a/source-image-capabilities", { headers: { cookie: "pia_access=good-token" } }), { params: Promise.resolve({ id: "proj_a" }) });
    expect(bad.status).toBe(503);
  });

  it("refuses a plate run that smuggles the raw plate as an output, and never echoes an unknown score field", async () => {
    const smuggled = JSON.parse(JSON.stringify(plateDone())) as Record<string, unknown>;
    smuggled.output = { asset_id: "a", reference_id: "r", sha256: "a".repeat(64), content_type: "image/png", size_bytes: "1", width_px: 1024, height_px: 1024, association_verified: true };
    vi.stubGlobal("fetch", async () => Response.json(smuggled));
    expect((await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).status).toBe(503);
    const scored = JSON.parse(JSON.stringify(plateDone())) as { plate: Record<string, unknown> };
    scored.plate.similarity = 99;
    vi.stubGlobal("fetch", async () => Response.json(scored));
    const res = await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"));
    expect(res.status).toBe(200);
    expect(JSON.stringify(await res.json())).not.toContain("similarity");
    // A fractional score is not even valid wire JSON for this contract.
    vi.stubGlobal("fetch", async () => new Response(JSON.stringify(scored).replace('"similarity":99', '"similarity":0.99'), { status: 200, headers: { "content-type": "application/json" } }));
    expect((await POST(request("/sir_plate/select", { method: "POST", body: "{}" }), ctx("select"))).status).toBe(503);
  });

  it("retires the model-plate action at the background-replacement BFF", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", async () => { calls.push("upstream"); return Response.json({}); });
    const res = await bgAction(new NextRequest(base + "/api/projects/proj_a/background-replacements/job_a/model-plate", { method: "POST", headers: { cookie: "pia_access=good-token" }, body: "{}" }), { params: Promise.resolve({ id: "proj_a", jobId: "job_a", action: "model-plate" }) });
    expect(res.status).toBe(404);
    expect(calls).toEqual([]);
  });
});
````

控制器（只发 `request_key/mode/input_id/background_intent`、不在 `supported_input_ids` 里的输入先于任何请求就拒、未就绪拒、双击合并为一次 create 且重试沿用原键、不接受另一种模式的 run、确认用 plate 模式能力、只有“已选定的限定候选”才能回传、待核实键按模式分开、`validBackground` 镜像服务端规则、解码器让两种模式互不串味且 `limited_candidate` 缺限制 / 结果 / 结论即拒）：

`web/tests/plate-lock-controller.test.ts`

````ts
import { describe, expect, it } from "vitest";
import { SourceImageController, sourceImageAPI, validBackground, decodeSourceRun, pendingHintKey } from "@/lib/source-image";
import type { PlateCreate, SourceCreate } from "@/lib/source-image";
import { plateCapabilities, plateDone, plateQuoted, quoted, scope, sourceAPIFixture } from "./source-image-fixture";

function plateController(calls: string[], over: Partial<ReturnType<typeof sourceAPIFixture>> = {}) {
  const sent: SourceCreate[] = [];
  const api = { ...sourceAPIFixture(calls), capabilities: async () => plateCapabilities(true), create: async (_p: string, input: SourceCreate) => { sent.push(input); return { ...plateQuoted(), request_key: input.request_key }; }, ...over };
  const c = new SourceImageController(api, () => "request-plate", () => 1700000100, "background_plate_lock");
  c.setScope(scope);
  return { c, sent };
}

describe("plate-lock controller", () => {
  it("quotes with exactly request_key, mode, input_id and background_intent", async () => {
    const calls: string[] = [];
    const { c, sent } = plateController(calls);
    await c.loadCapabilities();
    await c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "  深蓝色渐变摄影棚背景  " });
    expect(sent).toHaveLength(1);
    expect(Object.keys(sent[0]).sort()).toEqual(["background_intent", "input_id", "mode", "request_key"]);
    expect((sent[0] as PlateCreate).background_intent).toBe("深蓝色渐变摄影棚背景");
    expect(c.view?.mode).toBe("background_plate_lock");
  });

  it("refuses an input the server did not list as supported, before any request", async () => {
    const calls: string[] = [];
    const { c, sent } = plateController(calls);
    await c.loadCapabilities();
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_other", background: "深蓝" })).rejects.toThrow("不在已验证范围");
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "a\nb" })).rejects.toThrow("背景描述");
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "" })).rejects.toThrow("背景描述");
    expect(sent).toEqual([]);
  });

  it("refuses to quote when the mode is not ready", async () => {
    const { c, sent } = plateController([], { capabilities: async () => plateCapabilities(false) });
    await c.loadCapabilities();
    await expect(c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "深蓝" })).rejects.toThrow("不允许获取正式报价");
    expect(sent).toEqual([]);
  });

  it("double click coalesces to one create and a retry after an unknown reuses the original key", async () => {
    const calls: string[] = [];
    let attempts = 0;
    const { c } = plateController(calls, { create: async (_p, input) => { attempts++; if (attempts === 1) throw new Error("network"); return { ...plateQuoted(), request_key: input.request_key }; }, find: async () => null });
    await c.loadCapabilities();
    const input = { projectID: "proj_a", inputID: "pin_a", background: "深蓝" };
    await Promise.allSettled([c.quotePlate(input), c.quotePlate(input)]);
    expect(attempts).toBe(1);
    await c.quotePlate(input);
    expect(attempts).toBe(2);
    expect(c.pendingHint?.requestKey).toBe("request-plate");
  });

  it("never accepts a run of the other mode, so the two panels cannot swap views", async () => {
    const { c } = plateController([], { get: async () => quoted() });
    await expect(c.restore("proj_a", "sir_a")).rejects.toThrow("请求类型不一致");
    expect(c.view).toBeNull();
    const text = new SourceImageController(sourceAPIFixture([]), undefined, undefined, "text_generate");
    text.setScope(scope);
    await expect((async () => { await text.restore("proj_a", "sir_plate"); })().catch(e => { throw e; })).rejects.toBeDefined();
  });

  it("confirm uses the plate mode capability and the original quote only", async () => {
    const calls: string[] = [];
    const { c } = plateController(calls, { confirm: async (_p, _r, input) => { calls.push("confirm:" + input.quote_id + ":" + input.quote_fingerprint); return { ...plateQuoted(), phase: "task_linked", confirmed: true, task_id: "task_a" }; } });
    await c.loadCapabilities();
    await c.quotePlate({ projectID: "proj_a", inputID: "pin_a", background: "深蓝" });
    await c.confirm();
    expect(c.view?.confirmed).toBe(true);
    expect(calls).toContain("confirm:quote_a:" + "a".repeat(64));
  });

  it("return is possible only for a selected limited candidate", async () => {
    const calls: string[] = [];
    const { c } = plateController(calls, { get: async () => plateDone(), select: async () => ({ ...plateDone(), selected: true }) });
    await c.loadCapabilities();
    await c.restore("proj_a", "sir_plate");
    await expect(c.returnToSource()).rejects.toThrow("只有已选定的限定候选才能回传");
    await c.action("select");
    expect(await c.returnToSource()).toBe("out_a");
    expect(calls).toContain("returnOutput");
    const { c: bad } = plateController([], { get: async () => ({ ...plateDone("not_usable_candidate"), selected: true }) });
    await bad.loadCapabilities();
    await bad.restore("proj_a", "sir_plate");
    await expect(bad.returnToSource()).rejects.toThrow();
  });

  it("pending hints are separate per mode", () => {
    expect(pendingHintKey(scope, "background_plate_lock")).not.toBe(pendingHintKey(scope, "text_generate"));
    expect(pendingHintKey(scope)).toBe(pendingHintKey(scope, "text_generate"));
  });

  it("validBackground mirrors the server rule", () => {
    for (const [v, ok] of [["深蓝背景", true], ["", false], [" x", false], ["x ", false], ["a\nb", false], ["a\u007fb", false], ["界".repeat(200), true], ["界".repeat(201), false]] as const) expect(validBackground(v), JSON.stringify(v)).toBe(ok);
  });

  it("the decoder keeps the two modes apart", () => {
    expect(() => decodeSourceRun({ ...quoted(), plate: plateQuoted().plate })).toThrow();
    expect(() => decodeSourceRun({ ...plateQuoted(), plate: undefined })).toThrow();
    expect(() => decodeSourceRun({ ...plateQuoted(), fidelity: "not_applicable" })).toThrow();
    expect(() => decodeSourceRun({ ...quoted(), fidelity: "frozen_sample_only" })).toThrow();
    const done = decodeSourceRun(JSON.parse(JSON.stringify(plateDone())));
    expect(done.plate?.candidate_state).toBe("limited_candidate");
    const lying = JSON.parse(JSON.stringify(plateDone()));
    lying.plate.limits = [];
    expect(() => decodeSourceRun(lying)).toThrow();
  });

  it("the real API object exposes the plate create and return operations", () => {
    expect(typeof sourceImageAPI.returnOutput).toBe("function");
  });
});
````

界面状态（加载 / 禁用并给原因 / 无范围内照片 / 空表单 / 已报价 / 报价过期 / 生成中 / 限定候选 / 不可用候选（`role=alert`、按钮禁用、无导出）/ 回传需明确确认 / **主视图没有任何内部词与 40 位以上十六进制** / 能力矩阵如实且关闭项无按钮 / 项目未建时矩阵不装懂 / 标签与实时区 / 通知即 alert）：

`web/tests/plate-lock-screen.test.tsx`

````tsx
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PlateLockScreen } from "@/components/plate-lock/PlateLockScreen";
import type { PlateScreenProps } from "@/components/plate-lock/PlateLockScreen";
import { TECHNICAL_ONLY_TERMS } from "@/lib/plate-lock";
import { plateCapabilities, plateDone, plateQuoted } from "./source-image-fixture";

const base: PlateScreenProps = { view: null, capabilities: plateCapabilities(true), nowUnix: 1700000100, projectID: "proj_a", background: "" };
const html = (p: Partial<PlateScreenProps>) => renderToStaticMarkup(createElement(PlateLockScreen, { ...base, ...p }));
// The technical details fold is the only place internal words may appear.
const primary = (markup: string) => markup.replace(/<details data-technical="true">[\s\S]*?<\/details>/g, "");
// What a person reads: tags and attributes (class names, image URLs) are not text.
const visible = (markup: string) => primary(markup).replace(/<[^>]*>/g, " ");

describe("limited-fidelity screen states", () => {
  it("loading state shows a polite status and skeletons, no form", () => {
    const m = html({ loading: true, capabilities: null });
    expect(m).toContain('aria-busy="true"');
    expect(m).toContain("正在读取");
    expect(m).not.toContain("<form");
  });

  it("disabled state names the reason in plain words and offers no paid action", () => {
    for (const [reason, text] of [["plate_lock_disabled", "暂未开放"], ["sample_set_invalid", "没有通过校验"], ["plate_org_not_supported", "只支持个人付款"], ["project_write_forbidden", "不能修改这个工程"]] as const) {
      const caps = plateCapabilities(false);
      caps.modes = caps.modes.map(m => (m.mode === "background_plate_lock" ? { ...m, reason } : m));
      const m = html({ capabilities: caps });
      expect(m).toContain(text);
      expect(m).toContain('data-ready="false"');
      expect(m).not.toContain("获取报价");
      expect(m).not.toContain("确认并生成");
    }
  });

  it("ready but no photo inside the verified range explains the limit and creates no form", () => {
    const caps = plateCapabilities(true);
    caps.modes = caps.modes.map(m => (m.mode === "background_plate_lock" ? { ...m, supported_input_ids: [], supported_inputs: [] } : m));
    const m = html({ capabilities: caps });
    expect(m).toContain("冻结样本");
    expect(m).toContain("不会产生费用");
    expect(m).not.toContain("获取报价");
  });

  it("empty ready state is a labelled form with the pricing promise and a disabled quote button", () => {
    const m = html({});
    expect(m).toContain('for="plate-background"');
    expect(m).toContain('id="plate-background"');
    expect(m).toContain("不会自动重做，也不会再次扣费");
    expect(m).toMatch(/<button[^>]*disabled=""[^>]*>获取报价/);
    expect(html({ background: "深蓝色背景" })).not.toMatch(/<button[^>]*disabled=""[^>]*>获取报价/);
  });

  it("quoted state shows the amount, payer and one confirm button, with the honest cost sentence", () => {
    const m = html({ view: plateQuoted() });
    expect(m).toContain("¥0.25");
    expect(m).toContain("个人账户");
    expect(m).toContain("确认并生成 ¥0.25");
    expect(m).toContain("不会自动重做，也不会再次扣费");
    expect(m).not.toContain("plate-select");
  });

  it("an expired quote cannot be confirmed", () => {
    expect(html({ view: plateQuoted(), nowUnix: 1700000300 })).not.toContain("确认并生成");
  });

  it("pending derivation keeps the user informed and offers no selection", () => {
    const view = { ...plateQuoted(), confirmed: true, phase: "task_linked" };
    const m = html({ view });
    expect(m).toContain("正在生成背景并检查商品");
    expect(m).not.toContain("plate-select");
  });

  it("a limited candidate shows per-axis results in four plain states, the limits, and enables select and export", () => {
    const m = primary(html({ view: plateDone() }));
    expect(m).toContain('data-candidate="limited_candidate"');
    expect(m).toContain("通过商品检查（限定范围）");
    expect(m).toContain("商品像素与原图完全一致");
    expect(m).toContain(">通过<");
    expect(m).toContain(">待核实<");
    expect(m).toContain(">不适用<");
    expect(m).toContain("使用限制");
    expect(m).toContain("尚未经过真人评审");
    expect(m).toMatch(/<button[^>]*data-testid="plate-select"[^>]*>选定这张/);
    expect(m).not.toMatch(/<button[^>]*disabled=""[^>]*data-testid="plate-select"/);
    expect(m).toContain('data-testid="plate-export"');
    expect(m).toContain('src="/api/projects/proj_a/source-image-runs/sir_plate/content"');
  });

  it("an unusable candidate is an alert naming the failed check, and cannot be selected, exported or returned", () => {
    const m = primary(html({ view: plateDone("not_usable_candidate"), returnEnabled: true, returnAck: true }));
    expect(m).toContain('role="alert"');
    expect(m).toContain("未通过商品检查");
    expect(m).toContain("背景相比原图有明显变化");
    expect(m).toContain("未通过");
    expect(m).toMatch(/<button[^>]*disabled=""[^>]*data-testid="plate-select"/);
    expect(m).not.toContain('data-testid="plate-export"');
    expect(m).not.toContain('data-testid="plate-return"');
    expect(m).toContain("不可使用");
  });

  it("return needs an explicit acknowledgement that the receipt omits the limits", () => {
    const selected = { ...plateDone(), selected: true };
    const off = primary(html({ view: selected, returnEnabled: true, returnAck: false }));
    expect(off).toContain("下游收到的回执不含上述限制说明");
    expect(off).toMatch(/<button[^>]*disabled=""[^>]*>回传到来源/);
    const on = primary(html({ view: selected, returnEnabled: true, returnAck: true }));
    expect(on).not.toMatch(/<button[^>]*disabled=""[^>]*>回传到来源/);
    expect(primary(html({ view: plateDone(), returnEnabled: false }))).not.toContain("回传到来源");
  });

  it("the primary view never shows an internal word; the technical fold carries them", () => {
    for (const view of [plateQuoted(), plateDone(), plateDone("not_usable_candidate")]) {
      const m = visible(html({ view, returnEnabled: true }));
      for (const term of TECHNICAL_ONLY_TERMS) expect(m, `${term} leaked into the primary view`).not.toContain(term);
      expect(m).not.toContain("sir_plate");
      expect(m).not.toContain("task_a");
      expect(m).not.toMatch(/[0-9a-f]{40,}/);
    }
    const full = html({ view: plateDone() });
    expect(full).toContain('data-technical="true"');
    expect(full).toContain("sir_plate");
    expect(full).toContain("合成结果 hash");
    expect(full).toContain("c".repeat(64));
  });

  it("the capability matrix tells the truth about every mode, and the closed ones are not clickable", () => {
    const m = html({});
    expect(m).toContain('data-mode="background_plate_lock" data-state="available"');
    for (const id of ["creative_background", "light_scene", "showcase_video", "reference_edit", "first_image"]) expect(m).toContain(`data-mode="${id}" data-state="not_opened"`);
    expect(m).toContain("未开放：还没有真实成功证据");
    expect(m).toContain('data-mode="text_generate" data-state="unavailable"');
    const caps = plateCapabilities(true);
    caps.modes = caps.modes.map(x => (x.mode === "text_generate" ? { ...x, ready: true, disabled: false } : x));
    expect(html({ capabilities: caps })).toContain("不保证商品一致");
    const section = m.slice(m.indexOf('data-testid="capability-matrix"'));
    expect(section).not.toContain("<button");
  });

  it("before any project exists the matrix does not pretend to know", () => {
    const m = html({ capabilities: null, projectID: "" });
    expect(m).toContain('data-mode="background_plate_lock" data-state="unknown"');
    expect(m).toContain("选好照片后显示");
  });

  it("every control has a visible label or text, and a polite live region exists", () => {
    const m = html({ view: plateDone(), returnEnabled: true });
    expect(m).toContain('aria-live="polite"');
    expect(m).toContain('aria-label="制作步骤"');
    expect(m).toContain('aria-current="step"');
    expect(m).not.toMatch(/<img(?![^>]*alt=)/);
  });

  it("a notice is announced as an alert", () => {
    expect(html({ notice: "这张照片不在已验证范围内" })).toMatch(/role="alert"[^>]*data-testid="plate-notice"|data-testid="plate-notice"[^>]*role="alert"/);
  });
});
````

上传（无工程时先建个人工程再挂接、有工程不再建、服务端拒绝文案原样给用户且不挂接、空文件 / 超 20 MB 在任何请求前拒、建工程 / 挂接失败不被吞）：

`web/tests/plate-lock-upload.test.ts`

````ts
import { describe, expect, it } from "vitest";
import { MAX_PHOTO_BYTES, uploadAndAttach } from "@/components/plate-lock/PhotoUpload";

type Call = { url: string; method: string; body?: unknown };
function fetcher(calls: Call[], opts: { photoOk?: boolean; projectOk?: boolean; attachOk?: boolean } = {}): typeof fetch {
  const { photoOk = true, projectOk = true, attachOk = true } = opts;
  return (async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? "GET", body: init?.body });
    if (url.startsWith("/api/photos")) return photoOk ? Response.json({ photo: { platform_asset_id: "asset_1", original_name: "a.png", size_bytes: 3, media_type: "image/png" } }, { status: 201 }) : Response.json({ error: { message: "图片内容无法解码" } }, { status: 422 });
    if (url === "/api/projects") return projectOk ? Response.json({ project: { id: "proj_new" } }, { status: 201 }) : new Response("{}", { status: 500 });
    return attachOk ? Response.json({ input: { id: "pin_1" } }, { status: 201 }) : new Response("{}", { status: 500 });
  }) as typeof fetch;
}
const file = (size = 3, name = "a.png") => new File([new Uint8Array(size).fill(1)], name, { type: "image/png" });

describe("photo upload through the existing BFF routes", () => {
  it("with no project: uploads, creates a personal project, then attaches the input", async () => {
    const calls: Call[] = [];
    expect(await uploadAndAttach(file(), undefined, fetcher(calls))).toBe("proj_new");
    expect(calls.map(c => `${c.method} ${c.url.split("?")[0]}`)).toEqual(["POST /api/photos", "POST /api/projects", "POST /api/projects/proj_new/inputs"]);
    expect(calls[0].url).toContain("purpose=project_photo");
    expect(JSON.parse(String(calls[1].body))).toEqual({ name: "个人商品图", usage_kind: "电商主图", width_px: 1024, height_px: 1024, source_type: "standalone" });
    expect(JSON.parse(String(calls[2].body))).toMatchObject({ platform_asset_id: "asset_1", snapshot_content_type: "image/png" });
  });

  it("with a project: never creates another one", async () => {
    const calls: Call[] = [];
    expect(await uploadAndAttach(file(), "proj_a", fetcher(calls))).toBe("proj_a");
    expect(calls.some(c => c.url === "/api/projects")).toBe(false);
  });

  it("the server's rejection text reaches the user and nothing is attached", async () => {
    const calls: Call[] = [];
    await expect(uploadAndAttach(file(), "proj_a", fetcher(calls, { photoOk: false }))).rejects.toThrow("图片内容无法解码");
    expect(calls).toHaveLength(1);
  });

  it("refuses empty and oversized files before any request", async () => {
    const calls: Call[] = [];
    await expect(uploadAndAttach(file(0), undefined, fetcher(calls))).rejects.toThrow("文件是空的");
    await expect(uploadAndAttach({ size: MAX_PHOTO_BYTES + 1, arrayBuffer: async () => new ArrayBuffer(0), name: "x.png" } as unknown as File, undefined, fetcher(calls))).rejects.toThrow("20 MB");
    expect(calls).toEqual([]);
  });

  it("a failed project or attach step is reported, not swallowed", async () => {
    await expect(uploadAndAttach(file(), undefined, fetcher([], { projectOk: false }))).rejects.toThrow("创建工程没有成功");
    await expect(uploadAndAttach(file(), "proj_a", fetcher([], { attachOk: false }))).rejects.toThrow("没有加入工程");
  });
});
````

既有测试的三处必要更新（**不降低强度**：只改夹具形状、按模式计数、反转“旧按钮存在”的断言）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/web/tests/source-image-fixture.ts
+++ b/web/tests/source-image-fixture.ts
@@ -15,7 +15,7 @@
   return { historical_read: true, automatic_output_recovery_ready: false, modes: [
     { mode: "text_generate", ready: false, disabled: true, can_quote: true, can_confirm: true, reason: "runtime_output_recovery_unconfigured", runtime_reason: "runtime_output_recovery_unconfigured", authorization_reason: "", size: "1024*1024", model: "qwen-image-2.0", fidelity: "not_applicable", visual_quality: "unknown" },
     { mode: "reference_edit", ready: false, disabled: true, reason: "formal_contract_unconfigured" },
-    { mode: "background_plate_lock", ready: false, disabled: true, reason: "formal_contract_unconfigured" },
+    { mode: "background_plate_lock", ready: false, disabled: true, can_quote: false, can_confirm: false, reason: "plate_lock_disabled", size: "1024*1024", model: "qwen-image-2.0", claim: "frozen_synthetic_sample_only", fidelity: "frozen_sample_only", visual_quality: "unknown", supported_input_ids: [], supported_inputs: [] },
   ] };
 }
 export function sourceAPIFixture(calls: string[]): SourceImageAPI {
@@ -30,5 +30,28 @@
     cancel: async () => { calls.push("cancel"); return { ...quoted(), phase: "not_dispatched", cancel_requested: true }; },
     select: async () => { calls.push("select"); return quoted(); },
     deleteOutput: async () => { calls.push("deleteOutput"); return { ...quoted(), deleted: true, output: null }; },
+    returnOutput: async () => { calls.push("returnOutput"); return { output_id: "out_a" }; },
   };
 }
+
+export function plateCapabilities(ready = true): SourceCapabilities {
+  const caps = capabilities();
+  caps.automatic_output_recovery_ready = ready;
+  caps.modes = caps.modes.map(m => m.mode === "background_plate_lock" ? { ...m, ready, disabled: !ready, can_quote: ready, can_confirm: ready, reason: ready ? "" : "plate_lock_disabled", supported_input_ids: ready ? ["pin_a"] : [], supported_inputs: ready ? [{ input_id: "pin_a", kind: "carton", canvas: { width_px: 1024, height_px: 1280 } }] : [] } : m);
+  return caps;
+}
+export function plateQuoted(): SourceRunView {
+  return { ...quoted(), run_id: "sir_plate", request_key: "request-plate", mode: "background_plate_lock", fidelity: "frozen_sample_only", plate: { result_kind: "derived_composite", claim: "frozen_synthetic_sample_only", human_usefulness: "NOT_RUN", derivation_state: "pending", candidate_state: "pending", case_id: "carton-1024x1280", sample_set_id: "frozen-sample-set", background_intent: "深蓝色渐变摄影棚背景", canvas: { width_px: 1024, height_px: 1280 } } };
+}
+const PASS_AXES = ["mask_pixels_identical", "coverage_logo", "coverage_packaging_text", "coverage_spec", "coverage_structure", "product_count", "output_size", "plate_size", "plate_is_new_background", "background_changed", "plate_task_succeeded", "plate_asset_hash_verified", "plate_charged_observed"];
+export function plateDone(candidate: "limited_candidate" | "not_usable_candidate" = "limited_candidate"): SourceRunView {
+  const base = plateQuoted();
+  const axes = [...PASS_AXES.map(name => ({ name, state: (candidate === "not_usable_candidate" && name === "background_changed" ? "FAIL" : "PASS") as "PASS" | "FAIL", machine_checked: true, evidence: "ok" })),
+    ...["background_extra_objects", "background_intent_followed", "visual_composite_quality"].map(name => ({ name, state: "UNKNOWN" as const, machine_checked: false, evidence: "not machine checkable" })),
+    { name: "transmission_edge", state: "N/A" as const, machine_checked: false, evidence: "not machine checkable" }];
+  const ok = candidate === "limited_candidate";
+  return { ...base, phase: "succeeded", confirmed: true, task_id: "task_a", payment: { ...base.payment, status: "charged", charged_minor: "25", original_charged_minor: "25", charge_id: "charge_a", original_charge_id: "charge_a" },
+    plate: { ...base.plate!, derivation_state: "derived", candidate_state: candidate, verdicts: { fidelity: ok ? "PASS" : "PASS", generation: "PASS", billing: "PASS", human_usefulness: "NOT_RUN" }, axes,
+      limits: ok ? ["已验证范围仅限冻结合成样本（纸盒/金属把手杯/玻璃瓶），不代表任意商品照片", "背景内容未经机器检查：可能出现多余物体，也可能与所写方向不符"] : [], report_sha256: "b".repeat(64), composite_sha256: "c".repeat(64),
+      result: { content_type: "image/png", size_bytes: "52341", width_px: 1024, height_px: 1280, sha256: "c".repeat(64) } } };
+}
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/web/tests/source-image-mounts.test.tsx
+++ b/web/tests/source-image-mounts.test.tsx
@@ -21,7 +21,10 @@
 describe("normal actual page mounts and legacy read-only",()=>{
   it("both real page factories mount source creation and legacy historyOnly",()=>{
     for(const page of [StartPage,ProjectPage]) {hooks.states=0;hooks.project=page === ProjectPage; const tree=nodes(page());
-      expect(tree.filter(n=>n.type === SourceImagePanel)).toHaveLength(1);
+      // One ordinary text panel and one limited-fidelity panel per page; never two of the same mode.
+      const panels=tree.filter(n=>n.type === SourceImagePanel);
+      expect(panels.filter(n=>(n.props.mode ?? "text_generate") === "text_generate")).toHaveLength(1);
+      expect(panels.filter(n=>n.props.mode === "background_plate_lock")).toHaveLength(1);
       const legacy=tree.filter(n=>n.type === TextImagePanel); expect(legacy).toHaveLength(1); expect(legacy[0].props.historyOnly).toBe(true);
     }
   });
@@ -39,7 +42,7 @@
   it("new personal project is an explicit separate 1024-square local action and double click coalesces",async()=>{
     const calls:Array<[string,RequestInit|undefined]>=[];
     vi.stubGlobal("fetch",async(u:string,i?:RequestInit)=>{calls.push([u,i]); if(u === "/api/auth/session") return Response.json({principal:{user_id:"usr_a",account_id:"acct_a"}});
-      if(u.includes("capabilities")) return Response.json({modes:[{mode:"text_generate",can_quote:false,can_confirm:false,ready:false,disabled:true,reason:"source_unconfigured",model:"qwen-image-2.0",size:"1024*1024",fidelity:"not_applicable",visual_quality:"unknown"},{mode:"reference_edit",ready:false,disabled:true,reason:"formal_contract_unconfigured"},{mode:"background_plate_lock",ready:false,disabled:true,reason:"formal_contract_unconfigured"}],historical_read:true,automatic_output_recovery_ready:false});
+      if(u.includes("capabilities")) return Response.json({modes:[{mode:"text_generate",can_quote:false,can_confirm:false,ready:false,disabled:true,reason:"source_unconfigured",model:"qwen-image-2.0",size:"1024*1024",fidelity:"not_applicable",visual_quality:"unknown"},{mode:"reference_edit",ready:false,disabled:true,reason:"formal_contract_unconfigured"},{mode:"background_plate_lock",ready:false,disabled:true,can_quote:false,can_confirm:false,reason:"plate_lock_disabled",size:"1024*1024",model:"qwen-image-2.0",claim:"frozen_synthetic_sample_only",fidelity:"frozen_sample_only",visual_quality:"unknown",supported_input_ids:[],supported_inputs:[]}],historical_read:true,automatic_output_recovery_ready:false});
       if(u.includes("source-image-runs")) return Response.json({runs:[],next_cursor:""});
       return Response.json({project:{id:"proj_a"}});
     });
PATCH2
git apply --whitespace=nowarn <<'PATCH3'
--- a/web/tests/bg-replace.test.ts
+++ b/web/tests/bg-replace.test.ts
@@ -78,7 +78,9 @@
     expect(panel).not.toContain("计费已通过");
     expect(panel).not.toContain("生产出图已经通过");
     expect(panel).toContain("锁定主体并换背景");
-    expect(panel).toContain("用模型生成背景并锁定主体");
+    // The direct model-plate trigger is retired: it bypassed priced Task execution.
+    expect(panel).not.toContain("用模型生成背景并锁定主体");
+    expect(panel).not.toContain("/model-plate");
     expect(panel).toContain("subjectLockPreview");
     expect(panel).toContain("modelPlatePreview");
     expect(panel).toContain("/content");
PATCH3
````

- [ ] **Step 2: 运行，确认失败**

```bash
cd "$WT/web" && npx vitest run 2>&1 | grep -E "Test Files|Tests |FAIL|Error" | head -12
```

期望：新增的 4 个测试文件与被更新的 3 个既有测试 `FAIL`（模块 `@/lib/plate-lock` 不存在、`quotePlate is not a function`、`reference to 'returnOutput'`…）；其余既有文件仍通过。

- [ ] **Step 3: 写实现——数据层与 BFF**

`source-image.ts`（类型：`SourceModeName`、`PlateView`、`PlateCreate`；`decodePlate` 与 `decodeSourceRun` 的按模式严格解码——plate run 的 `output` 必须为 null、`fidelity` 按模式、`plate` 必须有 / 不得有；能力解码；控制器的 `begin` / `quotePlate` / `returnToSource` / 模式感知 `remember`；`pendingHintKey(scope, mode)`；`validBackground`）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/web/src/lib/source-image.ts
+++ b/web/src/lib/source-image.ts
@@ -3,11 +3,27 @@
 export type SourceQuote = { quote_id: string; quote_fingerprint: string; usage_id: string; currency: string; quantity: string; unit_price_minor: string; amount_minor: string; quoted_at_unix: number; expires_at_unix: number; pricing_version: string };
 export type SourcePayment = { currency: string; payer_source: string; payer_label: string; payer_account_id: string; payer_user_id: string; status: string; charged_minor: string | null; original_charged_minor: string | null; refund_amount_minor: string | null; refresh_status: string; observation_time_known: boolean; last_verified_at: number | null; hold_id?: string; charge_id?: string; release_id?: string; refund_id?: string; original_charge_id?: string };
 export type SourceOutput = { asset_id: string; reference_id: string; sha256: string; content_type: string; size_bytes: string; width_px: number; height_px: number; association_verified: boolean };
-export type SourceRunView = { run_id: string; project_id: string; request_key: string; owner_version: string; mode: string; model: string; provider: string; size: string; quantity: string; phase: string; task_id: string; task_phase?: string; task_status?: string; selected: boolean; deleted: boolean; cancel_requested: boolean; confirmed: boolean; payment: SourcePayment; quote: SourceQuote | null; output: SourceOutput | null; snapshot_updated_at: number; fidelity: string; visual_quality: string; human_adoption: string; automatic_output_recovery_ready: boolean; content_availability: string };
-export type SourceMode = { mode: string; ready: boolean; disabled: boolean; reason: string; can_quote?: boolean; can_confirm?: boolean; authorization_reason?: string; runtime_reason?: string; model?: string; size?: string; fidelity?: string; visual_quality?: string };
+export type SourceModeName = "text_generate" | "background_plate_lock";
+export type PlateAxisState = "PASS" | "FAIL" | "UNKNOWN" | "N/A";
+export type PlateAxis = { name: string; state: PlateAxisState; machine_checked: boolean; evidence: string };
+export type PlateCandidate = "limited_candidate" | "pending" | "not_usable_candidate" | "blocked";
+export type PlateView = {
+  result_kind: "derived_composite"; claim: "frozen_synthetic_sample_only"; human_usefulness: "NOT_RUN";
+  derivation_state: "pending" | "derived" | "blocked" | "invalid"; candidate_state: PlateCandidate;
+  sample_set_id?: string; case_id?: string; background_intent?: string; canvas?: { width_px: number; height_px: number }; blocked_reason?: string;
+  plate_task?: { task_id: string; asset_id: string; reference_id: string; sha256: string };
+  verdicts?: { fidelity: string; generation: string; billing: string; human_usefulness: "NOT_RUN" };
+  axes?: PlateAxis[]; limits?: string[]; report_sha256?: string; composite_sha256?: string;
+  result?: { content_type: "image/png"; size_bytes: string; width_px: number; height_px: number; sha256: string };
+};
+export type SourceRunView = { run_id: string; project_id: string; request_key: string; owner_version: string; mode: SourceModeName; model: string; provider: string; size: string; quantity: string; phase: string; task_id: string; task_phase?: string; task_status?: string; selected: boolean; deleted: boolean; cancel_requested: boolean; confirmed: boolean; payment: SourcePayment; quote: SourceQuote | null; output: SourceOutput | null; snapshot_updated_at: number; fidelity: "not_applicable" | "frozen_sample_only"; visual_quality: string; human_adoption: string; automatic_output_recovery_ready: boolean; content_availability: string; plate?: PlateView };
+export type PlateSupportedInput = { input_id: string; kind: string; canvas: { width_px: number; height_px: number } };
+export type SourceMode = { mode: string; ready: boolean; disabled: boolean; reason: string; can_quote?: boolean; can_confirm?: boolean; authorization_reason?: string; runtime_reason?: string; model?: string; size?: string; fidelity?: string; visual_quality?: string; claim?: string; supported_input_ids?: string[]; supported_inputs?: PlateSupportedInput[] };
 export type SourceCapabilities = { modes: SourceMode[]; historical_read: boolean; automatic_output_recovery_ready: boolean };
 export type SourceList = { runs: SourceRunView[]; next_cursor: string };
-export type SourceCreate = { request_key: string; mode: "text_generate"; prompt: string; size: "1024*1024" };
+export type TextCreate = { request_key: string; mode: "text_generate"; prompt: string; size: "1024*1024" };
+export type PlateCreate = { request_key: string; mode: "background_plate_lock"; input_id: string; background_intent: string };
+export type SourceCreate = TextCreate | PlateCreate;
 export type SourceConfirm = { quote_id: string; quote_fingerprint: string };
 export interface SourceImageAPI {
   capabilities(project: string): Promise<SourceCapabilities>;
@@ -20,6 +36,7 @@
   cancel(project: string, run: string): Promise<SourceRunView>;
   select(project: string, run: string): Promise<SourceRunView>;
   deleteOutput(project: string, run: string): Promise<SourceRunView>;
+  returnOutput(project: string, run: string): Promise<{ output_id: string }>;
 }
 export class SourceHTTPError extends Error { constructor(public status: number, public code: string) { super(code); } }
 const MAX_MINOR = 9223372036854775807n;
@@ -76,14 +93,42 @@
   if(result.amount_minor !== result.unit_price_minor || result.amount_minor === "0" || result.expires_at_unix-result.quoted_at_unix !== 300) throw new Error("invalid_source_response");
   return result;
 }
+const arrayOf = <T,>(v: unknown, max: number, each: (x: unknown) => T): T[] => { if (!Array.isArray(v) || v.length > max) throw new Error("invalid_source_response"); return v.map(each); };
+const AXIS_STATES: PlateAxisState[] = ["PASS", "FAIL", "UNKNOWN", "N/A"];
+const CANDIDATES: PlateCandidate[] = ["limited_candidate", "pending", "not_usable_candidate", "blocked"];
+export function decodePlate(v: unknown): PlateView {
+  const p = object(v);
+  const out: PlateView = {
+    result_kind: member(p.result_kind, ["derived_composite"]) as "derived_composite", claim: member(p.claim, ["frozen_synthetic_sample_only"]) as "frozen_synthetic_sample_only",
+    human_usefulness: member(p.human_usefulness, ["NOT_RUN"]) as "NOT_RUN", derivation_state: member(p.derivation_state, ["pending", "derived", "blocked", "invalid"]) as PlateView["derivation_state"],
+    candidate_state: member(p.candidate_state, CANDIDATES) as PlateCandidate,
+  };
+  if (p.sample_set_id !== undefined) out.sample_set_id = id(p.sample_set_id);
+  if (p.case_id !== undefined) out.case_id = text(p.case_id, 64);
+  if (p.background_intent !== undefined) out.background_intent = text(p.background_intent, 800);
+  if (p.blocked_reason !== undefined) out.blocked_reason = id(p.blocked_reason);
+  if (p.canvas !== undefined) { const c = object(p.canvas); out.canvas = { width_px: integer(c.width_px), height_px: integer(c.height_px) }; }
+  if (p.plate_task !== undefined) { const t = object(p.plate_task); out.plate_task = { task_id: id(t.task_id, true), asset_id: id(t.asset_id), reference_id: id(t.reference_id), sha256: text(t.sha256, 64) }; }
+  if (p.verdicts !== undefined) { const d = object(p.verdicts); out.verdicts = { fidelity: member(d.fidelity, ["PASS", "FAIL", "UNKNOWN"]), generation: member(d.generation, ["PASS", "FAIL", "UNKNOWN"]), billing: member(d.billing, ["PASS", "FAIL", "UNKNOWN"]), human_usefulness: member(d.human_usefulness, ["NOT_RUN"]) as "NOT_RUN" }; }
+  if (p.axes !== undefined) out.axes = arrayOf(p.axes, 24, x => { const a = object(x); return { name: id(a.name), state: member(a.state, AXIS_STATES) as PlateAxisState, machine_checked: bool(a.machine_checked), evidence: text(a.evidence, 512) }; });
+  if (p.limits !== undefined) out.limits = arrayOf(p.limits, 12, x => text(x, 512));
+  if (p.report_sha256 !== undefined) out.report_sha256 = text(p.report_sha256, 64);
+  if (p.composite_sha256 !== undefined) out.composite_sha256 = text(p.composite_sha256, 64);
+  if (p.result !== undefined) { const r = object(p.result); out.result = { content_type: member(r.content_type, ["image/png"]) as "image/png", size_bytes: minor(r.size_bytes), width_px: integer(r.width_px), height_px: integer(r.height_px), sha256: text(r.sha256, 64) }; }
+  if (out.candidate_state === "limited_candidate" && (out.derivation_state !== "derived" || !out.verdicts || !out.axes || !out.limits || out.limits.length === 0 || !out.result)) throw new Error("invalid_source_response");
+  return out;
+}
 export function decodeSourceRun(v: unknown): SourceRunView {
   const r=object(v), p=object(r.payment);
   const payment: SourcePayment = { currency:member(p.currency,["CNY"]), payer_source:member(p.payer_source,["personal","delegation"]), payer_label:member(p.payer_label,["个人账户","组织委托账户"]), payer_account_id:id(p.payer_account_id), payer_user_id:id(p.payer_user_id), status:member(p.status,["unknown","ingested","quoted","held","charged","released","refunded","reconciling"]), charged_minor:nullableMinor(p.charged_minor), original_charged_minor:nullableMinor(p.original_charged_minor), refund_amount_minor:nullableMinor(p.refund_amount_minor), refresh_status:member(p.refresh_status,["unknown","not_refreshed"]), observation_time_known:bool(p.observation_time_known), last_verified_at:p.last_verified_at === null ? null : integer(p.last_verified_at) };
   for (const k of ["hold_id","charge_id","release_id","refund_id","original_charge_id"] as const) if(p[k] !== undefined) payment[k]=id(p[k],true);
+  const modeName=member(r.mode,["text_generate","background_plate_lock"]) as SourceModeName;
   let output: SourceOutput | null=null;
+  if(modeName === "background_plate_lock" && r.output !== null) throw new Error("invalid_source_response");
   if(r.output !== null) { const o=object(r.output); output={ asset_id:id(o.asset_id), reference_id:id(o.reference_id), sha256:text(o.sha256), content_type:member(o.content_type,["image/png"]), size_bytes:minor(o.size_bytes), width_px:integer(o.width_px), height_px:integer(o.height_px), association_verified:bool(o.association_verified) }; if(!/^[0-9a-f]{64}$/.test(output.sha256) || output.width_px !== 1024 || output.height_px !== 1024 || !output.association_verified || BigInt(output.size_bytes) < 1n || BigInt(output.size_bytes)>16777216n) throw new Error("invalid_source_response"); }
-  const result: SourceRunView={ run_id:id(r.run_id), project_id:id(r.project_id), request_key:text(r.request_key), owner_version:member(r.owner_version,["product_source_v1"]), mode:member(r.mode,["text_generate"]), model:member(r.model,["qwen-image-2.0"]), provider:member(r.provider,["modelxing-qwen-image-2.0-v1"]), size:member(r.size,["1024*1024"]), quantity:member(r.quantity,["1"]), phase:member(r.phase,["intent","usage_pending","quote_pending","quoted","quote_expired","confirmed","task_submit_pending","task_linked","output_ready","settlement_pending","succeeded","not_dispatched","cancel_pending","provider_unknown","asset_pending","settlement_unknown","review_required","canceled","cancelled","failed","output_deleted"]), task_id:id(r.task_id,true), selected:bool(r.selected), deleted:bool(r.deleted), cancel_requested:bool(r.cancel_requested), confirmed:bool(r.confirmed), payment, quote:r.quote === null ? null : decodeQuote(r.quote), output, snapshot_updated_at:integer(r.snapshot_updated_at), fidelity:member(r.fidelity,["not_applicable"]), visual_quality:member(r.visual_quality,["unknown"]), human_adoption:member(r.human_adoption,["NOT_RUN"]), automatic_output_recovery_ready:bool(r.automatic_output_recovery_ready), content_availability:member(r.content_availability,["not_checked"]) };
+  const result: SourceRunView={ run_id:id(r.run_id), project_id:id(r.project_id), request_key:text(r.request_key), owner_version:member(r.owner_version,["product_source_v1"]), mode:modeName, model:member(r.model,["qwen-image-2.0"]), provider:member(r.provider,["modelxing-qwen-image-2.0-v1"]), size:member(r.size,["1024*1024"]), quantity:member(r.quantity,["1"]), phase:member(r.phase,["intent","usage_pending","quote_pending","quoted","quote_expired","confirmed","task_submit_pending","task_linked","output_ready","settlement_pending","succeeded","not_dispatched","cancel_pending","provider_unknown","asset_pending","settlement_unknown","review_required","canceled","cancelled","failed","output_deleted"]), task_id:id(r.task_id,true), selected:bool(r.selected), deleted:bool(r.deleted), cancel_requested:bool(r.cancel_requested), confirmed:bool(r.confirmed), payment, quote:r.quote === null ? null : decodeQuote(r.quote), output, snapshot_updated_at:integer(r.snapshot_updated_at), fidelity:member(r.fidelity,modeName === "text_generate" ? ["not_applicable"] : ["frozen_sample_only"]) as SourceRunView["fidelity"], visual_quality:member(r.visual_quality,["unknown"]), human_adoption:member(r.human_adoption,["NOT_RUN"]), automatic_output_recovery_ready:bool(r.automatic_output_recovery_ready), content_availability:member(r.content_availability,["not_checked"]) };
   if (!result.run_id.startsWith("sir_") || result.deleted && (result.output !== null || result.selected)) throw new Error("invalid_source_response");
+  if(modeName === "background_plate_lock") result.plate=decodePlate(r.plate); else if(r.plate !== undefined) throw new Error("invalid_source_response");
   if(r.task_phase !== undefined) result.task_phase=id(r.task_phase,true); if(r.task_status !== undefined) result.task_status=id(r.task_status,true);
   return result;
 }
@@ -93,6 +138,11 @@
     for(const k of ["can_quote","can_confirm"] as const) if(m[k] !== undefined) out[k]=bool(m[k]);
     for(const k of ["authorization_reason","runtime_reason"] as const) if(m[k] !== undefined) out[k]=id(m[k],true);
     if(out.mode === "text_generate") { out.model=member(m.model,["qwen-image-2.0"]); out.size=member(m.size,["1024*1024"]); out.fidelity=member(m.fidelity,["not_applicable"]); out.visual_quality=member(m.visual_quality,["unknown"]); }
+    if(out.mode === "background_plate_lock") {
+      out.model=member(m.model,["qwen-image-2.0"]); out.size=member(m.size,["1024*1024"]); out.fidelity=member(m.fidelity,["frozen_sample_only"]); out.visual_quality=member(m.visual_quality,["unknown"]); out.claim=member(m.claim,["frozen_synthetic_sample_only"]);
+      out.supported_input_ids=arrayOf(m.supported_input_ids,100,x=>id(x));
+      out.supported_inputs=arrayOf(m.supported_inputs,100,x=>{ const i=object(x),c=object(i.canvas); return { input_id:id(i.input_id), kind:member(i.kind,["carton","handled_metal","glass_bottle"]), canvas:{width_px:integer(c.width_px),height_px:integer(c.height_px)} }; });
+    }
     return out;
   });
   if(new Set(modes.map(m=>m.mode)).size !== 3) throw new Error("invalid_source_response");
@@ -107,7 +157,7 @@
 }
 export function decodeSourceList(v: unknown): SourceList { const d=object(v); if(!Array.isArray(d.runs) || d.runs.length>100) throw new Error("invalid_source_response"); const next=text(d.next_cursor,1024); if(next) validateCursor(next); return { runs:d.runs.map(decodeSourceRun), next_cursor:next }; }
 export function canConfirm(v: SourceRunView, nowUnix: number): boolean {
-  try { const q=decodeQuote(v.quote); return v.phase === "quoted" && !v.confirmed && !v.deleted && !v.cancel_requested && v.mode === "text_generate" && v.size === "1024*1024" && v.payment.status === "quoted" && q.quoted_at_unix <= nowUnix && nowUnix < q.expires_at_unix; } catch { return false; }
+  try { const q=decodeQuote(v.quote); return v.phase === "quoted" && !v.confirmed && !v.deleted && !v.cancel_requested && (v.mode === "text_generate" || v.mode === "background_plate_lock") && v.size === "1024*1024" && v.payment.status === "quoted" && q.quoted_at_unix <= nowUnix && nowUnix < q.expires_at_unix; } catch { return false; }
 }
 export function requestState(v: SourceRunView): string {
   if(v.deleted) return "输出已隐藏，引用清理待核实（不代表退款）";
@@ -123,12 +173,13 @@
   if(v.phase === "cancel_pending") return "取消待核实，不宣称已释放费用";
   return "等待结果，保留原请求";
 }
-export function sourceWritesDenied(c: SourceCapabilities | null): boolean {
-  const mode=c?.modes.find(m=>m.mode === "text_generate");
+export function sourceWritesDenied(c: SourceCapabilities | null, name: SourceModeName="text_generate"): boolean {
+  const mode=c?.modes.find(m=>m.mode === name);
   return !mode || mode.authorization_reason === "project_write_forbidden" || mode.reason === "project_archived";
 }
-export const pendingHintKey = (s: SourceScope): string => "source-image-pending:" + JSON.stringify([s.userID,s.accountID,s.projectID]);
-export function sourcePath(project: string, run?: string, action?: "content" | "export"): string {
+export function validBackground(v: string): boolean { return v.length>0 && v === v.trim() && Array.from(v).length<=200 && !/[\u0000-\u001f\u007f]/.test(v); }
+export const pendingHintKey = (s: SourceScope, mode: SourceModeName="text_generate"): string => (mode === "text_generate" ? "source-image-pending:" : "plate-lock-pending:") + JSON.stringify([s.userID,s.accountID,s.projectID]);
+export function sourcePath(project: string, run?: string, action?: "content" | "export" | "return"): string {
   if(!sourceSegment(project) || run !== undefined && !sourceSegment(run)) throw new Error("invalid_source_path");
   return `/api/projects/${encodeURIComponent(project)}/source-image-runs${run ? "/"+encodeURIComponent(run) : ""}${action ? "/"+action : ""}`;
 }
@@ -151,6 +202,7 @@
   cancel:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/cancel","POST",{})),
   select:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/select","POST",{})),
   deleteOutput:async(p,r)=>decodeSourceRun(await sourceFetch(sourcePath(p,r)+"/output","DELETE",{})),
+  returnOutput:async(p,r)=>{ const d=object(await sourceFetch(sourcePath(p,r)+"/return","POST",{})); const o=object(d.output); return { output_id:id(o.id) }; },
 };
 export class SourceImageController {
   view: SourceRunView | null=null;
@@ -160,24 +212,40 @@
   private pending: { hint: SourcePendingHint; input: SourceCreate | null; attempted: boolean } | null=null;
   private active: Promise<void> | null=null;
   private shownQuote="";
-  constructor(private api: SourceImageAPI, private uuid: () => string=()=>crypto.randomUUID(), private now: () => number=()=>Math.floor(Date.now()/1000)) {}
+  constructor(private api: SourceImageAPI, private uuid: () => string=()=>crypto.randomUUID(), private now: () => number=()=>Math.floor(Date.now()/1000), private mode: SourceModeName="text_generate") {}
   get pendingHint(): SourcePendingHint | null { return this.pending ? { ...this.pending.hint } : null; }
   setScope(s: SourceScope | null): void {
     if(s && (!sourceSegment(s.userID) || !sourceSegment(s.accountID) || !sourceSegment(s.projectID))) throw new Error("invalid_source_scope");
     if(JSON.stringify(s) !== JSON.stringify(this.scope)) { this.epoch++; this.view=null; this.capabilities=null; this.pending=null; this.active=null; this.shownQuote=""; this.scope=s ? {...s} : null; }
   }
   private current(): SourceScope { if(!this.scope) throw new Error("未验证当前登录身份与工程"); return this.scope; }
-  private remember(v: SourceRunView, epoch: number): void { const s=this.current(); if(epoch !== this.epoch || v.project_id !== s.projectID || v.payment.payer_user_id !== s.userID || v.payment.payer_source === "personal" && v.payment.payer_account_id !== s.accountID) throw new Error("登录范围已改变，请重新读取"); this.view=decodeSourceRun(v); this.shownQuote=JSON.stringify(this.view.quote); }
+  private remember(v: SourceRunView, epoch: number): void { const s=this.current(); if(v.mode !== this.mode) throw new Error("请求类型不一致，请重新读取"); if(epoch !== this.epoch || v.project_id !== s.projectID || v.payment.payer_user_id !== s.userID || v.payment.payer_source === "personal" && v.payment.payer_account_id !== s.accountID) throw new Error("登录范围已改变，请重新读取"); this.view=decodeSourceRun(v); this.shownQuote=JSON.stringify(this.view.quote); }
   private rememberOriginal(next: SourceRunView, previous: SourceRunView, epoch: number): void {
     if(next.run_id !== previous.run_id || next.request_key !== previous.request_key || previous.quote !== null && JSON.stringify(next.quote) !== JSON.stringify(previous.quote) || previous.task_id && next.task_id !== previous.task_id) throw new Error("原请求事实不一致");
     this.remember(next,epoch);
   }
   private coalesce(work: () => Promise<void>): Promise<void> { if(this.active) return this.active; const result=work(); this.active=result; void result.finally(()=>{if(this.active === result) this.active=null;}).catch(()=>{}); return result; }
-  quote(input: { projectID: string; prompt: string; size: "1024*1024" }): Promise<void> { return this.coalesce(async()=>{
-    const s=this.current(), epoch=this.epoch; if(input.projectID !== s.projectID || this.capabilities?.modes.find(m=>m.mode === "text_generate")?.can_quote !== true) throw new Error("当前工程不允许获取正式报价");
-    const prompt=input.prompt.trim(); if(!prompt || new TextEncoder().encode(prompt).length>16384 || input.size !== "1024*1024") throw new Error("文字描述不合法");
+  quote(input: { projectID: string; prompt: string; size: "1024*1024" }): Promise<void> {
+    return this.begin(input.projectID, "text_generate", () => {
+      const prompt=input.prompt.trim(); if(!prompt || new TextEncoder().encode(prompt).length>16384 || input.size !== "1024*1024") throw new Error("文字描述不合法");
+      return (requestKey: string): SourceCreate => ({ request_key:requestKey, mode:"text_generate", prompt, size:"1024*1024" });
+    });
+  }
+  // The only things a browser contributes to a limited-fidelity run are which of
+  // its own registered photos and the words describing the background.
+  quotePlate(input: { projectID: string; inputID: string; background: string }): Promise<void> {
+    return this.begin(input.projectID, "background_plate_lock", () => {
+      const background=input.background.trim(), plate=this.capabilities?.modes.find(m=>m.mode === "background_plate_lock");
+      if(!validBackground(background)) throw new Error("背景描述需为 1 到 200 个字，且不含换行");
+      if(!sourceSegment(input.inputID) || !plate?.supported_input_ids?.includes(input.inputID)) throw new Error("这张照片不在已验证范围内");
+      return (requestKey: string): SourceCreate => ({ request_key:requestKey, mode:"background_plate_lock", input_id:input.inputID, background_intent:background });
+    });
+  }
+  private begin(projectID: string, modeName: SourceModeName, prepare: () => (requestKey: string) => SourceCreate): Promise<void> { return this.coalesce(async()=>{
+    const s=this.current(), epoch=this.epoch; if(projectID !== s.projectID || this.capabilities?.modes.find(m=>m.mode === modeName)?.can_quote !== true) throw new Error("当前工程不允许获取正式报价");
+    const make=prepare();
     if(this.view && !this.pending) throw new Error("已有原请求，请显式重新生成");
-    const body: SourceCreate={ request_key:this.pending?.hint.requestKey ?? this.uuid(),mode:"text_generate",prompt,size:"1024*1024" };
+    const body: SourceCreate=make(this.pending?.hint.requestKey ?? this.uuid());
     if(!sourceSegment(body.request_key)) throw new Error("请求键不合法");
     if(this.pending?.input && JSON.stringify(body) !== JSON.stringify(this.pending.input)) throw new Error("原请求内容不一致，请保留原请求或显式重新生成");
     if(!this.pending) this.pending={ hint:{...s,requestKey:body.request_key},input:body,attempted:false };
@@ -186,7 +254,7 @@
     const v=await this.api.create(s.projectID,body); if(v.request_key !== body.request_key) throw new Error("原请求事实不一致"); this.remember(v,epoch);
   }); }
   confirm(): Promise<void> { return this.coalesce(async()=>{ const s=this.current(), v=this.view, epoch=this.epoch;
-    if(!v || this.capabilities?.modes.find(m=>m.mode === "text_generate")?.can_confirm !== true || !canConfirm(v,this.now()) || this.shownQuote !== JSON.stringify(v.quote)) throw new Error("原报价未验证、已过期或已改变");
+    if(!v || this.capabilities?.modes.find(m=>m.mode === v.mode)?.can_confirm !== true || !canConfirm(v,this.now()) || this.shownQuote !== JSON.stringify(v.quote)) throw new Error("原报价未验证、已过期或已改变");
     this.rememberOriginal(await this.api.confirm(s.projectID,v.run_id,{quote_id:v.quote!.quote_id,quote_fingerprint:v.quote!.quote_fingerprint}),v,epoch);
   }); }
   async loadCapabilities(): Promise<void> { const s=this.current(), epoch=this.epoch; const c=await this.api.capabilities(s.projectID); if(epoch !== this.epoch) throw new Error("登录范围已改变"); this.capabilities=c; }
@@ -196,6 +264,9 @@
     if(v.userID !== s.userID || v.accountID !== s.accountID || v.projectID !== s.projectID || !sourceSegment(v.requestKey)) return;
     this.pending={hint:{...s,requestKey:v.requestKey},input:null,attempted:true}; const r=await this.api.find(s.projectID,v.requestKey); if(epoch !== this.epoch) throw new Error("登录范围已改变"); if(r) {if(r.request_key !== v.requestKey) throw new Error("原请求键不一致");this.remember(r,epoch);}
   }
-  action(name: "reconcile" | "cancel" | "select" | "deleteOutput"): Promise<void> { return this.coalesce(async()=>{ const s=this.current(), epoch=this.epoch, v=this.view; if(!v || sourceWritesDenied(this.capabilities)) throw new Error("当前工程不允许写操作"); this.rememberOriginal(await this.api[name](s.projectID,v.run_id),v,epoch); }); }
+  action(name: "reconcile" | "cancel" | "select" | "deleteOutput"): Promise<void> { return this.coalesce(async()=>{ const s=this.current(), epoch=this.epoch, v=this.view; if(!v || sourceWritesDenied(this.capabilities,v.mode)) throw new Error("当前工程不允许写操作"); this.rememberOriginal(await this.api[name](s.projectID,v.run_id),v,epoch); }); }
+  // Registers the selected, limited-candidate composite for return to an order or
+  // campaign. The server re-checks quality, selection and the original charge.
+  async returnToSource(): Promise<string> { let outputID=""; await this.coalesce(async()=>{ const s=this.current(), v=this.view; if(!v || v.mode !== "background_plate_lock" || sourceWritesDenied(this.capabilities,v.mode) || !v.selected || v.plate?.candidate_state !== "limited_candidate") throw new Error("只有已选定的限定候选才能回传"); outputID=(await this.api.returnOutput(s.projectID,v.run_id)).output_id; }); return outputID; }
   newRequest(): void { if(this.active) throw new Error("原动作尚未返回"); this.view=null; this.pending=null; this.shownQuote=""; }
 }
PATCH1
````

BFF（`proxy.ts`：plate 封闭请求体与校验、`return` 动作、稳定错误码按各自状态透出且**用上游真实状态码比对**、`422` 通道；背景替换 BFF 删除 `model-plate`）：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/web/src/app/api/projects/[id]/source-image-runs/proxy.ts
+++ b/web/src/app/api/projects/[id]/source-image-runs/proxy.ts
@@ -1,8 +1,12 @@
 import { SERVER_BASE, ACCESS_COOKIE, serverHeaders } from "@/lib/server";
-import { decodeSourceRun, decodeSourceCapabilities, decodeSourceList, parseSourceJSON, sourceSegment, isMinor } from "@/lib/source-image";
+import { decodeSourceRun, decodeSourceCapabilities, decodeSourceList, parseSourceJSON, sourceSegment, isMinor, validBackground } from "@/lib/source-image";
 
 const headers={ "Cache-Control":"private, no-store", "X-Content-Type-Options":"nosniff" };
-const messages: Record<string,string>={ unauthenticated:"请重新登录", forbidden:"当前工程不允许此操作", not_found:"资源不存在", invalid_request:"请求不合法", conflict:"原请求事实冲突，请重新读取", source_unconfigured:"正式生成尚未配置", source_unavailable:"正式服务暂不可用，原请求状态待核实", method_not_allowed:"此资源不接受该方法" };
+const messages: Record<string,string>={ unauthenticated:"请重新登录", forbidden:"当前工程不允许此操作", not_found:"资源不存在", invalid_request:"请求不合法", conflict:"原请求事实冲突，请重新读取", source_unconfigured:"正式生成尚未配置", source_unavailable:"正式服务暂不可用，原请求状态待核实", method_not_allowed:"此资源不接受该方法",
+  sample_not_frozen:"这张照片不在已验证范围内", candidate_not_usable:"检查未通过的结果不能选用", not_selected:"请先选定这张结果", no_source_binding:"这个工程没有来源订单或活动，不能回传", no_composite:"没有可显示的结果", original_missing:"原图缺失，无法核对",
+  plate_lock_disabled:"限定保真换背景尚未开放", sample_set_unconfigured:"已验证样本尚未配置", sample_set_invalid:"已验证样本校验未通过", plate_org_not_supported:"组织付款暂不支持这个模式" };
+// Stable limited-fidelity reason codes the browser may see, each only with its own status.
+const PLATE_CODES: Record<string,number>={ sample_not_frozen:422, candidate_not_usable:409, not_selected:409, no_source_binding:409, no_composite:409, original_missing:409, plate_lock_disabled:503, sample_set_unconfigured:503, sample_set_invalid:503, plate_org_not_supported:503 };
 class Rejected extends Error { constructor(public status: number, public code: string) { super(code); } }
 const reject=(status: number, code: string): never => { throw new Rejected(status,code); };
 function errorResponse(status: number, code: string, run?: string): Response { return Response.json({error:{code,message:messages[code]},...(run ? {run_id:run} : {})},{status,headers}); }
@@ -51,9 +55,11 @@
   if(req.method === "GET") { if(bytes.length) reject(400,"invalid_request"); return undefined; }
   if(bytes.length && req.headers.get("content-type")?.split(";",1)[0].trim().toLowerCase() !== "application/json") reject(400,"invalid_request");
   let raw: string; let b: Record<string,unknown>; try { raw=new TextDecoder("utf-8",{fatal:true}).decode(bytes); b=record(parseSourceJSON(raw || "{}")); } catch {return reject(400,"invalid_request");}
-  const allowed=resource === "list" ? ["request_key","mode","prompt","size"] : action === "confirm" ? ["quote_id","quote_fingerprint"] : [];
+  const plate=resource === "list" && b.mode === "background_plate_lock";
+  const allowed=resource === "list" ? (plate ? ["request_key","mode","input_id","background_intent"] : ["request_key","mode","prompt","size"]) : action === "confirm" ? ["quote_id","quote_fingerprint"] : [];
   fields(b,allowed); if(allowed.some(k=>typeof b[k] !== "string")) reject(400,"invalid_request");
-  if(resource === "list" && (!cleanKey(b.request_key) || b.mode !== "text_generate" || b.size !== "1024*1024" || typeof b.prompt !== "string" || !b.prompt.trim() || new TextEncoder().encode(b.prompt).length>16384)) reject(400,"invalid_request");
+  if(plate) { if(!cleanKey(b.request_key) || !sourceSegment(b.input_id) || !validBackground(b.background_intent as string)) reject(400,"invalid_request"); }
+  else if(resource === "list" && (!cleanKey(b.request_key) || b.mode !== "text_generate" || b.size !== "1024*1024" || typeof b.prompt !== "string" || !b.prompt.trim() || new TextEncoder().encode(b.prompt).length>16384)) reject(400,"invalid_request");
   if(action === "confirm" && (!sourceSegment(b.quote_id) || typeof b.quote_fingerprint !== "string" || !/^[0-9a-f]{64}$/.test(b.quote_fingerprint))) reject(400,"invalid_request");
   return raw || undefined; // No parse/re-serialize or fabricated {} body.
 }
@@ -65,7 +71,7 @@
   try {
     const url=new URL(req.url);
     if(!sourceSegment(p.id) || p.runId !== undefined && (!sourceSegment(p.runId) || !p.runId.startsWith("sir_"))) reject(400,"invalid_request");
-    const actionMethods: Record<string,string>={confirm:"POST",reconcile:"POST",cancel:"POST",select:"POST",output:"DELETE",content:"GET",export:"GET"};
+    const actionMethods: Record<string,string>={confirm:"POST",reconcile:"POST",cancel:"POST",select:"POST",return:"POST",output:"DELETE",content:"GET",export:"GET"};
     const allowed=resource === "list" ? ["GET","POST"] : resource === "action" ? [actionMethods[p.action ?? ""]].filter(Boolean) : ["GET"];
     if(resource === "action" && !Object.hasOwn(actionMethods,p.action ?? "")) reject(404,"not_found");
     if(!allowed.includes(req.method)) reject(405,"method_not_allowed");
@@ -78,10 +84,11 @@
     if(upstream.status>=300 && upstream.status<400) { await upstream.body?.cancel(); reject(503,"source_unavailable"); }
     if(!upstream.ok) {
       let d: Record<string,unknown>={}; try {d=record(parseSourceJSON(new TextDecoder("utf-8",{fatal:true}).decode(await boundedBytes(upstream.body,1048576,abort.signal))));} catch {}
-      const status=[400,401,403,404,409].includes(upstream.status) ? upstream.status : 503;
+      const status=[400,401,403,404,409,422].includes(upstream.status) ? upstream.status : 503;
       const rawCode=d.error && typeof d.error === "object" ? (d.error as Record<string,unknown>).code : "";
-      const fallback: Record<number,string>={400:"invalid_request",401:"unauthenticated",403:"forbidden",404:"not_found",409:"conflict",503:"source_unavailable"};
-      const code=typeof rawCode === "string" && ["source_unavailable","source_unconfigured"].includes(rawCode) && status === 503 ? rawCode : fallback[status];
+      const fallback: Record<number,string>={400:"invalid_request",401:"unauthenticated",403:"forbidden",404:"not_found",409:"conflict",422:"invalid_request",503:"source_unavailable"};
+      const known=typeof rawCode === "string" && (status === 503 && ["source_unavailable","source_unconfigured"].includes(rawCode) || PLATE_CODES[rawCode] === upstream.status);
+      const code=known ? rawCode as string : fallback[status];
       const run=status !== 404 && sourceSegment(d.run_id) && d.run_id.startsWith("sir_") ? d.run_id : undefined;
       return errorResponse(status,code,run);
     }
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/web/src/app/api/projects/[id]/background-replacements/[jobId]/[action]/route.ts
+++ b/web/src/app/api/projects/[id]/background-replacements/[jobId]/[action]/route.ts
@@ -1,7 +1,7 @@
 import { NextRequest, NextResponse } from "next/server";
 import { accessTokenOf, callServer, toErrorResponse, UpstreamError } from "@/lib/server";
 
-const actions = new Set(["confirm", "submit", "refresh", "quality", "inspect", "select", "export", "lock", "model-plate"]);
+const actions = new Set(["confirm", "submit", "refresh", "quality", "inspect", "select", "export", "lock"]);
 
 type Ctx = { params: Promise<{ id: string; jobId: string; action: string }> };
 
PATCH2
````

- [ ] **Step 4: 写实现——文案、原语、界面**

文案与纯函数（主视图文案集中在这里；`TECHNICAL_ONLY_TERMS` 是测试用的禁词清单）：

`web/src/lib/plate-lock.ts`

````ts
// 限定保真换背景的界面文案与纯函数。这里没有任何网络调用，也没有分数。
// 主视图只用人话；内部编号、哈希、阶段码只允许出现在“技术详情”折叠区。
import type { PlateAxis, PlateAxisState, PlateCandidate, SourceCapabilities, SourceRunView } from "@/lib/source-image";

export const PLATE_TITLE = "限定保真换背景";
export const PLATE_SCOPE_NOTE = "已验证范围：冻结合成样本（纸盒 / 金属把手杯 / 玻璃瓶）。其它照片不在范围内。";
export const PLATE_PROMISE = "商品本身的像素一个都不改，只换背景。";
export const PRICING_NOTE = "费用按生成背景图计。生成后的商品检查未通过时，该结果会标记为不可用，不会自动重做，也不会再次扣费。";
export const RETURN_NOTE = "下游收到的回执不含上述限制说明。";
export const NO_USABLE_PHOTO = "还没有在已验证范围内的商品照片。目前只验证了冻结样本（纸盒 / 金属把手杯 / 玻璃瓶），其它照片不能使用这个模式，也不会产生费用。";
export const STEPS = ["选照片", "写背景", "确认费用", "生成中", "查看结果"] as const;

const STATE_LABEL: Record<PlateAxisState, string> = { PASS: "通过", FAIL: "未通过", UNKNOWN: "待核实", "N/A": "不适用" };
export const stateLabel = (s: PlateAxisState): string => STATE_LABEL[s];

export const AXIS_LABELS: Record<string, string> = {
  mask_pixels_identical: "商品像素与原图完全一致",
  coverage_logo: "Logo 区域保持不变",
  coverage_packaging_text: "包装文字区域保持不变",
  coverage_spec: "规格文字区域保持不变",
  coverage_structure: "商品结构保持不变",
  product_count: "商品数量正确",
  output_size: "输出尺寸正确",
  plate_size: "背景图尺寸正确",
  plate_is_new_background: "背景是新生成的",
  background_changed: "背景相比原图有明显变化",
  plate_task_succeeded: "背景生成已成功",
  plate_asset_hash_verified: "背景图文件已核对",
  plate_charged_observed: "费用已按原报价结算",
  background_extra_objects: "背景里没有多余物体",
  background_intent_followed: "背景符合所写方向",
  visual_composite_quality: "整体观感",
  transmission_edge: "玻璃透射与边缘",
};
export const axisLabel = (name: string): string => AXIS_LABELS[name] ?? "其它检查";

export const REASON_TEXT: Record<string, string> = {
  plate_lock_disabled: "限定保真换背景暂未开放。",
  sample_set_unconfigured: "已验证样本还没有就绪，暂不能使用。",
  sample_set_invalid: "已验证样本没有通过校验，暂不能使用。",
  plate_org_not_supported: "这个模式目前只支持个人付款。",
  project_write_forbidden: "当前账号不能修改这个工程。",
  project_archived: "工程已归档，不能再生成。",
  source_unconfigured: "生成服务暂未就绪。",
  runtime_output_recovery_unconfigured: "生成服务暂未就绪。",
  source_download_hosts_unconfigured: "生成服务暂未就绪。",
};
export const reasonText = (reason: string): string => REASON_TEXT[reason] ?? "暂时不能使用。";

export type Candidate = { label: string; tone: "ok" | "warn" | "bad"; detail: string };
export function candidateSummary(c: PlateCandidate): Candidate {
  switch (c) {
    case "limited_candidate": return { label: "通过商品检查（限定范围）", tone: "ok", detail: "商品像素与原图完全一致。结果只在下面列出的范围内有效。" };
    case "not_usable_candidate": return { label: "未通过商品检查", tone: "bad", detail: "这张图不能选用、导出或回传。费用已按生成的背景图结算，不会自动重做。" };
    case "blocked": return { label: "无法使用", tone: "bad", detail: "这张图所依据的验证样本已变更，不能使用。" };
    default: return { label: "商品检查尚未完成", tone: "warn", detail: "检查完成前不能选用、导出或回传。" };
  }
}

export function plate(v: SourceRunView | null) { return v?.mode === "background_plate_lock" ? v.plate ?? null : null; }
export const charged = (v: SourceRunView): boolean => v.payment.status === "charged";
export const canSelectPlate = (v: SourceRunView): boolean => !!plate(v) && plate(v)!.candidate_state === "limited_candidate" && !v.deleted && charged(v) && !v.selected;
export const canExportPlate = (v: SourceRunView): boolean => !!plate(v) && plate(v)!.candidate_state === "limited_candidate" && !v.deleted && charged(v);
export const canReturnPlate = (v: SourceRunView, returnEnabled: boolean): boolean => returnEnabled && canExportPlate(v) && v.selected;
export const failedAxes = (v: SourceRunView): PlateAxis[] => (plate(v)?.axes ?? []).filter(a => a.machine_checked && a.state === "FAIL");

export function stageOf(v: SourceRunView | null, hasPhoto: boolean, quoteReady: boolean): number {
  if (!v) return hasPhoto ? (quoteReady ? 2 : 1) : 0;
  const p = plate(v);
  if (p && p.derivation_state !== "pending") return 4;
  if (v.confirmed) return 3;
  return 2;
}

export function plateStatusLine(v: SourceRunView | null): string {
  if (!v) return "";
  const p = plate(v);
  if (v.deleted) return "结果已隐藏。";
  if (v.payment.status === "refunded") return "这笔费用已有退款记录，结果不再可用。";
  if (p && p.derivation_state === "derived") return candidateSummary(p.candidate_state).label;
  if (p && p.derivation_state === "blocked") return "结果无法使用。";
  if (v.phase === "quoted") return "报价已生成，确认后才会开始。";
  if (v.phase === "quote_expired") return "报价已过期，请重新获取报价。";
  if (v.phase === "provider_unknown" || v.phase === "settlement_unknown") return "正在核实生成状态，请稍候，不会重复扣费。";
  if (v.phase === "not_dispatched" || v.phase === "canceled" || v.phase === "cancelled") return "这次没有开始生成。";
  if (v.confirmed) return "正在生成背景并检查商品……";
  return "处理中……";
}

// Terms that must never appear in the primary (non-technical) view.
export const TECHNICAL_ONLY_TERMS = ["Task", "Bill", "Hold", "hash", "sha256", "provider", "model", "NOT_RUN", "UNKNOWN", "run_id", "asset_id", "qwen", "usage_id", "quote_id", "Upload"] as const;
````

无障碍原语（Alert：`danger/warn` 为 `role=alert`，`info/ok` 为 `role=status`；Field：显式 `label for` + 提示 / 错误 id；Stepper：有序列表 + `aria-current="step"`；Skeleton：纯装饰 `aria-hidden`）与语义变量层（Painuo Token 尚未进本树，**颜色全部走 `globals.css` 的语义变量**，Token 到位后只改这一处）：

`web/src/components/ui/alert.tsx`

````tsx
import type { HTMLAttributes, ReactNode } from "react";
import styles from "./ui.module.css";

type Tone = "info" | "ok" | "warn" | "danger";

// Alert: danger and warn interrupt (role=alert); info and ok are polite status.
export function Alert({ tone = "info", title, children, ...props }: { tone?: Tone; title?: string; children: ReactNode } & Omit<HTMLAttributes<HTMLDivElement>, "title">) {
  const role = tone === "danger" || tone === "warn" ? "alert" : "status";
  return (
    <div role={role} className={[styles.alert, styles[`alert_${tone}`]].join(" ")} data-tone={tone} {...props}>
      {title ? <strong className={styles.alertTitle}>{title}</strong> : null}
      <div>{children}</div>
    </div>
  );
}
````

`web/src/components/ui/field.tsx`

````tsx
import type { ReactNode } from "react";
import styles from "./ui.module.css";

// Field ties a visible label, optional hint and optional error to one control.
// The control must carry id={htmlFor}; hint and error ids are derived from it.
export function Field({ htmlFor, label, hint, error, children }: { htmlFor: string; label: string; hint?: string; error?: string; children: ReactNode }) {
  return (
    <div className={styles.field}>
      <label htmlFor={htmlFor} className={styles.fieldLabel}>{label}</label>
      {children}
      {hint ? <p id={`${htmlFor}-hint`} className={styles.fieldHint}>{hint}</p> : null}
      {error ? <p id={`${htmlFor}-error`} role="alert" className={styles.fieldError}>{error}</p> : null}
    </div>
  );
}
````

`web/src/components/ui/skeleton.tsx`

````tsx
import type { HTMLAttributes } from "react";
import styles from "./ui.module.css";

// Skeleton is decorative loading filler; the live status text carries meaning.
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div aria-hidden="true" className={[styles.skeleton, className].filter(Boolean).join(" ")} {...props} />;
}
````

`web/src/components/ui/stepper.tsx`

````tsx
import styles from "./ui.module.css";

// Stepper is an ordered list; the current step carries aria-current="step".
export function Stepper({ steps, current, label }: { steps: readonly string[]; current: number; label: string }) {
  return (
    <ol className={styles.stepper} aria-label={label}>
      {steps.map((step, i) => (
        <li key={step} className={[styles.step, i < current ? styles.stepDone : "", i === current ? styles.stepNow : ""].filter(Boolean).join(" ")} aria-current={i === current ? "step" : undefined}>
          <span className={styles.stepNo} aria-hidden="true">{i + 1}</span>
          <span>{step}</span>
          {i < current ? <span className={styles.srOnly}>（已完成）</span> : null}
        </li>
      ))}
    </ol>
  );
}
````

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/web/src/components/ui/ui.module.css
+++ b/web/src/components/ui/ui.module.css
@@ -41,3 +41,44 @@
   border: 1px solid #e4e4e7;
 }
 .warn { background: #fffbeb; color: #92400e; border-color: #fcd34d; }
+
+/* ---- HUI-2232: accessible primitives. Colours come from the semantic
+   variables in globals.css so a token source can replace them in one place. ---- */
+.btn:focus-visible, .field input:focus-visible, .field select:focus-visible, .field textarea:focus-visible, .link:focus-visible, .checkRow input:focus-visible {
+  outline: 3px solid var(--focus);
+  outline-offset: 2px;
+}
+.alert { border: 1px solid var(--border); border-radius: 8px; padding: 10px 12px; margin: 10px 0; display: grid; gap: 4px; line-height: 1.5; }
+.alertTitle { font-size: 14px; }
+.alert_info { background: var(--info-bg); color: var(--info-fg); border-color: var(--info-border); }
+.alert_ok { background: var(--ok-bg); color: var(--ok-fg); border-color: var(--ok-border); }
+.alert_warn { background: var(--warn-bg); color: var(--warn-fg); border-color: var(--warn-border); }
+.alert_danger { background: var(--danger-bg); color: var(--danger-fg); border-color: var(--danger-border); }
+.field { display: grid; gap: 4px; margin: 12px 0; }
+.fieldLabel { font-weight: 600; color: var(--text); margin: 0; }
+.fieldHint { margin: 0; color: var(--muted); font-size: 13px; }
+.fieldError { margin: 0; color: var(--danger-fg); font-size: 13px; }
+.field textarea, .field select, .field input[type="text"] { width: 100%; padding: 10px; border: 1px solid var(--border); border-radius: 8px; font: inherit; background: var(--card); color: var(--text); }
+.skeleton { height: 14px; border-radius: 6px; background: linear-gradient(90deg, var(--border), var(--bg), var(--border)); background-size: 200% 100%; animation: skeleton 1.4s ease-in-out infinite; margin: 8px 0; }
+@keyframes skeleton { from { background-position: 200% 0; } to { background-position: -200% 0; } }
+@media (prefers-reduced-motion: reduce) { .skeleton { animation: none; } }
+.stepper { display: flex; flex-wrap: wrap; gap: 8px 16px; list-style: none; padding: 0; margin: 12px 0; }
+.step { display: inline-flex; align-items: center; gap: 6px; color: var(--muted); font-size: 13px; }
+.stepNo { display: inline-grid; place-items: center; width: 22px; height: 22px; border-radius: 50%; border: 1px solid var(--border); font-size: 12px; background: var(--card); }
+.stepDone { color: var(--ok-fg); }
+.stepDone .stepNo { background: var(--ok-bg); border-color: var(--ok-border); }
+.stepNow { color: var(--text); font-weight: 600; }
+.stepNow .stepNo { background: var(--accent); border-color: var(--accent); color: #fff; }
+.srOnly { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
+.checkRow { display: flex; gap: 8px; align-items: flex-start; margin: 10px 0; }
+.checkRow input { margin-top: 3px; width: 18px; height: 18px; }
+.axisList { list-style: none; padding: 0; margin: 8px 0; display: grid; gap: 6px; }
+.axisList li { display: flex; justify-content: space-between; gap: 12px; border-bottom: 1px dashed var(--border); padding: 4px 0; }
+.stateOK { color: var(--ok-fg); font-weight: 600; }
+.stateBad { color: var(--danger-fg); font-weight: 600; }
+.stateWait { color: var(--warn-fg); font-weight: 600; }
+.stateNA { color: var(--muted); }
+.resultImg { max-width: 100%; height: auto; border: 1px solid var(--border); border-radius: 8px; background: var(--bg); }
+.actions { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin: 12px 0; }
+.link { color: var(--accent); }
+@media (max-width: 480px) { .actions > * { width: 100%; } .btn { min-height: 44px; } }
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/web/src/app/globals.css
+++ b/web/src/app/globals.css
@@ -6,6 +6,13 @@
   --muted: #66707d;
   --accent: #2563eb;
   --danger: #b91c1c;
+  /* HUI-2232: semantic state colours. Painuo tokens are not in this tree yet;
+     when they land, only these variables change. */
+  --focus: #1d4ed8;
+  --info-bg: #eff6ff; --info-fg: #1e3a8a; --info-border: #bfdbfe;
+  --ok-bg: #ecfdf3; --ok-fg: #166534; --ok-border: #86efac;
+  --warn-bg: #fffbeb; --warn-fg: #92400e; --warn-border: #fcd34d;
+  --danger-bg: #fef2f2; --danger-fg: #991b1b; --danger-border: #fecaca;
 }
 * { box-sizing: border-box; }
 body {
PATCH2
````

限定保真面板（纯展示；报价出现后用 effect 把焦点移到“确认并生成”，因为按钮在报价请求尚未结束时先以禁用态挂载，`autoFocus` 在禁用元素上无效；主视图全是人话，内部编号 / 哈希只在折叠的“技术详情”里）：

`web/src/components/plate-lock/PlateLockScreen.tsx`

````tsx
"use client";
import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import { Stepper } from "@/components/ui/stepper";
import styles from "@/components/ui/ui.module.css";
import { canConfirm, formatMinor, sourcePath } from "@/lib/source-image";
import type { PlateAxisState, SourceCapabilities, SourceRunView } from "@/lib/source-image";
import { NO_USABLE_PHOTO, PLATE_PROMISE, PLATE_SCOPE_NOTE, PLATE_TITLE, PRICING_NOTE, RETURN_NOTE, STEPS, axisLabel, candidateSummary, canExportPlate, canReturnPlate, canSelectPlate, charged, failedAxes, plate, plateStatusLine, reasonText, stageOf, stateLabel } from "@/lib/plate-lock";

export type PlateScreenProps = {
  view: SourceRunView | null; capabilities: SourceCapabilities | null; nowUnix: number; projectID: string;
  loading?: boolean; busy?: boolean; background?: string; inputId?: string; notice?: string;
  returnEnabled?: boolean; returnAck?: boolean; returnNotice?: string; uploadSlot?: ReactNode;
  onBackground?: (v: string) => void; onInput?: (id: string) => void; onQuote?: () => void; onConfirm?: () => void; onRefresh?: () => void;
  onSelect?: () => void; onReturn?: () => void; onReturnAck?: (v: boolean) => void; onNew?: () => void;
};

const KIND_LABEL: Record<string, string> = { carton: "纸盒", handled_metal: "金属把手杯", glass_bottle: "玻璃瓶" };
function expiry(at: number): string { const d = new Date(at * 1000); return Number.isNaN(d.getTime()) ? "到期时间待核实" : d.toISOString().replace("T", " ").replace(".000Z", " UTC"); }
const stateClass = (s: PlateAxisState): string => (s === "PASS" ? styles.stateOK : s === "FAIL" ? styles.stateBad : s === "UNKNOWN" ? styles.stateWait : styles.stateNA);

export function PlateLockScreen(p: PlateScreenProps) {
  const mode = p.capabilities?.modes.find(m => m.mode === "background_plate_lock");
  const v = p.view, pl = plate(v), q = v?.quote;
  const supported = mode?.supported_inputs ?? [];
  const ready = mode?.ready === true;
  const chosen = supported.find(i => i.input_id === p.inputId) ?? supported[0];
  const hasPhoto = supported.length > 0;
  const bg = (p.background ?? "").trim();
  const pay = !!v && mode?.can_confirm === true && canConfirm(v, p.nowUnix);
  const stage = stageOf(v, hasPhoto, bg.length > 0);
  const failed = v ? failedAxes(v) : [];
  const summary = pl ? candidateSummary(pl.candidate_state) : null;
  const contentSrc = v && pl?.result && !v.deleted ? sourcePath(p.projectID, v.run_id, "content") : "";
  // Once per quote, when the confirm button first becomes usable, move focus to it so a
  // keyboard user lands on the one decision that spends money. (autoFocus cannot do this:
  // the button mounts disabled while the quote request is still finishing.)
  const focusedQuote = useRef("");
  const quoteId = q?.quote_id ?? "";
  useEffect(() => {
    if (!pay || p.busy || !quoteId || focusedQuote.current === quoteId) return;
    const el = document.querySelector<HTMLButtonElement>('[data-testid="plate-confirm"]');
    if (el) { el.focus(); focusedQuote.current = quoteId; }
  }, [pay, p.busy, quoteId]);
  return (
    <Card data-plate-entry="true" data-mode="background_plate_lock" data-ready={ready ? "true" : "false"} aria-labelledby="plate-title" aria-busy={p.busy || p.loading ? "true" : "false"}>
      <CardHeader>
        <CardTitle>{PLATE_TITLE}</CardTitle>
        <CardDescription>{PLATE_PROMISE}{PLATE_SCOPE_NOTE}</CardDescription>
      </CardHeader>
      <Stepper steps={STEPS} current={stage} label="制作步骤" />
      <div role="status" aria-live="polite" data-testid="plate-status">{p.loading ? "正在读取……" : plateStatusLine(v)}</div>
      {p.loading ? <><Skeleton /><Skeleton style={{ width: "70%" }} /></> : null}
      {p.uploadSlot}
      {!p.loading && mode && !ready ? <Alert tone="info" title="暂时不能使用" data-testid="plate-unavailable">{reasonText(mode.reason)}</Alert> : null}
      {!p.loading && ready && !hasPhoto && !v ? <Alert tone="warn" title="还不能开始" data-testid="plate-no-photo">{NO_USABLE_PHOTO}</Alert> : null}
      {ready && !v && hasPhoto ? (
        <form onSubmit={e => { e.preventDefault(); p.onQuote?.(); }} data-testid="plate-form">
          {supported.length > 1 ? (
            <Field htmlFor="plate-input" label="商品照片" hint="只列出在已验证范围内的照片">
              <select id="plate-input" value={chosen?.input_id} onChange={e => p.onInput?.(e.target.value)} aria-describedby="plate-input-hint">
                {supported.map(i => <option key={i.input_id} value={i.input_id}>{KIND_LABEL[i.kind] ?? "样本"} · {i.canvas.width_px}×{i.canvas.height_px}</option>)}
              </select>
            </Field>
          ) : <p data-testid="plate-photo">商品照片：{KIND_LABEL[chosen?.kind ?? ""] ?? "样本"} · 输出 {chosen?.canvas.width_px}×{chosen?.canvas.height_px}</p>}
          <Field htmlFor="plate-background" label="背景描述" hint="1 到 200 个字，例如：深蓝色渐变摄影棚背景，柔和顶光">
            <textarea id="plate-background" rows={3} maxLength={200} value={p.background ?? ""} onChange={e => p.onBackground?.(e.target.value)} aria-describedby="plate-background-hint" required />
          </Field>
          <p>{PRICING_NOTE}</p>
          <Button type="submit" disabled={!!p.busy || bg.length === 0}>获取报价</Button>
        </form>
      ) : null}
      {v ? (
        <section aria-label="报价与费用" data-testid="plate-quote">
          {q ? <p>报价 <strong>{formatMinor(q.amount_minor)}</strong> · 付款人：{v.payment.payer_label} · 有效至 {expiry(q.expires_at_unix)}</p> : <p>报价待核实。</p>}
          {pl?.background_intent ? <p>背景：{pl.background_intent}</p> : null}
          <p>{PRICING_NOTE}</p>
          {pay ? <Button type="button" disabled={!!p.busy} onClick={p.onConfirm} data-testid="plate-confirm">确认并生成 {formatMinor(q?.amount_minor)}</Button> : null}
          {charged(v) ? <p>已扣费 {formatMinor(v.payment.original_charged_minor ?? v.payment.charged_minor)}。</p> : null}
        </section>
      ) : null}
      {pl && summary && pl.derivation_state !== "pending" ? (
        <section aria-label="结果与商品检查" data-testid="plate-result" data-candidate={pl.candidate_state}>
          <Alert tone={summary.tone === "ok" ? "ok" : summary.tone === "bad" ? "danger" : "warn"} title={summary.label}>{summary.detail}</Alert>
          {failed.length > 0 ? <Alert tone="danger" title="没有通过的检查">{failed.map(a => axisLabel(a.name)).join("、")}</Alert> : null}
          {contentSrc ? <figure><img className={styles.resultImg} alt={pl.candidate_state === "limited_candidate" ? "换背景后的商品图" : "换背景后的商品图（未通过检查，不可使用）"} src={contentSrc} width={pl.canvas?.width_px} height={pl.canvas?.height_px} /><figcaption>{pl.canvas?.width_px}×{pl.canvas?.height_px}</figcaption></figure> : null}
          {(pl.axes ?? []).length > 0 ? (
            <>
              <h3>商品检查（逐项）</h3>
              <ul className={styles.axisList} aria-label="商品检查结果">
                {(pl.axes ?? []).filter(a => a.machine_checked).map(a => <li key={a.name}><span>{axisLabel(a.name)}</span><span className={stateClass(a.state)}>{stateLabel(a.state)}</span></li>)}
              </ul>
              <h3>无法由机器检查的项目</h3>
              <ul className={styles.axisList} aria-label="无法由机器检查的项目">
                {(pl.axes ?? []).filter(a => !a.machine_checked).map(a => <li key={a.name}><span>{axisLabel(a.name)}</span><span className={stateClass(a.state)}>{stateLabel(a.state)}</span></li>)}
              </ul>
              <p>是否真实好用：尚未经过真人评审。</p>
            </>
          ) : null}
          {(pl.limits ?? []).length > 0 ? <><h3>使用限制</h3><ul aria-label="使用限制">{(pl.limits ?? []).map(l => <li key={l}>{l}</li>)}</ul></> : null}
          <div className={styles.actions}>
            <Button type="button" disabled={!!p.busy || !canSelectPlate(v!)} onClick={p.onSelect} data-testid="plate-select">{v!.selected ? "已选定" : "选定这张"}</Button>
            {canExportPlate(v!) ? <a className={styles.link} href={sourcePath(p.projectID, v!.run_id, "export")} data-testid="plate-export">导出图片与检查报告</a> : <span aria-disabled="true">导出不可用</span>}
          </div>
          {p.returnEnabled && pl.candidate_state === "limited_candidate" ? (
            <div data-testid="plate-return">
              <Alert tone="warn" title="回传到来源订单 / 活动">{RETURN_NOTE}</Alert>
              <div className={styles.checkRow}>
                <input id="plate-return-ack" type="checkbox" checked={!!p.returnAck} onChange={e => p.onReturnAck?.(e.target.checked)} />
                <label htmlFor="plate-return-ack">我知道下游收到的回执不含上述限制说明</label>
              </div>
              <Button type="button" disabled={!!p.busy || !p.returnAck || !canReturnPlate(v!, true)} onClick={p.onReturn}>回传到来源</Button>
              {!v!.selected ? <p>请先选定这张结果，才能回传。</p> : null}
              {p.returnNotice ? <p role="status">{p.returnNotice}</p> : null}
            </div>
          ) : null}
        </section>
      ) : null}
      {p.notice ? <Alert tone="danger" title="没有完成" data-testid="plate-notice">{p.notice}</Alert> : null}
      <div className={styles.actions}>
        {v ? <Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onRefresh}>刷新状态</Button> : null}
        {v ? <Button type="button" variant="outline" disabled={!!p.busy} onClick={p.onNew}>换个背景再试（重新报价）</Button> : null}
      </div>
      <CapabilityMatrix capabilities={p.capabilities} />
      {v ? (
        <details data-technical="true">
          <summary>技术详情</summary>
          <dl>
            <dt>请求</dt><dd>{v.run_id} · {v.phase}</dd>
            <dt>背景生成任务</dt><dd>{v.task_id || "无"}{v.task_phase ? ` · ${v.task_phase}` : ""}</dd>
            {pl?.plate_task ? <><dt>背景图文件</dt><dd>{pl.plate_task.asset_id} · sha256 {pl.plate_task.sha256}</dd></> : null}
            {pl?.composite_sha256 ? <><dt>合成结果 hash</dt><dd>{pl.composite_sha256}</dd></> : null}
            {pl?.report_sha256 ? <><dt>检查报告 hash</dt><dd>{pl.report_sha256}</dd></> : null}
            {pl?.verdicts ? <><dt>分项结论</dt><dd>fidelity {pl.verdicts.fidelity} · generation {pl.verdicts.generation} · billing {pl.verdicts.billing} · human {pl.verdicts.human_usefulness}</dd></> : null}
            <dt>模型</dt><dd>{v.model} · {v.provider} · {v.size}</dd>
          </dl>
        </details>
      ) : null}
    </Card>
  );
}

const NOT_OPEN: Array<{ id: string; label: string }> = [
  { id: "creative_background", label: "创意换背景" },
  { id: "light_scene", label: "光影场景" },
  { id: "showcase_video", label: "展示视频" },
  { id: "reference_edit", label: "参考图编辑" },
  { id: "first_image", label: "首图生成（旧版）" },
];

// CapabilityMatrix tells the truth about every mode: two are server driven, the
// rest are closed until they have real success evidence.
export function CapabilityMatrix({ capabilities }: { capabilities: SourceCapabilities | null }) {
  const plateMode = capabilities?.modes.find(m => m.mode === "background_plate_lock");
  const textMode = capabilities?.modes.find(m => m.mode === "text_generate");
  const unknown = capabilities === null;
  const rows = [
    { id: "background_plate_lock", label: "限定保真换背景", state: unknown ? "unknown" : plateMode?.ready ? "available" : "unavailable", note: unknown ? "选好照片后显示" : plateMode?.ready ? "可用范围：冻结样本" : reasonText(plateMode?.reason ?? "") },
    { id: "text_generate", label: "文字生成概念图", state: unknown ? "unknown" : textMode?.ready ? "available" : "unavailable", note: unknown ? "选好照片后显示" : textMode?.ready ? "不保证商品一致" : "暂时不能使用" },
    ...NOT_OPEN.map(m => ({ ...m, state: "not_opened", note: "未开放：还没有真实成功证据" })),
  ];
  return (
    <section aria-label="做图方式" data-testid="capability-matrix">
      <h3>做图方式</h3>
      <ul className={styles.axisList}>
        {rows.map(r => (
          <li key={r.id} data-mode={r.id} data-state={r.state}>
            <span>{r.label}</span>
            <span className={r.state === "available" ? styles.stateOK : styles.stateNA}>{r.note}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
````

上传（只走既有 BFF：`/api/photos` → 无工程时 `/api/projects` → `/api/projects/{id}/inputs`；是否在已验证范围内是服务端的回答，这里不判断）：

`web/src/components/plate-lock/PhotoUpload.tsx`

````tsx
"use client";
import { useRef, useState } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";

// PhotoUpload sends the chosen file through the existing BFF routes only:
// /api/photos (server-side validation and content-idempotent registration),
// optionally /api/projects (a personal project when none exists yet), then
// /api/projects/{id}/inputs. It decides nothing about fidelity: whether the photo
// is inside the verified range is the server's answer in the capabilities.
export const MAX_PHOTO_BYTES = 20 << 20;
export const ACCEPT = "image/png,image/jpeg,image/webp";

export async function uploadAndAttach(file: File, projectId: string | undefined, fetcher: typeof fetch = fetch): Promise<string> {
  if (file.size === 0) throw new Error("文件是空的，请重新选择。");
  if (file.size > MAX_PHOTO_BYTES) throw new Error("照片超过 20 MB，请压缩后再试。");
  const bytes = await file.arrayBuffer();
  const qs = new URLSearchParams({ purpose: "project_photo", filename: file.name });
  const up = await fetcher(`/api/photos?${qs.toString()}`, { method: "POST", body: bytes, credentials: "same-origin" });
  const upData = (await up.json().catch(() => null)) as { photo?: { platform_asset_id?: string; original_name?: string; size_bytes?: number; media_type?: string }; error?: { message?: string } } | null;
  if (up.status === 401) throw new Error("请重新登录。");
  if (!up.ok || !upData?.photo?.platform_asset_id) throw new Error(upData?.error?.message ?? "上传没有成功，请稍后重试。");
  let project = projectId;
  if (!project) {
    const created = await fetcher("/api/projects", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name: "个人商品图", usage_kind: "电商主图", width_px: 1024, height_px: 1024, source_type: "standalone" }), credentials: "same-origin" });
    const data = (await created.json().catch(() => null)) as { project?: { id?: string } } | null;
    if (!created.ok || !data?.project?.id) throw new Error("创建工程没有成功，照片已保存，请重试。");
    project = data.project.id;
  }
  const photo = upData.photo;
  const attach = await fetcher(`/api/projects/${encodeURIComponent(project)}/inputs`, {
    method: "POST", headers: { "Content-Type": "application/json" }, credentials: "same-origin",
    body: JSON.stringify({ platform_asset_id: photo.platform_asset_id, snapshot_name: photo.original_name ?? file.name, snapshot_size: photo.size_bytes ?? bytes.byteLength, snapshot_content_type: photo.media_type ?? "application/octet-stream" }),
  });
  if (!attach.ok) throw new Error("照片已保存，但没有加入工程，请重试。");
  return project;
}

export function PhotoUpload({ projectId, onAttached }: { projectId?: string; onAttached: (projectId: string) => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState("");
  const input = useRef<HTMLInputElement | null>(null);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!file || busy) return;
    setBusy(true); setError(""); setDone("");
    try {
      const project = await uploadAndAttach(file, projectId);
      setDone("照片已加入。");
      setFile(null);
      if (input.current) input.current.value = "";
      onAttached(project);
    } catch (err) {
      setError(err instanceof Error ? err.message : "上传没有成功，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <form onSubmit={submit} aria-label="选择商品照片" data-testid="photo-upload">
      <Field htmlFor="photo-file" label="商品照片" hint="支持 PNG / JPEG / WebP，最大 20 MB。">
        <input id="photo-file" ref={input} type="file" accept={ACCEPT} aria-describedby="photo-file-hint" onChange={e => { setFile(e.target.files?.[0] ?? null); setError(""); setDone(""); }} />
      </Field>
      <Button type="submit" variant="secondary" disabled={!file || busy}>{busy ? "上传中……" : "上传并使用"}</Button>
      {error ? <Alert tone="danger">{error}</Alert> : null}
      {done ? <Alert tone="ok">{done}</Alert> : null}
    </form>
  );
}
````

- [ ] **Step 5: 写实现——面板接线与页面**

`SourceImagePanel` 泛化成按 `mode` 渲染（文字模式行为不变；plate 模式用上面的 Screen、每 3 秒用**纯 GET** 轮询原 run 直到派生完成、`回传` = `return` 后再用既有回执路由发回执）；`StartScreen` 增加 `compact`（个人路径只显示标题与工作区，删掉无效的“选择商品照片”按钮与开发者行）；`/start` 个人路径换成上传 + plate 面板、文字生成折叠进 `<details>`、旧 ConsumePanel / first-image 只留给订单 / 活动深链、个人工程靠 `?project=` 找回；项目页挂载 plate 面板并在任何照片挂接后让它重读；背景替换面板删掉“用模型生成背景并锁定主体”按钮与其处理函数：

````bash
cd "$WT"
git apply --whitespace=nowarn <<'PATCH1'
--- a/web/src/components/source-image/Panel.tsx
+++ b/web/src/components/source-image/Panel.tsx
@@ -2,8 +2,9 @@
 import { useEffect, useRef, useState } from "react";
 import { Card, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
 import { Button } from "@/components/ui/button";
+import { PlateLockScreen } from "@/components/plate-lock/PlateLockScreen";
 import { SourceImageController, sourceImageAPI, canConfirm, formatMinor, requestState, sourcePath, sourceSegment, pendingHintKey, parseSourceJSON, SourceHTTPError, sourceWritesDenied } from "@/lib/source-image";
-import type { SourceRunView, SourceCapabilities, SourceScope } from "@/lib/source-image";
+import type { SourceRunView, SourceCapabilities, SourceScope, SourceModeName } from "@/lib/source-image";
 
 type ScreenProps={ view: SourceRunView | null; capabilities: SourceCapabilities | null; nowUnix: number; projectID: string; busy?: boolean; prompt?: string; notice?: string; deleteArmed?: boolean; history?: SourceRunView[]; nextCursor?: string; pendingKey?: string;
   onPrompt?: (v: string)=>void; onQuote?: ()=>void; onConfirm?: ()=>void; onRefresh?: ()=>void; onReload?: ()=>void; onNew?: ()=>void; onAction?: (name:"reconcile"|"cancel"|"select"|"deleteOutput")=>void; onArmDelete?: ()=>void; onHistory?: (run:string)=>void; onMore?: ()=>void;
@@ -48,10 +49,13 @@
 }
 // A lease binds async validation to the panel that dispatched it, before controller scope exists.
 type PanelLease={generation:number;project:string};
-export function SourceImagePanel({projectId}:{projectId?:string}) {
-  const controller=useRef<SourceImageController | null>(null); if(!controller.current) controller.current=new SourceImageController(sourceImageAPI);
+export type SourceImagePanelProps={projectId?:string;mode?:SourceModeName;returnEnabled?:boolean;reloadKey?:number;uploadSlot?:React.ReactNode;onReturned?:()=>void};
+export function SourceImagePanel({projectId,mode="text_generate",returnEnabled=false,reloadKey=0,uploadSlot,onReturned}:SourceImagePanelProps) {
+  const plateMode=mode === "background_plate_lock", runParam=plateMode ? "plate_run" : "source_run";
+  const controller=useRef<SourceImageController | null>(null); if(!controller.current) controller.current=new SourceImageController(sourceImageAPI,undefined,undefined,mode);
   const c=controller.current;
   const [,setBound]=useState(projectId ?? ""), [prompt,setPrompt]=useState(""), [busy,setBusy]=useState(false), [notice,setNotice]=useState(""), [armed,setArmed]=useState(false), [tick,setTick]=useState(0), [history,setHistory]=useState<SourceRunView[]>([]), [nextCursor,setNextCursor]=useState("");
+  const [background,setBackground]=useState(""), [inputId,setInputId]=useState(""), [ack,setAck]=useState(false), [returnNotice,setReturnNotice]=useState(""), [loaded,setLoaded]=useState(false);
   const verifiedScope=useRef<SourceScope | null>(null), mounted=useRef(true);
   const binding=useRef({prop:projectId,project:projectId ?? "",generation:0});
   const operationRunning=useRef<PanelLease | null>(null);
@@ -73,7 +77,7 @@
     ensure(l);c.setScope(s);verifiedScope.current=s;
     if(changed) {setHistory([]);setNextCursor("");setArmed(false);repaint();}return s;
   }
-  function hint(l:PanelLease): void {ensure(l);const s=verifiedScope.current;if(!s)return;try {const h=c.pendingHint;if(h)sessionStorage.setItem(pendingHintKey(s),JSON.stringify(h));else sessionStorage.removeItem(pendingHintKey(s));}catch {}}
+  function hint(l:PanelLease): void {ensure(l);const s=verifiedScope.current;if(!s)return;try {const h=c.pendingHint;if(h)sessionStorage.setItem(pendingHintKey(s,mode),JSON.stringify(h));else sessionStorage.removeItem(pendingHintKey(s,mode));}catch {}}
   async function readHistory(l:PanelLease,cursor?:string): Promise<void> {
     const list=await step(l,()=>c.history(cursor));ensure(l);
     setHistory(prev=>cursor ? [...prev,...list.runs.filter(r=>!prev.some(p=>p.run_id === r.run_id))] : list.runs);setNextCursor(list.next_cursor);
@@ -83,14 +87,14 @@
     setBound(l.project);c.setScope(null);verifiedScope.current=null;setHistory([]);setNextCursor("");setArmed(false);setBusy(false);repaint();
     if(l.project && sourceSegment(l.project)) void (async()=>{try {
       await authorize(l);await step(l,()=>c.loadCapabilities());await readHistory(l);ensure(l);
-      const run=new URLSearchParams(window.location.search).get("source_run");
+      const run=new URLSearchParams(window.location.search).get(runParam);
       if(run && sourceSegment(run)) await step(l,()=>c.restore(l.project,run));
-      else {try {ensure(l);const raw=sessionStorage.getItem(pendingHintKey(verifiedScope.current!));if(raw)await step(l,()=>c.restoreHint(parseSourceJSON(raw)));}catch(e){ensure(l);void e;}}
-      ensure(l);repaint();
-    }catch {if(current(l)){invalidate();setHistory([]);setNextCursor("");setNotice("当前登录或工程未验证，请重新读取。");repaint();}}})();
+      else {try {ensure(l);const raw=sessionStorage.getItem(pendingHintKey(verifiedScope.current!,mode));if(raw)await step(l,()=>c.restoreHint(parseSourceJSON(raw)));}catch(e){ensure(l);void e;}}
+      ensure(l);setLoaded(true);repaint();
+    }catch {if(current(l)){invalidate();setHistory([]);setNextCursor("");setLoaded(true);setNotice("当前登录或工程未验证，请重新读取。");repaint();}}})();
     return()=>{if(current(l)){invalidate();mounted.current=false;}};
   // Effects perform only session/capabilities/history/original-run GET reads.
-  },[projectId,c]);
+  },[projectId,c,reloadKey]);
   useEffect(()=>{
     const interval=setInterval(()=>{if(c.view?.quote)repaint();},1000);
     const hide=()=>{invalidate();setHistory([]);setNextCursor("");setArmed(false);setBusy(false);repaint();};
@@ -104,13 +108,18 @@
     try {
       if(l.project){await authorize(l);await step(l,()=>c.loadCapabilities());}
       await step(l,()=>work(l));hint(l);
-      if(c.view){ensure(l);const url=new URL(window.location.href);url.searchParams.set("source_run",c.view.run_id);window.history.replaceState(null,"",url);await readHistory(l);}
+      if(c.view){ensure(l);const url=new URL(window.location.href);url.searchParams.set(runParam,c.view.run_id);window.history.replaceState(null,"",url);await readHistory(l);}
     }catch(e){if(current(l)){
       hint(l);setNotice(e instanceof SourceHTTPError ? e.status === 401 ? "请重新登录；原请求需服务端再次授权" : e.status === 409 ? "原请求事实冲突，请读取原记录" : "原请求状态待核实，请读取原记录或用原请求键恢复" : e instanceof Error ? e.message : "原请求状态待核实");
       if(e instanceof SourceHTTPError && [401,403,404].includes(e.status)){invalidate();setHistory([]);setNextCursor("");setBusy(false);repaint();}
     }}finally{if(current(l)){if(operationRunning.current === l)operationRunning.current=null;setBusy(false);repaint();}}
   }
   async function quote(l:PanelLease): Promise<void> {
+    if(plateMode){
+      if(!l.project)throw new Error("请先选择商品照片");
+      const supported=c.capabilities?.modes.find(m=>m.mode === "background_plate_lock")?.supported_input_ids ?? [];
+      await step(l,()=>c.quotePlate({projectID:l.project,inputID:inputId && supported.includes(inputId) ? inputId : supported[0] ?? "",background}));return;
+    }
     if(!l.project){
       const p=await step(l,()=>principal(()=>ensure(l)));
       const r=await step(l,()=>fetch("/api/projects",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({name:"个人普通概念图",usage_kind:"文生图",width_px:1024,height_px:1024,source_type:"standalone"}),credentials:"same-origin",redirect:"error",signal:AbortSignal.timeout(10000)}));
@@ -122,12 +131,34 @@
     }
     await step(l,()=>c.quote({projectID:l.project,prompt,size:"1024*1024"}));
   }
+  // A plate run is slow: poll the original run with plain GET reads until it is derived.
+  useEffect(()=>{
+    if(!plateMode)return;
+    const timer=setInterval(()=>{
+      const v=c.view,l={generation:binding.current.generation,project:binding.current.project};
+      if(!v || !v.confirmed || v.deleted || v.plate?.derivation_state !== "pending" || operationRunning.current || !verifiedScope.current || !current(l))return;
+      void (async()=>{try{await step(l,()=>c.restore(l.project,v.run_id));repaint();}catch{}})();
+    },3000);
+    return()=>clearInterval(timer);
+  // eslint-disable-next-line react-hooks/exhaustive-deps
+  },[plateMode,c]);
+  async function returnToSource(l:PanelLease): Promise<void> {
+    const outputId=await step(l,()=>c.returnToSource());
+    const r=await step(l,()=>fetch(`/api/projects/${encodeURIComponent(l.project)}/outputs/${encodeURIComponent(outputId)}/receipt`,{method:"POST",headers:{"Content-Type":"application/json"},body:"{}",credentials:"same-origin",redirect:"error",signal:AbortSignal.timeout(20000)}));
+    const d=await step(l,()=>r.json().catch(()=>null)) as {receipt?:{status?:string}}|null;
+    ensure(l);setReturnNotice(r.ok && d?.receipt?.status === "delivered" ? "已回传到来源。" : r.ok ? "已登记，回传正在重试，可稍后在成果区重发。" : "结果已登记，但回传没有成功，可稍后在成果区重发。");
+    setAck(false);onReturned?.();
+  }
   const displayed=projectId ?? binding.current.project, s=verifiedScope.current;
   const sameScope=displayed === binding.current.project && !!s && s.projectID === displayed;
   const belongs=(v:SourceRunView)=>sameScope && v.project_id === s!.projectID && v.payment.payer_user_id === s!.userID && (v.payment.payer_source !== "personal" || v.payment.payer_account_id === s!.accountID);
   const view=c.view && belongs(c.view) ? c.view:null;
   const local=(work:()=>void)=>{if(current(rendered))work();};
-  return <SourceImageScreen view={view} capabilities={sameScope ? c.capabilities:null} nowUnix={Math.floor(Date.now()/1000)} projectID={displayed} prompt={prompt} busy={busy && !!operationRunning.current} notice={notice} deleteArmed={sameScope && armed} history={history.filter(belongs)} nextCursor={sameScope ? nextCursor:""} pendingKey={sameScope ? c.pendingHint?.requestKey:undefined}
+  if(plateMode)return <PlateLockScreen view={view} capabilities={sameScope ? c.capabilities:null} nowUnix={Math.floor(Date.now()/1000)} projectID={displayed} loading={!loaded && !!displayed} busy={busy && !!operationRunning.current} background={background} inputId={inputId} notice={notice} returnEnabled={returnEnabled} returnAck={ack} returnNotice={returnNotice} uploadSlot={uploadSlot}
+    onBackground={v=>local(()=>setBackground(v))} onInput={v=>local(()=>setInputId(v))} onQuote={()=>void operate(quote)} onConfirm={()=>void operate(l=>step(l,()=>c.confirm()))}
+    onRefresh={()=>void operate(l=>step(l,()=>c.action("reconcile")))} onSelect={()=>void operate(l=>step(l,()=>c.action("select")))} onReturnAck={v=>local(()=>setAck(v))} onReturn={()=>void operate(returnToSource)}
+    onNew={()=>local(()=>{try {if(operationRunning.current)throw new Error("pending");c.newRequest();hint(rendered);setReturnNotice("");setAck(false);setNotice("");repaint();}catch {setNotice("上一步还没有结束，请稍候。");}})} />;
+  return <SourceImageScreen view={view} capabilities={sameScope ? c.capabilities:null} nowUnix={Math.floor(Date.now()/1000)} projectID={displayed} prompt={prompt} busy={busy && !!operationRunning.current} notice={notice} deleteArmed={sameScope && armed} history={history.filter(r=>belongs(r) && r.mode === mode)} nextCursor={sameScope ? nextCursor:""} pendingKey={sameScope ? c.pendingHint?.requestKey:undefined}
     onPrompt={v=>local(()=>setPrompt(v))} onQuote={()=>void operate(quote)} onConfirm={()=>void operate(l=>step(l,()=>c.confirm()))} onRefresh={()=>void operate(l=>step(l,()=>c.restore(l.project,c.view?.run_id ?? "")))} onReload={()=>void operate(async l=>{await readHistory(l);const h=c.pendingHint;if(h)await step(l,()=>c.restoreHint(h));})}
     onNew={()=>local(()=>{try {if(operationRunning.current)throw new Error("pending");c.newRequest();hint(rendered);setArmed(false);setNotice("下一次获取报价将创建新请求，原请求保留在历史中。");repaint();}catch {setNotice("原动作尚未返回");}})} onAction={name=>void operate(async l=>{await step(l,()=>c.action(name));ensure(l);setArmed(false);})} onArmDelete={()=>local(()=>setArmed(true))} onHistory={run=>void operate(l=>step(l,()=>c.restore(l.project,run)))} onMore={()=>void operate(l=>readHistory(l,nextCursor))} />;
 }
PATCH1
git apply --whitespace=nowarn <<'PATCH2'
--- a/web/src/components/first-image/StartScreen.tsx
+++ b/web/src/components/first-image/StartScreen.tsx
@@ -20,11 +20,35 @@
   sourceLabel?: string;
   returnHref?: string;
   authorizedAssets?: string[];
+  // compact: the personal start path. Only the title and the working area; the
+  // legacy status badges, step list and developer-facing lines are not shown.
+  compact?: boolean;
   children?: React.ReactNode;
 };
 
 export function StartScreen(props: StartScreenProps) {
   const assets = props.authorizedAssets ?? [];
+  if (props.compact) {
+    return (
+      <Card
+        data-entry-screen="start"
+        data-catalog={props.showsCatalog ? "true" : "false"}
+        data-scope={props.scope}
+        data-task-id={props.taskId}
+        data-task-status={props.status}
+        data-fake-output="false"
+        data-useful-proven="false"
+        data-upload-required={props.uploadRequired ? "true" : "false"}
+        data-quote={props.quoteLabel}
+      >
+        <CardHeader>
+          <CardTitle>{props.title}</CardTitle>
+          <CardDescription>选一张商品照片，写下想要的背景，确认费用后生成。商品本身不会被改动。</CardDescription>
+        </CardHeader>
+        {props.children}
+      </Card>
+    );
+  }
   return (
     <Card
       data-entry-screen="start"
@@ -66,9 +90,11 @@
       <p>{props.headline}</p>
       {props.showImage && props.outputAssetId ? <p>候选引用 {props.outputAssetId}</p> : null}
       {props.children}
-      <Button type="button" variant="secondary" disabled>
-        {props.uploadRequired ? "选择商品照片" : "已带入素材"}
-      </Button>
+      {props.uploadRequired ? null : (
+        <Button type="button" variant="secondary" disabled>
+          已带入素材
+        </Button>
+      )}
     </Card>
   );
 }
PATCH2
git apply --whitespace=nowarn <<'PATCH3'
--- a/web/src/app/start/ui.tsx
+++ b/web/src/app/start/ui.tsx
@@ -5,6 +5,7 @@
 import { useEffect, useMemo, useState } from "react";
 import { ConsumePanel } from "@/components/billing/ConsumePanel";
 import { StartScreen } from "@/components/first-image/StartScreen";
+import { PhotoUpload } from "@/components/plate-lock/PhotoUpload";
 import { TextImagePanel } from "@/components/text-image/Panel";
 import { SourceImagePanel } from "@/components/source-image/Panel";
 import { Button } from "@/components/ui/button";
@@ -53,11 +54,13 @@
   const topupPage = search.get("topup") || "";
   const quoteRef = search.get("quote_ref") || "";
   const sourced = source === "order" || source === "campaign";
+  // A personal project survives a refresh or a re-login through the address: ?project=<id>.
+  const projectParam = /^[A-Za-z0-9_-]{1,256}$/.test(search.get("project") ?? "") ? (search.get("project") as string) : "";
   const rechargeReturned = topupPage === "success";
 
   const [ready, setReady] = useState(false);
   const [scope, setScope] = useState(PERSONAL_SCOPE);
-  const [projectId, setProjectId] = useState("");
+  const [projectId, setProjectId] = useState(projectParam);
   const [taskId, setTaskId] = useState("");
   const [previousTaskId, setPreviousTaskId] = useState("");
   const [status, setStatus] = useState("");
@@ -67,6 +70,7 @@
   const [href, setHref] = useState(returnHref);
   const [notice, setNotice] = useState("");
   const [busy, setBusy] = useState(false);
+  const [reloadKey, setReloadKey] = useState(0);
   const [quotedPayer, setQuotedPayer] = useState("");
   const [quotedPresentation, setQuotedPresentation] = useState("");
   const [quotedGenerate, setQuotedGenerate] = useState(false);
@@ -195,7 +199,7 @@
       const listed = await fetch("/api/entry/personal");
       const list = (await listed.json().catch(() => null)) as { sessions?: SessionBody[] } | null;
       const current = list?.sessions?.[0];
-      if (!cancelled && current?.project_id) {
+      if (!cancelled && current?.project_id && !projectParam) {
         setProjectId(current.project_id);
         setScope(PERSONAL_SCOPE);
         const task = await fetch(`/api/projects/${current.project_id}/first-image`);
@@ -253,32 +257,6 @@
         return;
       }
       applySession(data?.session);
-    } finally {
-      setBusy(false);
-    }
-  }
-
-  async function begin(e: React.FormEvent) {
-    e.preventDefault();
-    if (rechargeReturned) return;
-    setBusy(true);
-    setNotice("");
-    try {
-      const res = await fetch("/api/entry/start", {
-        method: "POST",
-        headers: { "Content-Type": "application/json" },
-        body: JSON.stringify(form),
-      });
-      const data = (await res.json().catch(() => null)) as StartResponse & { error?: { message?: string } };
-      if (!res.ok || !data?.project?.id) {
-        setNotice(data?.error?.message ?? "还不能开始");
-        return;
-      }
-      applyStart(data);
-      const quote = await requestQuote(data.project.id);
-      applyQuote(quote.status, quote.body);
-      if (quote.status === 402 || quote.body?.topup) return;
-      await postFirstImage(data.project.id);
     } finally {
       setBusy(false);
     }
@@ -291,7 +269,9 @@
 
   useEffect(() => {
     setQuotedGenerate(false);
-    if (!projectId) return;
+    // The legacy quote belongs to the order/campaign deep link only. The personal
+    // path prices through the formal source quote inside the plate-lock panel.
+    if (!projectId || !sourced) return;
     let cancelled = false;
     (async () => {
       const quote = await requestQuote(projectId);
@@ -356,6 +336,7 @@
   });
   const heldForFunds = rechargeReturned || quotedTopup != null;
 
+  const personal = !sourced;
   return (
     <StartScreen
       title={screen.title}
@@ -375,109 +356,91 @@
       sourceLabel={sourceLabel}
       returnHref={href || undefined}
       authorizedAssets={assets}
+      compact={personal}
     >
-      <ConsumePanel
-        payerDisplay={quotedPayer || payerDisplay}
-        presentation={quotedPresentation || presentQuote({})}
-        generateAllowed={rechargeReturned ? false : quotedGenerate}
-        onReconfirm={() => {
-          if (!projectId || rechargeReturned) return;
-          void fetch("/api/billing/consume/confirm", {
-            method: "POST",
-            headers: { "Content-Type": "application/json" },
-            body: JSON.stringify(quotePayload(projectId)),
-          })
-            .then(async (res) => ({ status: res.status, body: (await res.json()) as QuoteBody }))
-            .then((quote) => {
-              applyQuote(quote.status, quote.body);
-              if (quote.status === 402 || quote.body?.topup) setQuotedGenerate(false);
-            })
-            .catch(() => setQuotedGenerate(false));
-        }}
-        mustRequote={quotedMust || quoteDecision.mustRequote || rechargeReturned}
-        topup={
-          rechargeReturned
-            ? {
-                entry: "billing_center",
-                returnTarget: "ti-product-image-engine-web",
-                quoteRef: quoteRef || quotedTopup?.quoteRef || "待重新报价",
-              }
-            : quotedTopup
-        }
-        levels={completionLevels()}
-        connection={connection}
-        settlement={settlement}
-        fundsState={fundsState}
-        quoteOrigin={quoteOrigin}
-        quoted={quotedFlag}
-      />
-      {sourced ? null : (
-        <form onSubmit={begin}>
-          <label>
-            工程名称
-            <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
-          </label>
-          <label>
-            使用场景
-            <input value={form.usage_kind} onChange={(e) => setForm({ ...form, usage_kind: e.target.value })} />
-          </label>
-          <label>
-            模型
-            <select value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })}>
-              <option value="standard">标准</option>
-              <option value="premium">高级</option>
-            </select>
-          </label>
-          <label>
-            张数
-            <input
-              type="number"
-              min={1}
-              value={form.image_count}
-              onChange={(e) => setForm({ ...form, image_count: Number(e.target.value) || 1 })}
+      {personal ? (
+        <SourceImagePanel
+          mode="background_plate_lock"
+          projectId={projectId || undefined}
+          reloadKey={reloadKey}
+          uploadSlot={
+            <PhotoUpload
+              projectId={projectId || undefined}
+              onAttached={(id) => {
+                setProjectId(id);
+                setReloadKey((k) => k + 1);
+                try {
+                  const url = new URL(window.location.href);
+                  url.searchParams.set("project", id);
+                  window.history.replaceState(null, "", url);
+                } catch {
+                  // The address is a convenience for refresh; the project itself is already saved.
+                }
+              }}
             />
-          </label>
-          <label>
-            宽
-            <input type="number" value={form.width_px} onChange={(e) => setForm({ ...form, width_px: Number(e.target.value) })} />
-          </label>
-          <label>
-            高
-            <input type="number" value={form.height_px} onChange={(e) => setForm({ ...form, height_px: Number(e.target.value) })} />
-          </label>
-          <label>
-            背景方向
-            <input
-              value={form.background_direction}
-              onChange={(e) => setForm({ ...form, background_direction: e.target.value })}
-            />
-          </label>
-          <label>
-            已有商品照片编号（没有真实上传时可以留空）
-            <input
-              value={form.photo_asset_id}
-              onChange={(e) => setForm({ ...form, photo_asset_id: e.target.value })}
-            />
-          </label>
-          <Button type="submit" disabled={busy || !ready || heldForFunds}>
-            {busy ? "处理中…" : "开始做产品图"}
-          </Button>
-        </form>
+          }
+        />
+      ) : (
+        <ConsumePanel
+          payerDisplay={quotedPayer || payerDisplay}
+          presentation={quotedPresentation || presentQuote({})}
+          generateAllowed={rechargeReturned ? false : quotedGenerate}
+          onReconfirm={() => {
+            if (!projectId || rechargeReturned) return;
+            void fetch("/api/billing/consume/confirm", {
+              method: "POST",
+              headers: { "Content-Type": "application/json" },
+              body: JSON.stringify(quotePayload(projectId)),
+            })
+              .then(async (res) => ({ status: res.status, body: (await res.json()) as QuoteBody }))
+              .then((quote) => {
+                applyQuote(quote.status, quote.body);
+                if (quote.status === 402 || quote.body?.topup) setQuotedGenerate(false);
+              })
+              .catch(() => setQuotedGenerate(false));
+          }}
+          mustRequote={quotedMust || quoteDecision.mustRequote || rechargeReturned}
+          topup={
+            rechargeReturned
+              ? {
+                  entry: "billing_center",
+                  returnTarget: "ti-product-image-engine-web",
+                  quoteRef: quoteRef || quotedTopup?.quoteRef || "待重新报价",
+                }
+              : quotedTopup
+          }
+          levels={completionLevels()}
+          connection={connection}
+          settlement={settlement}
+          fundsState={fundsState}
+          quoteOrigin={quoteOrigin}
+          quoted={quotedFlag}
+        />
       )}
       {projectId && sourced ? (
         <Button type="button" onClick={() => generate()} disabled={busy || heldForFunds}>
           用已授权素材生成
         </Button>
       ) : null}
-      {projectId ? (
+      {projectId && sourced ? (
         <Button type="button" variant="outline" onClick={refreshTask} disabled={busy}>
           刷新同一任务
         </Button>
       ) : null}
       {!same ? <p>刷新后的任务编号变了，请不要把它当成同一次生成。</p> : null}
-      {notice ? <p>{notice}</p> : null}
-      <SourceImagePanel projectId={projectId || undefined} />
-      <TextImagePanel projectId={projectId || undefined} historyOnly />
+      {notice ? <p role="alert">{notice}</p> : null}
+      {personal ? (
+        <details>
+          <summary>普通文字生成（不保证商品一致）</summary>
+          <SourceImagePanel projectId={projectId || undefined} />
+          <TextImagePanel projectId={projectId || undefined} historyOnly />
+        </details>
+      ) : (
+        <>
+          <SourceImagePanel projectId={projectId || undefined} />
+          <TextImagePanel projectId={projectId || undefined} historyOnly />
+        </>
+      )}
       <p>
         <Link href="/projects">已有制作工程</Link>
       </p>
PATCH3
git apply --whitespace=nowarn <<'PATCH4'
--- a/web/src/app/projects/[id]/page.tsx
+++ b/web/src/app/projects/[id]/page.tsx
@@ -10,6 +10,7 @@
 import { LightScenePanel } from "@/components/light-scene/Panel";
 import { ShowcaseVideoPanel } from "@/components/showcase-video/Panel";
 import { TextImagePanel } from "@/components/text-image/Panel";
+import { PhotoUpload } from "@/components/plate-lock/PhotoUpload";
 import { SourceImagePanel } from "@/components/source-image/Panel";
 
 interface Project {
@@ -289,6 +290,8 @@
   // 成功后把返回的资产引用挂接到本工程(同内容重传走幂等,不重复登记)。
   const [photoFile, setPhotoFile] = useState<File | null>(null);
   const [photoBusy, setPhotoBusy] = useState(false);
+  // Bumped whenever a photo is attached so the plate-lock panel re-reads which inputs it supports.
+  const [plateReload, setPlateReload] = useState(0);
 
   async function uploadPhoto(e: React.FormEvent) {
     e.preventDefault();
@@ -333,6 +336,7 @@
       );
       setPhotoFile(null);
       await load();
+      setPlateReload((k) => k + 1);
     } finally {
       setPhotoBusy(false);
     }
@@ -737,6 +741,14 @@
         }}
       />
 
+      <SourceImagePanel
+        mode="background_plate_lock"
+        projectId={id ?? ""}
+        returnEnabled={!!binding}
+        reloadKey={plateReload}
+        onReturned={() => void load()}
+        uploadSlot={<PhotoUpload projectId={id ?? ""} onAttached={() => setPlateReload((k) => k + 1)} />}
+      />
       <SourceImagePanel projectId={id ?? ""} />
       <TextImagePanel projectId={id ?? ""} historyOnly />
 
PATCH4
git apply --whitespace=nowarn <<'PATCH5'
--- a/web/src/components/background-replace/Panel.tsx
+++ b/web/src/components/background-replace/Panel.tsx
@@ -272,17 +272,6 @@
     if (data?.job) setActive(data.job as Job);
   }
 
-  async function modelPlate() {
-    if (!active || !maskFile) return;
-    const mask = await fileBase64(maskFile);
-    const data = await send(
-      `/api/projects/${projectId}/background-replacements/${active.id}/model-plate`,
-      "POST",
-      { mask_png_base64: mask }
-    );
-    if (data?.job) setActive(data.job as Job);
-  }
-
   async function inspect() {
     const body: Record<string, unknown> = { ...axes, similarity_only: similarityOnly };
     if (similarity.trim() !== "") {
@@ -416,22 +405,6 @@
             >
               锁定主体并换背景
             </Button>
-            <Button
-              type="button"
-              variant="secondary"
-              data-testid="bg-model-plate"
-              disabled={
-                busy ||
-                !active ||
-                stale ||
-                active.quote_status !== "confirmed" ||
-                (active.job_status !== "quoted" && active.job_status !== "generation_unavailable") ||
-                !maskFile
-              }
-              onClick={() => void modelPlate()}
-            >
-              用模型生成背景并锁定主体
-            </Button>
           </>
         ) : null}
       </div>
PATCH5
````

- [ ] **Step 6: 运行单测、类型检查与生产构建**

```bash
cd "$WT/web" && npx tsc --noEmit 2>&1 | head -5                                    # 期望：无输出
npx vitest run 2>&1 | grep -E "Test Files|Tests |FAIL|×" | head                    # 期望：Test Files 30 passed；Tests 240 passed（基线 201 + 新增 39）
"$RUN/bin/heavy" -- npm run build 2>&1 | tee "$EV/task5-next-build.log" | tail -8  # 期望：构建成功，/start 与 /projects/[id] 出现在路由表里
git -C "$WT" status --short | grep -v "^??" | head -30                             # 只能是上面 Files 里的路径
```

> `next build` 会生成未被跟踪的 `web/next-env.d.ts`：**不要 `git add` 它**。

- [ ] **Step 7: 引入 Playwright 并写 fixture 栈取证（只覆盖 UI 状态，不当验收）**

依赖：只加 `playwright-core`，**精确固定** `1.63.0`，不下载浏览器（复用本机 Chrome 二进制；若 `E2E_CHROME` 指向的二进制不存在，需要 Root 批准才能下载 Playwright 浏览器——此时本步记 `BLOCKED` 并停，不得自行下载）：

```bash
cd "$WT/web" && npm install --save-dev --save-exact --ignore-scripts playwright-core@1.63.0
node -e "const p=require('./package.json'); console.log(p.devDependencies['playwright-core'])"     # 期望：1.63.0（没有 ^ 或 ~）
node -e "const fs=require('fs'); const p=JSON.parse(fs.readFileSync('package.json','utf8')); p.scripts['e2e:fixture']='node e2e/fixture-run.mjs'; fs.writeFileSync('package.json', JSON.stringify(p,null,2)+'\n')"
git -C "$WT" diff --stat -- web/package.json web/package-lock.json
```

fixture API（**仅 fixture**：进程内的产品 Go API 替身，鉴权头 / 路径 / 错误码与真实 Go 一致；场景 `limited | not_usable | slow | create_503 | disabled`，由 `POST /__fixture/scenario` 切换；`/__fixture/stats` 暴露计数用来断言“刷新 / 重登不会再创建 run”）：

`web/e2e/zip.mjs`

````js
// Minimal ZIP helpers for "stored" (uncompressed) archives, which is what the
// product export writes. Used to verify exports and to build the fixture export.
import zlib from "node:zlib";

const crc32 = (buf) => (zlib.crc32 ? zlib.crc32(buf) >>> 0 : slowCrc(buf));
function slowCrc(buf) { let crc = 0xffffffff; for (const b of buf) { crc ^= b; for (let k = 0; k < 8; k++) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1; } return (crc ^ 0xffffffff) >>> 0; }

export function writeStoredZip(files) {
  const locals = [], centrals = []; let offset = 0;
  for (const [name, data] of Object.entries(files)) {
    const nameBuf = Buffer.from(name), crc = crc32(data);
    const local = Buffer.alloc(30); local.writeUInt32LE(0x04034b50, 0); local.writeUInt16LE(20, 4); local.writeUInt32LE(crc, 14); local.writeUInt32LE(data.length, 18); local.writeUInt32LE(data.length, 22); local.writeUInt16LE(nameBuf.length, 26);
    const central = Buffer.alloc(46); central.writeUInt32LE(0x02014b50, 0); central.writeUInt16LE(20, 4); central.writeUInt16LE(20, 6); central.writeUInt32LE(crc, 16); central.writeUInt32LE(data.length, 20); central.writeUInt32LE(data.length, 24); central.writeUInt16LE(nameBuf.length, 28); central.writeUInt32LE(offset, 42);
    locals.push(local, nameBuf, data); centrals.push(central, nameBuf);
    offset += 30 + nameBuf.length + data.length;
  }
  const centralBuf = Buffer.concat(centrals);
  const end = Buffer.alloc(22); end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(centrals.length / 2, 8); end.writeUInt16LE(centrals.length / 2, 10); end.writeUInt32LE(centralBuf.length, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, centralBuf, end]);
}

export function readStoredZip(buf) {
  let eocd = -1;
  for (let i = buf.length - 22; i >= 0; i--) if (buf.readUInt32LE(i) === 0x06054b50) { eocd = i; break; }
  if (eocd < 0) throw new Error("not a zip");
  const count = buf.readUInt16LE(eocd + 10); let p = buf.readUInt32LE(eocd + 16); const out = new Map();
  for (let n = 0; n < count; n++) {
    if (buf.readUInt32LE(p) !== 0x02014b50) throw new Error("bad central directory");
    const method = buf.readUInt16LE(p + 10), size = buf.readUInt32LE(p + 24), nameLen = buf.readUInt16LE(p + 28), extraLen = buf.readUInt16LE(p + 30), commentLen = buf.readUInt16LE(p + 32), localAt = buf.readUInt32LE(p + 42);
    const name = buf.subarray(p + 46, p + 46 + nameLen).toString();
    if (method !== 0) throw new Error(`entry ${name} is not stored`);
    const dataAt = localAt + 30 + buf.readUInt16LE(localAt + 26) + buf.readUInt16LE(localAt + 28);
    out.set(name, buf.subarray(dataAt, dataAt + size));
    p += 46 + nameLen + extraLen + commentLen;
  }
  return out;
}
````

`web/e2e/fixture-api.mjs`

````js
// FIXTURE ONLY. A tiny in-process stand-in for the product Go API, used to drive
// the real web app in a real browser through states that are expensive or unsafe
// to produce on the real stack (errors, slow runs, unusable candidates).
// Evidence taken against it is labelled `fixture` and is never acceptance evidence.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { writeStoredZip } from "./zip.mjs";

const PORT = Number(process.env.FIXTURE_API_PORT ?? 32324);
const SAMPLES = process.env.FIXTURE_SAMPLES_DIR;
if (!SAMPLES) throw new Error("FIXTURE_SAMPLES_DIR is required");
const manifest = JSON.parse(fs.readFileSync(path.join(SAMPLES, "manifest.json"), "utf8"));
const frozen = new Map(manifest.cases.map((c) => [c.original.sha256, c]));
const TOKEN = "fixture-token";
const ACCESS = "fixture-access";
const sha = (b) => crypto.createHash("sha256").update(b).digest("hex");
const PASS = ["mask_pixels_identical", "coverage_logo", "coverage_packaging_text", "coverage_spec", "coverage_structure", "product_count", "output_size", "plate_size", "plate_is_new_background", "background_changed", "plate_task_succeeded", "plate_asset_hash_verified", "plate_charged_observed"];

const state = { scenario: "limited", photos: new Map(), inputs: [], runs: new Map(), stats: { creates: 0, confirms: 0, selects: 0, returns: 0, receipts: 0, uploads: 0, projects: 0 }, serial: 0 };
const reset = () => { state.photos.clear(); state.inputs = []; state.runs.clear(); Object.keys(state.stats).forEach((k) => (state.stats[k] = 0)); state.serial = 0; };

const json = (res, status, body) => { res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" }); res.end(JSON.stringify(body)); };
const err = (res, status, code, message = code) => json(res, status, { error: { code, message } });
const body = (req) => new Promise((resolve) => { const chunks = []; req.on("data", (c) => chunks.push(c)); req.on("end", () => resolve(Buffer.concat(chunks))); });

function supported() {
  return state.inputs.filter((i) => state.photos.get(i.platform_asset_id)?.frozen).map((i) => ({ input_id: i.id, kind: state.photos.get(i.platform_asset_id).frozen.kind, canvas: { width_px: state.photos.get(i.platform_asset_id).frozen.canvas.w, height_px: state.photos.get(i.platform_asset_id).frozen.canvas.h } }));
}
function capabilities() {
  const disabled = state.scenario === "disabled";
  const list = supported();
  return {
    historical_read: true, automatic_output_recovery_ready: true,
    modes: [
      { mode: "text_generate", ready: false, disabled: true, can_quote: false, can_confirm: false, authorization_reason: "", runtime_reason: "", reason: "source_unconfigured", size: "1024*1024", model: "qwen-image-2.0", fidelity: "not_applicable", visual_quality: "unknown" },
      { mode: "background_plate_lock", ready: !disabled, disabled: disabled, can_quote: !disabled, can_confirm: !disabled, reason: disabled ? "plate_lock_disabled" : "", size: "1024*1024", model: "qwen-image-2.0", claim: "frozen_synthetic_sample_only", fidelity: "frozen_sample_only", visual_quality: "unknown", supported_input_ids: disabled ? [] : list.map((i) => i.input_id), supported_inputs: disabled ? [] : list },
      { mode: "reference_edit", ready: false, disabled: true, reason: "formal_contract_unconfigured" },
    ],
  };
}
const report = (run) => JSON.stringify({ report_version: "product-source-quality/v2", facts: { composite_sha256: sha(run.png) } });
function view(run) {
  const done = run.derived;
  const candidate = !done ? "pending" : state.scenario === "not_usable" ? "not_usable_candidate" : "limited_candidate";
  const axes = [...PASS.map((name) => ({ name, state: candidate === "not_usable_candidate" && name === "background_changed" ? "FAIL" : "PASS", machine_checked: true, evidence: "fixture" })),
    ...["background_extra_objects", "background_intent_followed", "visual_composite_quality"].map((name) => ({ name, state: "UNKNOWN", machine_checked: false, evidence: "not machine checkable" })), { name: "transmission_edge", state: "N/A", machine_checked: false, evidence: "not machine checkable" }];
  const now = Math.floor(Date.now() / 1000);
  const plate = { result_kind: "derived_composite", claim: "frozen_synthetic_sample_only", human_usefulness: "NOT_RUN", derivation_state: done ? "derived" : "pending", candidate_state: candidate, sample_set_id: "frozen-sample-set", case_id: run.case.case_id, background_intent: run.intent, canvas: { width_px: run.case.canvas.w, height_px: run.case.canvas.h } };
  if (done) Object.assign(plate, { verdicts: { fidelity: "PASS", generation: "PASS", billing: "PASS", human_usefulness: "NOT_RUN" }, axes, limits: candidate === "limited_candidate" ? ["已验证范围仅限冻结合成样本（纸盒/金属把手杯/玻璃瓶），不代表任意商品照片", "样本为硬边渲染，未覆盖真实照片的边缘羽化与光晕", "背景内容未经机器检查：可能出现多余物体，也可能与所写方向不符", "整体观感未经人工评审"] : [], report_sha256: sha(Buffer.from(report(run))), composite_sha256: sha(run.png), result: { content_type: "image/png", size_bytes: String(run.png.length), width_px: run.case.canvas.w, height_px: run.case.canvas.h, sha256: sha(run.png) } });
  return {
    run_id: run.id, project_id: run.project, request_key: run.key, owner_version: "product_source_v1", mode: "background_plate_lock", model: "qwen-image-2.0", provider: "modelxing-qwen-image-2.0-v1", size: "1024*1024", quantity: "1",
    phase: done ? "succeeded" : run.confirmed ? "task_linked" : "quoted", task_id: run.confirmed ? "task_fx" : "", selected: run.selected, deleted: false, cancel_requested: false, confirmed: run.confirmed,
    payment: { currency: "CNY", payer_source: "personal", payer_label: "个人账户", payer_account_id: "acct_fixture", payer_user_id: "usr_fixture", status: done ? "charged" : "quoted", charged_minor: done ? "25" : null, original_charged_minor: done ? "25" : null, refund_amount_minor: null, refresh_status: "not_refreshed", observation_time_known: false, last_verified_at: null, ...(done ? { charge_id: "charge_fx", original_charge_id: "charge_fx" } : {}) },
    quote: { quote_id: "quote_fx", quote_fingerprint: "a".repeat(64), usage_id: "usage_fx", currency: "CNY", quantity: "1", unit_price_minor: "25", amount_minor: "25", quoted_at_unix: run.quotedAt, expires_at_unix: run.quotedAt + 300, pricing_version: "fixture-price-v1" },
    output: null, snapshot_updated_at: now, fidelity: "frozen_sample_only", visual_quality: "unknown", human_adoption: "NOT_RUN", automatic_output_recovery_ready: true, content_availability: "not_checked", plate,
  };
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${PORT}`);
  const p = url.pathname;
  if (p === "/__fixture/scenario" && req.method === "POST") { const b = JSON.parse((await body(req)).toString() || "{}"); state.scenario = b.name ?? "limited"; if (b.reset) reset(); return json(res, 200, { scenario: state.scenario }); }
  if (p === "/__fixture/stats") return json(res, 200, { ...state.stats, scenario: state.scenario });
  if (p === "/__fixture/advance" && req.method === "POST") { for (const r of state.runs.values()) if (r.confirmed) r.derived = true; return json(res, 200, {}); }
  if (req.headers["x-product-internal-token"] !== TOKEN) return err(res, 401, "unauthenticated");
  if (req.headers.authorization !== `Bearer ${ACCESS}`) return err(res, 401, "unauthenticated");
  if (p === "/api/v1/auth/session") return json(res, 200, { principal: { user_id: "usr_fixture", account_id: "acct_fixture", tenant_id: "acct_fixture" } });
  if (p === "/api/v1/entry/personal") return json(res, 200, { sessions: [] });
  if (p === "/api/v1/billing/balance") return json(res, 200, { balance_cny: "100.00", items: [], account_id: "acct_fixture" });
  if (p === "/api/v1/photos" && req.method === "POST") {
    const buf = await body(req);
    if (buf.subarray(0, 8).toString("hex") !== "89504e470d0a1a0a") return err(res, 415, "unsupported_media_type", "内容不是受支持的图片格式");
    const sha = crypto.createHash("sha256").update(buf).digest("hex");
    state.stats.uploads++;
    const asset = "asset_" + sha.slice(0, 12);
    const photo = { id: "pho_" + sha.slice(0, 8), platform_asset_id: asset, original_name: url.searchParams.get("filename") ?? "photo.png", size_bytes: buf.length, media_type: "image/png", sha256: sha };
    state.photos.set(asset, { ...photo, frozen: frozen.get(sha) ?? null });
    return json(res, 201, { photo, idempotent: false });
  }
  if (p === "/api/v1/projects" && req.method === "POST") { state.stats.projects++; return json(res, 201, { project: { id: "proj_fx", name: "个人商品图", status: "draft", width_px: 1024, height_px: 1024, source_type: "standalone" } }); }
  let m;
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/inputs$/)) && req.method === "POST") {
    const b = JSON.parse((await body(req)).toString());
    const input = { id: "pin_" + ++state.serial, platform_asset_id: b.platform_asset_id, snapshot_name: b.snapshot_name };
    state.inputs.push(input);
    return json(res, 201, { input });
  }
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/source-image-capabilities$/))) return json(res, 200, capabilities());
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/source-image-runs(?:\/([^/]+))?(?:\/([^/]+))?$/))) {
    const [, project, runId, action] = m;
    if (!runId) {
      if (req.method === "GET") return json(res, 200, { runs: [...state.runs.values()].map(view), next_cursor: "" });
      const b = JSON.parse((await body(req)).toString());
      if (state.scenario === "create_503") return err(res, 503, "sample_set_invalid");
      const input = state.inputs.find((i) => i.id === b.input_id);
      const photo = input && state.photos.get(input.platform_asset_id);
      if (!photo?.frozen) return err(res, 422, "sample_not_frozen");
      const existing = [...state.runs.values()].find((r) => r.key === b.request_key);
      if (existing) return json(res, 200, view(existing));
      state.stats.creates++;
      const c = photo.frozen;
      const run = { id: "sir_fx" + state.serial++, project, key: b.request_key, intent: b.background_intent, case: c, quotedAt: Math.floor(Date.now() / 1000) - 1, confirmed: false, derived: false, selected: false, polls: 0, png: fs.readFileSync(path.join(SAMPLES, c.original.path)) };
      state.runs.set(run.id, run);
      return json(res, 200, view(run));
    }
    if (runId === "by-request") { const r = [...state.runs.values()].find((x) => x.key === url.searchParams.get("request_key")); return r ? json(res, 200, view(r)) : err(res, 404, "not_found"); }
    const run = state.runs.get(runId);
    if (!run) return err(res, 404, "not_found");
    if (!action && req.method === "GET") { if (run.confirmed && state.scenario !== "slow" && ++run.polls >= 2) run.derived = true; return json(res, 200, view(run)); }
    if (action === "confirm") { await body(req); state.stats.confirms++; run.confirmed = true; return json(res, 200, view(run)); }
    if (action === "reconcile") { await body(req); if (run.confirmed && state.scenario !== "slow") run.derived = true; return json(res, 200, view(run)); }
    if (action === "select") { await body(req); if (view(run).plate.candidate_state !== "limited_candidate") return err(res, 409, "candidate_not_usable"); state.stats.selects++; run.selected = true; return json(res, 200, view(run)); }
    if (action === "return") { await body(req); state.stats.returns++; return json(res, 200, { output: { id: "out_fx" } }); }
    if (action === "content") { res.writeHead(200, { "Content-Type": "image/png", "Content-Length": run.png.length, "X-Quality-Verdict": "limited" }); return res.end(run.png); }
    if (action === "export") { const zip = writeStoredZip({ "image.png": run.png, "quality.json": Buffer.from(report(run)), "snapshot.json": Buffer.from("{}") }); res.writeHead(200, { "Content-Type": "application/zip", "Content-Length": zip.length }); return res.end(zip); }
  }
  if ((m = p.match(/^\/api\/v1\/projects\/([^/]+)\/outputs\/([^/]+)\/receipt$/)) && req.method === "POST") { state.stats.receipts++; return json(res, 201, { receipt: { status: "delivered" } }); }
  return err(res, 404, "not_found");
});
server.listen(PORT, "127.0.0.1", () => console.log(`fixture api listening on ${PORT}`));
````

取证脚本（12 个场景：空态、范围外照片、**纯键盘**走完整流程并断言焦点与焦点环、刷新与退出重登找回同一 run 且计数不变、不可用候选、创建 503 与关闭态、能力矩阵；每个场景桌面 1280×800 与手机 390×844 各一张全页截图并断言无横向滚动与主视图无内部词）。浏览器经 `playwright-core` 启动本机 Chrome，**独立 `--user-data-dir`**、headless、只用 `32321/32324`：

`web/e2e/fixture-run.mjs`

````js
// FIXTURE-STACK browser run. Drives the real Next.js app in a real headless
// Chrome (own profile, own ports) against e2e/fixture-api.mjs. Everything it
// produces is labelled `fixture`; it proves UI states, never real generation.
import { spawn, execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const WEB = Number(process.env.E2E_WEB_PORT ?? 32321), API = Number(process.env.FIXTURE_API_PORT ?? 32324);
const OUT = required("E2E_OUT"), PROFILE = required("E2E_PROFILE"), SAMPLES = required("FIXTURE_SAMPLES_DIR");
const CHROME = process.env.E2E_CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
// Browse via "localhost": the source BFF compares the request Origin with the URL Next rebuilds, which is localhost.
const BASE = `http://localhost:${WEB}`;
const VIEWPORTS = { desktop: { width: 1280, height: 800 }, mobile: { width: 390, height: 844 } };
const results = [];
const children = [];

function required(name) { const v = process.env[name]; if (!v) { console.error(`${name} is required`); process.exit(2); } return v; }
function crc(buf) { let c, crcv = 0xffffffff; for (let n = 0; n < buf.length; n++) { c = (crcv ^ buf[n]) & 0xff; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; crcv = (crcv >>> 8) ^ c; } return (crcv ^ 0xffffffff) >>> 0; }
function chunk(type, data) { const len = Buffer.alloc(4); len.writeUInt32BE(data.length); const td = Buffer.concat([Buffer.from(type), data]); const c = Buffer.alloc(4); c.writeUInt32BE(crc(td)); return Buffer.concat([len, td, c]); }
function tinyPng() { const ihdr = Buffer.alloc(13); ihdr.writeUInt32BE(2, 0); ihdr.writeUInt32BE(2, 4); ihdr[8] = 8; ihdr[9] = 2; const raw = Buffer.from([0, 200, 30, 30, 200, 30, 30, 0, 30, 200, 30, 30, 200, 30]); return Buffer.concat([Buffer.from("89504e470d0a1a0a", "hex"), chunk("IHDR", ihdr), chunk("IDAT", zlib.deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]); }

function start(cmd, args, env, name) {
  const child = spawn(cmd, args, { cwd: webDir, env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] });
  child.stdout.on("data", (d) => process.env.E2E_VERBOSE && process.stdout.write(`[${name}] ${d}`));
  child.stderr.on("data", (d) => process.env.E2E_VERBOSE && process.stderr.write(`[${name}] ${d}`));
  children.push(child);
  return child;
}
async function waitFor(url, ms = 60000) { const end = Date.now() + ms; for (;;) { try { const r = await fetch(url); if (r.status < 500) return; } catch {} if (Date.now() > end) throw new Error(`timeout waiting for ${url}`); await new Promise((r) => setTimeout(r, 300)); } }
const api = (p, body) => fetch(`http://127.0.0.1:${API}${p}`, body ? { method: "POST", body: JSON.stringify(body) } : {}).then((r) => r.json());

async function open(name, vp) {
  const dir = path.join(PROFILE, `${name}-${vp}`);
  fs.rmSync(dir, { recursive: true, force: true });
  const context = await chromium.launchPersistentContext(dir, { executablePath: CHROME, headless: true, viewport: VIEWPORTS[vp], args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
  await context.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
  const page = context.pages()[0] ?? (await context.newPage());
  return { context, page };
}
const shot = (page, name, vp) => page.screenshot({ path: path.join(OUT, `fixture-${name}-${vp}.png`), fullPage: true });
const noHorizontalScroll = (page) => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1);
const visibleText = (page) => page.evaluate(() => { const clone = document.body.cloneNode(true); clone.querySelectorAll("details").forEach((d) => d.remove()); clone.querySelectorAll("script,style").forEach((d) => d.remove()); return clone.innerText; });

async function run(name, vps, fn) {
  for (const vp of vps) {
    const label = `${name}@${vp}`;
    const { context, page } = await open(name, vp);
    try { await fn({ page, context, vp }); results.push({ name: label, layer: "fixture", ok: true }); console.log(`PASS ${label}`); }
    catch (e) { results.push({ name: label, layer: "fixture", ok: false, error: String(e.message ?? e) }); console.log(`FAIL ${label}: ${e.message}`); try { await shot(page, `${name}-FAILED`, vp); } catch {} }
    finally { await context.close(); }
  }
}

const frozenCarton = path.join(SAMPLES, "originals", "carton-1024x1280.png");
async function chooseFrozen(page) { await page.locator("#photo-file").setInputFiles(frozenCarton); await page.getByRole("button", { name: "上传并使用" }).click(); await page.getByTestId("plate-form").waitFor({ timeout: 20000 }); }

async function main() {
  fs.mkdirSync(OUT, { recursive: true });
  for (const f of fs.readdirSync(OUT)) if (f.startsWith("fixture-")) fs.rmSync(path.join(OUT, f));
  start("node", ["e2e/fixture-api.mjs"], { FIXTURE_API_PORT: String(API), FIXTURE_SAMPLES_DIR: SAMPLES }, "api");
  start(path.join(webDir, "node_modules/.bin/next"), ["start", "-p", String(WEB)], { PRODUCT_SERVER_BASE_URL: `http://127.0.0.1:${API}`, PRODUCT_SERVER_TOKEN: "fixture-token", NEXT_TELEMETRY_DISABLED: "1" }, "web");
  await waitFor(`http://127.0.0.1:${API}/__fixture/stats`);
  await waitFor(`${BASE}/login`);

  await run("start-empty", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    await page.getByRole("heading", { name: "开始做产品图" }).waitFor();
    await page.locator("#photo-file").waitFor();
    assert.equal(await page.getByRole("button", { name: "选择商品照片" }).count(), 0, "the dead disabled button must be gone");
    const text = await visibleText(page);
    for (const banned of ["商品照片编号", "Task", "NOT_RUN", "UNKNOWN", "provider", "hash"]) assert.ok(!text.includes(banned), `developer wording on /start: ${banned}`);
    assert.ok(await noHorizontalScroll(page), "horizontal scroll");
    await shot(page, "start-empty", vp);
  });

  await run("photo-not-in-range", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    const png = path.join(PROFILE, "tiny.png"); fs.writeFileSync(png, tinyPng());
    await page.locator("#photo-file").setInputFiles(png);
    await page.getByRole("button", { name: "上传并使用" }).click();
    await page.getByTestId("plate-no-photo").waitFor({ timeout: 20000 });
    assert.equal(await page.getByTestId("plate-form").count(), 0, "no paid form for a photo outside the verified range");
    assert.equal((await api("/__fixture/stats")).creates, 0);
    await shot(page, "photo-not-in-range", vp);
  });

  await run("limited-flow-keyboard", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    const bg = page.getByLabel("背景描述");
    await bg.focus();
    await page.keyboard.type("深蓝色渐变摄影棚背景，柔和顶光");
    await page.keyboard.press("Tab");
    assert.equal(await page.evaluate(() => document.activeElement?.textContent), "获取报价", "Tab reaches the quote button");
    await page.keyboard.press("Enter");
    await page.getByTestId("plate-quote").waitFor();
    await page.getByText("¥0.25").first().waitFor();
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("data-testid")), "plate-confirm", "focus moves to the confirm button");
    const ring = await page.evaluate(() => { const s = getComputedStyle(document.activeElement); return `${s.outlineStyle} ${s.outlineWidth}`; });
    assert.ok(ring.startsWith("solid") && !ring.endsWith(" 0px"), `visible focus ring: ${ring}`);
    await shot(page, "quote", vp);
    await page.keyboard.press("Enter");
    await page.locator('[data-testid="plate-result"][data-candidate="limited_candidate"]').waitFor({ timeout: 40000 });
    assert.ok(await page.locator('[data-testid="plate-result"] img').evaluate((img) => img.complete && img.naturalWidth > 0), "the result image really loads");
    assert.ok(await page.getByText("商品像素与原图完全一致").first().isVisible());
    assert.ok(await page.getByText("使用限制").first().isVisible());
    await page.getByTestId("plate-select").click();
    await page.getByRole("button", { name: "已选定" }).waitFor();
    assert.match(await page.getByTestId("plate-export").getAttribute("href"), /\/export$/);
    assert.ok(await noHorizontalScroll(page), "horizontal scroll");
    const text = await visibleText(page);
    for (const banned of ["Task", "NOT_RUN", "UNKNOWN", "sha256", "provider"]) assert.ok(!text.includes(banned), `internal word in the primary view: ${banned}`);
    await shot(page, "result-limited", vp);
    const stats = await api("/__fixture/stats");
    assert.equal(stats.creates, 1); assert.equal(stats.confirms, 1);
  });

  await run("refresh-and-relogin-restore-same-run", ["desktop"], async ({ page, context, vp }) => {
    await api("/__fixture/scenario", { name: "slow", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    await page.getByLabel("背景描述").fill("暖色原木桌面，窗边自然光");
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-confirm").click();
    await page.getByText("正在生成背景并检查商品").first().waitFor();
    const url = page.url();
    assert.match(url, /plate_run=sir_/);
    await page.reload();
    await page.getByText("正在生成背景并检查商品").first().waitFor({ timeout: 20000 });
    await context.clearCookies();
    await context.addCookies([{ name: "pia_access", value: "fixture-access", url: BASE }]);
    await page.goto(url);
    await page.getByText("正在生成背景并检查商品").first().waitFor({ timeout: 20000 });
    await shot(page, "restored-pending", vp);
    const stats = await api("/__fixture/stats");
    assert.equal(stats.creates, 1, "refresh and re-login must not create a second run"); assert.equal(stats.confirms, 1);
    await api("/__fixture/advance", {});
    await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 30000 });
  });

  await run("not-usable-candidate", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "not_usable", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    await page.getByLabel("背景描述").fill("纯白背景");
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-confirm").click();
    const result = page.locator('[data-testid="plate-result"][data-candidate="not_usable_candidate"]');
    await result.waitFor({ timeout: 40000 });
    assert.ok(await result.getByRole("alert").first().isVisible());
    assert.ok(await result.getByText("背景相比原图有明显变化").first().isVisible());
    assert.equal(await page.getByTestId("plate-select").isDisabled(), true);
    assert.equal(await page.getByTestId("plate-export").count(), 0);
    await shot(page, "result-not-usable", vp);
  });

  await run("errors-and-disabled", ["desktop", "mobile"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "create_503", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    await page.getByLabel("背景描述").fill("深蓝");
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-notice").waitFor({ timeout: 20000 });
    assert.equal(await page.getByTestId("plate-confirm").count(), 0);
    await shot(page, "error-create", vp);
    await api("/__fixture/scenario", { name: "disabled" });
    await page.goto(`${BASE}/start?project=${new URL(page.url()).searchParams.get("project")}`);
    await page.getByTestId("plate-unavailable").waitFor({ timeout: 20000 });
    assert.ok((await page.getByTestId("plate-unavailable").innerText()).includes("暂未开放"));
    assert.equal(await page.getByRole("button", { name: "获取报价" }).count(), 0);
    await shot(page, "disabled", vp);
  });

  await run("capability-matrix", ["desktop"], async ({ page, vp }) => {
    await api("/__fixture/scenario", { name: "limited", reset: true });
    await page.goto(`${BASE}/start`);
    await chooseFrozen(page);
    const matrix = page.getByTestId("capability-matrix");
    await matrix.waitFor();
    for (const id of ["creative_background", "light_scene", "showcase_video", "reference_edit", "first_image"]) assert.equal(await matrix.locator(`[data-mode="${id}"]`).getAttribute("data-state"), "not_opened");
    assert.equal(await matrix.locator("button").count(), 0);
    await shot(page, "capability-matrix", vp);
  });
}

let failed = false;
try { await main(); } catch (e) { console.error(e); failed = true; }
finally {
  for (const c of children) c.kill("SIGTERM");
  let sha = "unknown"; try { sha = execFileSync("git", ["rev-parse", "HEAD"], { cwd: webDir }).toString().trim(); } catch {}
  fs.writeFileSync(path.join(OUT, "fixture-run-record.json"), JSON.stringify({ layer: "fixture", note: "UI states against a fake API; not acceptance evidence", frontend_git_sha: sha, chrome: CHROME, results }, null, 2));
}
if (failed || results.some((r) => !r.ok)) process.exit(1);
````

- [ ] **Step 8: 跑取证并目视检查**

```bash
cd "$WT/web" && export E2E_OUT="$EV/browser/fixture" E2E_PROFILE="$RUN/caches/HUI-2232/pw-profile" FIXTURE_SAMPLES_DIR="$WT/fixtures/frozen-samples/v1"
mkdir -p "$E2E_OUT" "$E2E_PROFILE"
"$RUN/bin/heavy" -- node e2e/fixture-run.mjs 2>&1 | tee "$EV/task5-fixture-run.log"
```

期望：恰好 12 行 `PASS`（`start-empty@desktop|mobile`、`photo-not-in-range@desktop|mobile`、`limited-flow-keyboard@desktop|mobile`、`refresh-and-relogin-restore-same-run@desktop`、`not-usable-candidate@desktop|mobile`、`errors-and-disabled@desktop|mobile`、`capability-matrix@desktop`），退出码 0，`$E2E_OUT/fixture-run-record.json` 里 `layer:"fixture"` 且 `results` 全 `ok:true`，目录里有 `fixture-*.png`。若有 `FAIL`，对应的 `fixture-<场景>-FAILED-*.png` 是失败瞬间的截图。

用 Read 工具逐张打开并**目视确认**：`fixture-result-limited-desktop.png`（图片真的显示、逐项检查四态、使用限制、能力矩阵、无内部词）、`fixture-result-limited-mobile.png`（单列、无横向滚动、按钮可点区域足够）、`fixture-result-not-usable-desktop.png`（红色 alert、选定按钮禁用、没有导出链接）、`fixture-disabled-desktop.png`、`fixture-photo-not-in-range-mobile.png`。**注意：这些是 fixture 栈上的截图，只证明界面状态，不证明任何真实生成。**

收尾检查：

```bash
lsof -nP -iTCP:32321 -iTCP:32324 -sTCP:LISTEN     # 期望：无输出（脚本自己启动的 Next 与 fixture API 已退出）
pgrep -fl "pw-profile" || echo "no leftover browser"  # 期望：no leftover browser
```

- [ ] **Step 9: 提交**

```bash
cd "$WT" && git add web/src web/tests web/e2e web/package.json web/package-lock.json
git status --short        # 不得出现 web/next-env.d.ts、web/.next、node_modules
git commit -m "feat(web): real upload and a limited-fidelity panel with honest copy

/start 个人路径换成真实文件上传 + 限定保真换背景（旧 first-image 表单退出个人路径，
个人工程靠 ?project= 找回）；source BFF 放行封闭的 plate 请求体与 return 动作，稳定
错误码按各自状态透出；纯展示 PlateLockScreen（主视图零内部词，逐项四态、限制、
能力矩阵）与无障碍原语；旧 model-plate 按钮与 BFF 动作删除。Playwright（fixture 栈，
本机 Chrome 独立 profile）覆盖键盘、焦点、空/错/加载/不可用态与桌面+手机宽度。

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```


---

## Task 6：真实付费隔离验收（M1–M3）、SIMULATED-HUMAN、模式收口、runbook、handoff 与清理

**交付物：** 一套**不会撒谎**的验收驱动与证据包：账本先意图后观测、构建身份预检、故障注入、零新增费用的恢复探针、导出与展示结果的逐字节核对；真实前置缺失时如实 `BLOCKED`（不用 fixture 补数）；六层结论互不折叠的 verdict 推导器；runbook；`RUN/handoffs/HUI-2232.md`；收尾清理。对应 spec §4.9、§4.10、§8、A8、A9、A11、A12。

**Files:**
- Create: `web/e2e/{ledger.mjs,fault-proxy.mjs,review-validate.mjs,verdict.mjs,real-run.mjs}`、`web/tests/e2e-helpers.test.ts`、`docs/runbooks/hui-2232-plate-lock.md`
- Modify: `web/package.json`（加 `e2e:real` 脚本）
- 仓库外产出：`$EV/**` 证据、`$RUN/handoffs/HUI-2232.md`

**Interfaces:**
- Consumes：Task 1–5 全部；Task 5 的 `web/e2e/zip.mjs`；真实栈前置（**只能是环境变量 / 文件路径引用**，名字见 Step 6）。
- Produces：`$EV/real-e2e/run-record.json`（`layer:"real"`）**或** `$EV/real-e2e/BLOCKED.json`；`charge-ledger.jsonl`（每个 `observed` 之前必有同 label 的 `intent`）；`$EV/gates.json`；`verdict.mjs` 输出的六层表（Engineering / Browser / Service-Provider / Billing / Production / Human）与 verdict。

- [ ] **Step 1: 先写失败的测试（证据工具本身也要被测）**

覆盖：ZIP 往返逐字节一致；账本拒绝“没有先写意图的观测”、把“有意图没观测”标为 `UNKNOWN`、只统计 `charged`；SIMULATED-HUMAN 评审**只有**在“不声称任何更多”时才被接受（拒绝 `kind` 不对、上下文非 fresh、带 `verdict/pass` 字段、轴意见取值越界、声称真人确认）；verdict 推导器**永远不会高于证据**（门禁有一项非 0 → `PARTIAL`；fixture 干跑的记录不算真实；真实 + 账本完整但无评审 → `PARTIAL`；真实 + 账本完整 + 有效评审 → `DONE_WITH_SIMULATION`；`production` 恒 `NOT_RUN`）：

`web/tests/e2e-helpers.test.ts`

````ts
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { readStoredZip, writeStoredZip } from "../e2e/zip.mjs";
import { ledgerProblems, openLedger, readLedger, spentMinor } from "../e2e/ledger.mjs";
import { validateReview } from "../e2e/review-validate.mjs";
import { assess } from "../e2e/verdict.mjs";

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), "hui2232-"));
const sha = "c".repeat(64);
const goodReview = () => ({ schema: "review.simulated_human.v1", kind: "SIMULATED-HUMAN", reviewer_context: "fresh", items: [{ case_id: "carton-1024x1024", composite_sha256: sha, axes: { background_extra_objects: { opinion: "cannot_tell" }, visual_composite_quality: { opinion: "looks_acceptable" } } }] });

describe("evidence helpers", () => {
  it("stored zip round trips every entry byte for byte", () => {
    const files = { "image.png": Buffer.from([137, 80, 78, 71, 0, 255]), "quality.json": Buffer.from('{"report_version":"product-source-quality/v2"}'), "snapshot.json": Buffer.from("{}") };
    const read = readStoredZip(writeStoredZip(files));
    expect([...read.keys()]).toEqual(Object.keys(files));
    for (const [name, data] of Object.entries(files)) expect(Buffer.compare(read.get(name)!, data)).toBe(0);
    expect(() => readStoredZip(Buffer.from("not a zip"))).toThrow();
  });

  it("the ledger refuses an observation without a prior intent and flags an unobserved intent as unknown", () => {
    const dir = tmp(), l = openLedger(path.join(dir, "ledger.jsonl"));
    l.observed("A", { payment_status: "charged", charged_minor: "25" });
    expect(ledgerProblems(readLedger(l.file)).join("|")).toContain("observation without a prior intent: A");
    const dir2 = tmp(), l2 = openLedger(path.join(dir2, "ledger.jsonl"));
    l2.intent("B", { expected_amount_minor: "25" });
    expect(ledgerProblems(readLedger(l2.file)).join("|")).toContain("intent never observed (outcome UNKNOWN): B");
    l2.observed("B", { payment_status: "charged", charged_minor: "25" });
    l2.intent("C", {}); l2.observed("C", { payment_status: "held", charged_minor: null });
    const entries = readLedger(l2.file);
    expect(ledgerProblems(entries)).toEqual([]);
    expect(spentMinor(entries)).toBe(25);
    expect(entries.every(e => e.phase === "intent" || e.supplier_expense_ledger_verified === false)).toBe(true);
  });

  it("a SIMULATED-HUMAN review is accepted only when it claims nothing more", () => {
    expect(validateReview(goodReview())).toEqual([]);
    const bad: Array<[string, (d: Record<string, unknown>) => void]> = [
      ["kind", d => { d.kind = "HUMAN"; }], ["context", d => { d.reviewer_context = "same"; }], ["empty items", d => { d.items = []; }],
      ["verdict field", d => { (d.items as Array<Record<string, unknown>>)[0].verdict = "PASS"; }],
      ["axis opinion", d => { (d.items as Array<{ axes: Record<string, unknown> }>)[0].axes.visual_composite_quality = { opinion: "PASS" }; }],
      ["real person claim", d => { d.note = "a real person confirmed this"; }],
    ];
    for (const [name, mutate] of bad) { const d = goodReview() as Record<string, unknown>; mutate(d); expect(validateReview(d).length, name).toBeGreaterThan(0); }
  });

  it("the verdict never rises above what the evidence supports", () => {
    const dir = tmp();
    fs.mkdirSync(path.join(dir, "real-e2e"), { recursive: true });
    const write = (f: string, v: unknown) => fs.writeFileSync(path.join(dir, f), JSON.stringify(v));
    const gates = { go_test: 0, go_vet: 0, go_race: 0, web_test: 0, web_tsc: 0, next_build: 0, samplegen_check: 0 };
    write("gates.json", { ...gates, go_race: 1 });
    expect(assess(dir).verdict).toBe("PARTIAL");
    write("gates.json", gates);
    expect(assess(dir).layers.browser_real).toBe("NOT_RUN");
    expect(assess(dir).verdict).toBe("PARTIAL");
    write("real-e2e/BLOCKED.json", { reason: "no_storage_state" });
    expect(assess(dir).layers.browser_real).toBe("BLOCKED");
    fs.rmSync(path.join(dir, "real-e2e", "BLOCKED.json"));
    // A dry run on the fixture stack never counts as real evidence.
    const run = (layer: string) => ({ layer, runs: [1, 2, 3].map(i => ({ layer, task_id: "t" + i, plate: { asset_id: "a" + i } })) });
    write("real-e2e/run-record.json", run("fixture-dry-run"));
    expect(assess(dir).layers.browser_real).toBe("NOT_RUN");
    const l = openLedger(path.join(dir, "real-e2e", "charge-ledger.jsonl"));
    for (const label of ["a", "b", "c"]) { l.intent(label, {}); l.observed(label, { payment_status: "charged", charged_minor: "25" }); }
    write("real-e2e/run-record.json", run("real"));
    expect(assess(dir).layers.browser_real).toBe("PASS");
    expect(assess(dir).layers.production).toContain("NOT_RUN");
    expect(assess(dir).verdict).toBe("PARTIAL"); // no simulated review yet
    write("review.simulated_human.v1.json", goodReview());
    const done = assess(dir);
    expect(done.verdict).toBe("DONE_WITH_SIMULATION");
    expect(done.layers.human).toContain("SIMULATED-HUMAN");
    expect(done.layers.billing).toContain("not audited");
  });
});
````

- [ ] **Step 2: 运行，确认失败**

```bash
cd "$WT/web" && npx vitest run tests/e2e-helpers.test.ts 2>&1 | grep -E "Test Files|Tests |Error|Failed to resolve" | head -5
```

期望：`FAIL`（无法解析 `../e2e/ledger.mjs` 等；`zip.mjs` 已在 Task 5 存在）。

- [ ] **Step 3: 写实现——证据工具**

账本（追加写；供应商成本恒为“声明值、未核账”）：

`web/e2e/ledger.mjs`

````js
// Append-only charge ledger. A paid click is only allowed after its `intent`
// line exists; the matching `observed` line records what the platform reported.
// Supplier cost is only ever a DECLARED value: it is never presented as audited.
import fs from "node:fs";
import path from "node:path";

export function openLedger(file) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const append = (entry) => fs.appendFileSync(file, JSON.stringify({ at: new Date().toISOString(), ...entry }) + "\n");
  return {
    file,
    intent: (label, fields) => append({ phase: "intent", label, ...fields }),
    observed: (label, fields) => append({ phase: "observed", label, supplier_expense_ledger_verified: false, supplier_cost_basis: "declared_only", ...fields }),
  };
}

export const readLedger = (file) => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);

// ledgerProblems returns human-readable defects: an observation with no earlier
// intent, an intent that was never observed (the outcome is then UNKNOWN), or
// two intents with one label.
export function ledgerProblems(entries) {
  const problems = [], intents = new Map();
  entries.forEach((e, i) => {
    if (e.phase === "intent") { if (intents.has(e.label)) problems.push(`duplicate intent for ${e.label}`); intents.set(e.label, i); }
    else if (e.phase === "observed") { if (!intents.has(e.label) || intents.get(e.label) > i) problems.push(`observation without a prior intent: ${e.label}`); intents.set(`${e.label}#observed`, i); }
  });
  for (const e of entries) if (e.phase === "intent" && !intents.has(`${e.label}#observed`)) problems.push(`intent never observed (outcome UNKNOWN): ${e.label}`);
  return problems;
}
export const spentMinor = (entries) => entries.filter((e) => e.phase === "observed" && e.payment_status === "charged").reduce((n, e) => n + Number(e.charged_minor ?? 0), 0);
````

SIMULATED-HUMAN 评审校验（只作参考，不带结论）：

`web/e2e/review-validate.mjs`

````js
// Validates a SIMULATED-HUMAN review written by an independent reviewer agent.
// A review is advisory evidence only: it can never carry a verdict, and it must
// not claim to be a real person.
import fs from "node:fs";

const AXES = ["background_extra_objects", "background_intent_followed", "visual_composite_quality", "transmission_edge"];
const OPINIONS = ["looks_acceptable", "has_concerns", "cannot_tell"];

export function validateReview(doc) {
  const problems = [];
  if (doc?.schema !== "review.simulated_human.v1") problems.push("schema must be review.simulated_human.v1");
  if (doc?.kind !== "SIMULATED-HUMAN") problems.push("kind must be exactly SIMULATED-HUMAN");
  if (doc?.reviewer_context !== "fresh") problems.push("reviewer_context must be fresh (an independent, new context)");
  if (!Array.isArray(doc?.items) || doc.items.length === 0) problems.push("items must be a non-empty array");
  for (const [i, it] of (doc?.items ?? []).entries()) {
    if (!/^[a-z0-9_-]+-[0-9]+x[0-9]+$/.test(it?.case_id ?? "")) problems.push(`items[${i}].case_id`);
    if (!/^[0-9a-f]{64}$/.test(it?.composite_sha256 ?? "")) problems.push(`items[${i}].composite_sha256`);
    for (const a of Object.keys(it?.axes ?? {})) if (!AXES.includes(a) || !OPINIONS.includes(it.axes[a].opinion)) problems.push(`items[${i}].axes.${a}`);
    if (JSON.stringify(it).match(/"(verdict|pass|passed|approved|human_usefulness)"/i)) problems.push(`items[${i}] carries a verdict-like field`);
  }
  if (/real (human|person|user)|真人(已)?(确认|验证|采用)/i.test(JSON.stringify(doc))) problems.push("must not claim a real person reviewed it");
  return problems;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  const problems = validateReview(JSON.parse(fs.readFileSync(process.argv[2], "utf8")));
  console.log(problems.length ? `INVALID\n- ${problems.join("\n- ")}` : "VALID (SIMULATED-HUMAN, advisory only)");
  process.exit(problems.length ? 1 : 0);
}
````

verdict 推导器（只会拒绝抬高，不会抬高）：

`web/e2e/verdict.mjs`

````js
// Computes the six-layer table and the verdict the evidence supports. It cannot
// raise a verdict; it can only refuse to. Layers are never merged.
import fs from "node:fs";
import path from "node:path";
import { readLedger, ledgerProblems } from "./ledger.mjs";
import { validateReview } from "./review-validate.mjs";

const read = (f) => (fs.existsSync(f) ? JSON.parse(fs.readFileSync(f, "utf8")) : null);

export function assess(dir) {
  const gates = read(path.join(dir, "gates.json")) ?? {};
  const real = read(path.join(dir, "real-e2e", "run-record.json"));
  const blocked = read(path.join(dir, "real-e2e", "BLOCKED.json"));
  const fixture = read(path.join(dir, "browser", "fixture", "fixture-run-record.json"));
  const review = read(path.join(dir, "review.simulated_human.v1.json"));
  const entries = readLedger(path.join(dir, "real-e2e", "charge-ledger.jsonl"));
  const required = ["go_test", "go_vet", "go_race", "web_test", "web_tsc", "next_build", "samplegen_check"];
  const missingGates = required.filter((g) => gates[g] !== 0);
  const realOk = !!real && real.layer === "real" && real.runs?.length >= 3 && real.runs.every((r) => r.layer === "real");
  const ledgerOk = ledgerProblems(entries).length === 0 && entries.length > 0;
  const reviewProblems = review ? validateReview(review) : null;
  const layers = {
    engineering: missingGates.length === 0 ? "PASS" : `FAIL (${missingGates.join(", ")} not 0)`,
    browser_fixture: fixture ? (fixture.results.every((r) => r.ok) ? "PASS (fixture only)" : "FAIL") : "NOT_RUN",
    browser_real: blocked ? "BLOCKED" : realOk ? "PASS" : "NOT_RUN",
    service_provider: realOk ? (real.runs.every((r) => r.task_id && r.plate?.asset_id) ? "PASS" : "UNKNOWN") : blocked ? "BLOCKED" : "NOT_RUN",
    billing: realOk && ledgerOk ? "PASS (supplier cost declared, not audited)" : blocked ? "BLOCKED" : realOk ? "UNKNOWN (ledger incomplete)" : "NOT_RUN",
    production: "NOT_RUN (needs Root go)",
    human: review && reviewProblems.length === 0 ? "SIMULATED-HUMAN (advisory)" : "NOT_RUN",
  };
  let verdict = "PARTIAL";
  if (missingGates.length > 0) verdict = "PARTIAL";
  else if (realOk && ledgerOk) verdict = review && reviewProblems.length === 0 ? "DONE_WITH_SIMULATION" : "PARTIAL";
  return { layers, verdict, notes: { missingGates, blocked: blocked?.reason ?? null, ledgerProblems: ledgerProblems(entries), reviewProblems } };
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  const out = assess(process.argv[2]);
  console.log(JSON.stringify(out, null, 2));
}
````

故障代理（让产品必须靠原幂等键恢复；只匹配 `POST /internal/v1/tasks`，SDK `task.source_submit` 的真实路径；其余原样转发）：

`web/e2e/fault-proxy.mjs`

````js
// Drops the RESPONSE of selected Task submits so the product must recover the
// original request by its idempotency key. Everything else is relayed untouched.
// FAULT_UPSTREAM=<Task base URL> FAULT_PORT=32323 FAULT_DROP_SUBMITS=2 FAULT_LOG=<file>
import http from "node:http";
import https from "node:https";
import fs from "node:fs";

const upstream = new URL(process.env.FAULT_UPSTREAM ?? "");
const PORT = Number(process.env.FAULT_PORT ?? 32323);
const drop = new Set(String(process.env.FAULT_DROP_SUBMITS ?? "").split(",").filter(Boolean).map(Number));
const log = { submits_seen: 0, dropped: [] };
const save = () => process.env.FAULT_LOG && fs.writeFileSync(process.env.FAULT_LOG, JSON.stringify(log));
save();

http.createServer((req, res) => {
  const submit = req.method === "POST" && new URL(req.url, "http://x").pathname === "/internal/v1/tasks";
  const n = submit ? ++log.submits_seen : 0;
  const lib = upstream.protocol === "https:" ? https : http;
  const out = lib.request({ protocol: upstream.protocol, hostname: upstream.hostname, port: upstream.port, method: req.method, path: upstream.pathname.replace(/\/$/, "") + req.url, headers: { ...req.headers, host: upstream.host } }, (up) => {
    const chunks = [];
    up.on("data", (c) => chunks.push(c));
    up.on("end", () => {
      if (submit && drop.has(n)) { log.dropped.push(n); save(); req.socket.destroy(); return; } // the platform already acted; the caller never hears back
      res.writeHead(up.statusCode ?? 502, up.headers);
      res.end(Buffer.concat(chunks));
      save();
    });
  });
  out.on("error", () => { res.writeHead(502); res.end(); });
  req.pipe(out);
}).listen(PORT, "127.0.0.1", () => console.log(`fault proxy on ${PORT} -> ${upstream.origin}, dropping submit responses ${[...drop].join(",") || "none"}`));
````

真实栈驱动（M1 三个付费 run、M3 恢复探针、M2 文字生成；**不键入任何凭据**，登录态只来自 Root 提供的 storage state；预检要求构建身份与 `git rev-parse HEAD` 一致且工作区干净、`/readyz` 的 `plate_lock.usable`、有预算；每次付费点击前先写 `intent`；`REAL_DRY_RUN_FIXTURE=1` 时对 fixture 栈干跑同一套步骤用来验证驱动本身，产物标 `fixture-dry-run`）：

`web/e2e/real-run.mjs`

````js
// REAL-STACK acceptance driver (HUI-2232 M1-M3). It drives the normal product UI
// in a real headless Chrome (own profile, own ports) against a stack the
// operator already started: real Go server + real Next web + Root-provided QA
// platform. It never types credentials: login state comes from a Root-provided
// Playwright storage-state file (E2E_STORAGE_STATE) => login is ROOT_ASSISTED.
//
// REAL_DRY_RUN_FIXTURE=1 runs the very same steps against the FIXTURE stack so
// the driver itself can be verified for free. Its output is then labelled
// `fixture-dry-run` and is never acceptance evidence.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";
import { openLedger, readLedger, ledgerProblems, spentMinor } from "./ledger.mjs";
import { readStoredZip } from "./zip.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const webDir = path.resolve(here, "..");
const DRY = process.env.REAL_DRY_RUN_FIXTURE === "1";
const LAYER = DRY ? "fixture-dry-run" : "real";
const WEB = process.env.REAL_WEB_URL ?? "http://localhost:32321"; // always "localhost": the source BFF compares Origin with it
const API = process.env.REAL_API_URL ?? "http://127.0.0.1:32320";
const need = (n) => { const v = process.env[n]; if (!v) { console.error(`${n} is required`); process.exit(2); } return v; };
const OUT = path.join(need("E2E_OUT"), "real-e2e"), PROFILE = need("E2E_PROFILE"), SAMPLES = need("FIXTURE_SAMPLES_DIR");
const CHROME = process.env.E2E_CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const BUDGET = Number(process.env.E2E_BUDGET_MINOR ?? "0"); // remaining minor units Root supplied; 0 = unknown => refuse to spend
const RUN_TEXT = process.env.REAL_RUN_TEXT === "1" || (!DRY && process.env.REAL_RUN_TEXT !== "0");
const FAULT_LOG = process.env.REAL_FAULT_LOG;
const FAULT_RUN = Number(process.env.REAL_FAULT_RUN_INDEX ?? "1"); // which M1 run goes through the fault proxy
const WAIT_MS = Number(process.env.REAL_WAIT_MS ?? String(8 * 60_000));
const manifest = JSON.parse(fs.readFileSync(path.join(SAMPLES, "manifest.json"), "utf8"));
const sha = (b) => crypto.createHash("sha256").update(b).digest("hex");
const VIEWPORTS = { desktop: { width: 1280, height: 800 }, mobile: { width: 390, height: 844 } };
fs.mkdirSync(path.join(OUT, "exports"), { recursive: true });
const ledger = openLedger(path.join(OUT, "charge-ledger.jsonl"));

function stop(reason, details, code = 3) {
  fs.writeFileSync(path.join(OUT, "BLOCKED.json"), JSON.stringify({ layer: LAYER, reason, details, at: new Date().toISOString(), note: "No fixture value stands in for a missing prerequisite." }, null, 2));
  console.error(`BLOCKED: ${reason}`); process.exit(code);
}
const git = (...a) => execFileSync("git", a, { cwd: webDir }).toString().trim();

// ---- preflight -------------------------------------------------------------
let cookies;
if (DRY) cookies = [{ name: "pia_access", value: "fixture-access", domain: "localhost", path: "/", httpOnly: false, secure: false, sameSite: "Lax", expires: -1 }];
else {
  const state = process.env.E2E_STORAGE_STATE;
  if (!state || !fs.existsSync(state)) stop("no_storage_state", { need: "E2E_STORAGE_STATE (Root-provided Playwright storage state; login is ROOT_ASSISTED)" });
  cookies = JSON.parse(fs.readFileSync(state, "utf8")).cookies ?? [];
  if (!cookies.length) stop("empty_storage_state", {});
}
const frontendSha = (() => { try { return git("rev-parse", "HEAD"); } catch { return "unknown"; } })();
const dirty = (() => { try { return git("status", "--porcelain").length > 0; } catch { return true; } })();
let ready = null;
if (!DRY) {
  try { ready = await (await fetch(`${API}/readyz`)).json(); } catch { stop("backend_unreachable", { api: API }); }
  if (!ready.ok || ready.gates?.plate_lock?.usable !== true) stop("plate_lock_not_ready", { fatal: ready.fatal, plate_lock: ready.gates?.plate_lock });
  if (ready.build?.vcs_revision !== frontendSha || ready.build?.vcs_modified || dirty) stop("build_identity_mismatch", { backend: ready.build, frontend: frontendSha, tree_dirty: dirty });
  if (!(BUDGET > 0)) stop("no_budget_supplied", { need: "E2E_BUDGET_MINOR (remaining minor units Root granted)" });
}

// ---- helpers ---------------------------------------------------------------
async function session(name, vp = "desktop") {
  const dir = path.join(PROFILE, `real-${name}-${vp}`); fs.rmSync(dir, { recursive: true, force: true });
  const ctx = await chromium.launchPersistentContext(dir, { executablePath: CHROME, headless: true, viewport: VIEWPORTS[vp], args: ["--no-first-run", "--disable-extensions", "--disable-sync"] });
  await ctx.addCookies(cookies.map((c) => (DRY ? c : { ...c })));
  return { ctx, page: ctx.pages()[0] ?? (await ctx.newPage()) };
}
const jget = async (ctx, p) => { const r = await ctx.request.get(WEB + p); return { status: r.status(), body: await r.json().catch(() => null) }; };
const jpost = async (ctx, p, body) => { const r = await ctx.request.post(WEB + p, { data: body, headers: { origin: WEB, "content-type": "application/json" } }); return { status: r.status(), body: await r.json().catch(() => null) }; };
const balance = async (ctx) => { const r = await jget(ctx, "/api/billing/balance"); return r.status === 200 ? { balance_cny: String(r.body?.balance_cny ?? ""), items: (r.body?.items ?? []).length } : { unavailable: r.status }; };
const urlParam = (page, k) => new URL(page.url()).searchParams.get(k);
const shot = async (page, name) => { const f = `${LAYER}-${name}.png`; await page.screenshot({ path: path.join(OUT, f), fullPage: true }); return f; };
async function waitDerived(ctx, project, run) {
  const end = Date.now() + WAIT_MS; let last;
  while (Date.now() < end) {
    last = (await jget(ctx, `/api/projects/${project}/source-image-runs/${run}`)).body;
    if (last?.plate && last.plate.derivation_state !== "pending") return { view: last, timedOut: false };
    if (["refunded", "released"].includes(last?.payment?.status)) return { view: last, timedOut: false };
    await new Promise((r) => setTimeout(r, 3000));
  }
  return { view: last, timedOut: true };
}
const facts = (v) => ({ run_id: v.run_id, project_id: v.project_id, request_key: v.request_key, usage_id: v.quote?.usage_id, quote_id: v.quote?.quote_id, task_id: v.task_id, hold_id: v.payment?.hold_id ?? null, charge_id: v.payment?.original_charge_id ?? v.payment?.charge_id ?? null, amount_minor: v.quote?.amount_minor, charged_minor: v.payment?.charged_minor, payment_status: v.payment?.status, candidate_state: v.plate?.candidate_state, plate: v.plate?.plate_task ?? null, composite_sha256: v.plate?.composite_sha256 ?? null, report_sha256: v.plate?.report_sha256 ?? null, verdicts: v.plate?.verdicts ?? null });

// ---- M1: paid, limited-fidelity, through the normal UI ----------------------
const plan = [["carton-1024x1024"], ["handled_metal-1024x1280"], ["glass_bottle-768x1024"]];
const runs = [];
async function plateRun(index, caseId) {
  const label = `M1-${caseId}`;
  const c = manifest.cases.find((x) => x.case_id === caseId); const { ctx, page } = await session(label);
  try {
    await page.goto(`${WEB}/start`);
    await page.locator("#photo-file").setInputFiles(path.join(SAMPLES, c.original.path));
    await page.getByRole("button", { name: "上传并使用" }).click();
    await page.getByTestId("plate-form").waitFor({ timeout: 90_000 });
    const project = urlParam(page, "project");
    if (index === 0) fs.writeFileSync(path.join(OUT, "capabilities-snapshot.json"), JSON.stringify((await jget(ctx, `/api/projects/${project}/source-image-capabilities`)).body, null, 2));
    await page.getByLabel("背景描述").fill(c.intents[0]);
    await page.getByRole("button", { name: "获取报价" }).click();
    await page.getByTestId("plate-quote").waitFor({ timeout: 90_000 });
    const runId = urlParam(page, "plate_run");
    const before = (await jget(ctx, `/api/projects/${project}/source-image-runs/${runId}`)).body;
    const amount = Number(before.quote.amount_minor), bal0 = await balance(ctx);
    if (!DRY && spentMinor(readLedger(ledger.file)) + amount > BUDGET) { return { label, skipped: "budget", would_spend_minor: amount }; }
    ledger.intent(label, { case_id: caseId, project_id: project, run_id: runId, usage_id: before.quote.usage_id, quote_id: before.quote.quote_id, expected_amount_minor: String(amount), balance_before: bal0, fault_injected: index === FAULT_RUN && !!FAULT_LOG });
    const screens = [await shot(page, `${label}-quote-desktop`)];
    await page.getByTestId("plate-confirm").click();
    const { view, timedOut } = await waitDerived(ctx, project, runId);
    const bal1 = await balance(ctx);
    ledger.observed(label, { ...facts(view), timed_out: timedOut, balance_after: bal1 });
    if (timedOut || !view.plate || view.plate.derivation_state === "pending") return { label, layer: LAYER, outcome: "UNKNOWN", ...facts(view), screenshots: screens };
    await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 60_000 });
    screens.push(await shot(page, `${label}-result-desktop`));
    await page.setViewportSize(VIEWPORTS.mobile); screens.push(await shot(page, `${label}-result-mobile`)); await page.setViewportSize(VIEWPORTS.desktop);
    let exportCheck = null, selected = false;
    if (view.plate.candidate_state === "limited_candidate") {
      await page.getByTestId("plate-select").click(); await page.getByRole("button", { name: "已选定" }).waitFor({ timeout: 60_000 }); selected = true;
      const res = await ctx.request.get(`${WEB}/api/projects/${project}/source-image-runs/${runId}/export`); assert.equal(res.status(), 200, "export");
      const buf = await res.body(); fs.writeFileSync(path.join(OUT, "exports", `${label}.zip`), buf);
      const zip = readStoredZip(buf), image = zip.get("image.png"), quality = JSON.parse(zip.get("quality.json").toString());
      exportCheck = { image_sha_matches_view: sha(image) === view.plate.composite_sha256, report_version: quality.report_version, report_bound_to_image: quality.facts?.composite_sha256 === sha(image), entries: [...zip.keys()] };
      assert.ok(exportCheck.image_sha_matches_view && exportCheck.report_bound_to_image && exportCheck.report_version === "product-source-quality/v2", "export must be the displayed composite with its frozen report");
    }
    const fault = index === FAULT_RUN && FAULT_LOG && fs.existsSync(FAULT_LOG) ? JSON.parse(fs.readFileSync(FAULT_LOG, "utf8")) : null;
    return { label, layer: LAYER, outcome: "OBSERVED", ...facts(view), quote_fingerprint: before.quote.quote_fingerprint, selected, export: exportCheck, fault, balance_before: bal0, balance_after: bal1, screenshots: screens, frontend_git_sha: frontendSha };
  } finally { await ctx.close(); }
}

// ---- M3: zero-new-cost recovery probes on a finished run ---------------------
async function probes(r0) {
  const { ctx, page } = await session("probes");
  try {
    const base = `/api/projects/${r0.project_id}/source-image-runs/${r0.run_id}`;
    const snap = async () => { const v = (await jget(ctx, base)).body, list = (await jget(ctx, `/api/projects/${r0.project_id}/source-image-runs`)).body; return { ...facts(v), runs_in_project: list.runs.length, balance: await balance(ctx) }; };
    const baseline = await snap(), out = [];
    const open = async (p) => { await p.goto(`${WEB}/start?project=${r0.project_id}&plate_run=${r0.run_id}`); await p.locator('[data-testid="plate-result"]').waitFor({ timeout: 90_000 }); };
    const steps = [
      ["confirm_again_with_the_original_quote", () => jpost(ctx, `${base}/confirm`, { quote_id: r0.quote_id, quote_fingerprint: r0.quote_fingerprint })],
      ["reconcile", () => jpost(ctx, `${base}/reconcile`, {})],
      ["reload_page", async () => { await open(page); await page.reload(); await page.locator('[data-testid="plate-result"]').waitFor({ timeout: 90_000 }); }],
      ["relogin_new_browser_context", async () => { const s2 = await session("probes-relogin"); try { await open(s2.page); } finally { await s2.ctx.close(); } }],
    ];
    for (const [name, step] of steps) { await step(); const after = await snap(); out.push({ name, equal: JSON.stringify(after) === JSON.stringify(baseline), before: baseline, after }); }
    return out;
  } finally { await ctx.close(); }
}

// ---- M2: one paid ordinary text image through the existing text panel ---------
async function textRun() {
  const label = "M2-text_generate"; const { ctx, page } = await session(label);
  try {
    await page.goto(`${WEB}/start`);
    await page.locator("summary", { hasText: "普通文字生成" }).click();
    await page.getByRole("button", { name: /创建个人普通概念图工程/ }).click();
    await page.getByLabel("文字描述").waitFor({ timeout: 90_000 });
    await page.getByLabel("文字描述").fill("蓝色纸盒，浅灰棚拍");
    await page.getByRole("button", { name: "获取报价" }).click();
    const confirm = page.getByRole("button", { name: /确认支付并生成/ }); await confirm.waitFor({ timeout: 90_000 });
    const text = await confirm.innerText(); const amount = Math.round(Number(text.match(/¥([0-9.]+)/)?.[1] ?? "0") * 100), bal0 = await balance(ctx);
    if (!DRY && spentMinor(readLedger(ledger.file)) + amount > BUDGET) return { label, skipped: "budget", would_spend_minor: amount };
    ledger.intent(label, { expected_amount_minor: String(amount), balance_before: bal0 });
    await confirm.click();
    const img = page.locator("figure img"); await img.first().waitFor({ timeout: WAIT_MS });
    const ok = await img.first().evaluate((i) => i.complete && i.naturalWidth > 0);
    const bal1 = await balance(ctx); ledger.observed(label, { payment_status: "see_view", balance_after: bal1, image_loaded: ok });
    return { label, layer: LAYER, outcome: ok ? "OBSERVED" : "UNKNOWN", image_loaded: ok, fidelity_claim: "none (ordinary concept image)", screenshot: await shot(page, `${label}-result`) };
  } finally { await ctx.close(); }
}

// ---- go ----------------------------------------------------------------------
let textRunRecord = null, probeRecord = [];
for (const [i, [caseId]] of plan.entries()) { const r = await plateRun(i, caseId); runs.push(r); if (r.skipped === "budget") break; }
const finished = runs.find((r) => r.outcome === "OBSERVED");
if (finished) probeRecord = await probes(finished);
if (RUN_TEXT) textRunRecord = await textRun();
const entries = readLedger(ledger.file);
const record = {
  layer: LAYER, frontend_git_sha: frontendSha, tree_dirty: dirty, backend_build: ready?.build ?? null, login: DRY ? "fixture cookie" : "ROOT_ASSISTED", ready_gates: ready?.gates?.plate_lock ?? null,
  runs, probes: probeRecord, text_run: textRunRecord, spent_minor: spentMinor(entries), ledger_problems: ledgerProblems(entries),
  supplier_expense_ledger_verified: false, notes: ["supplier cost is only a declared SKU value; no audited supplier ledger was available", "human usefulness: NOT_RUN"],
};
fs.writeFileSync(path.join(OUT, "run-record.json"), JSON.stringify(record, null, 2));
const bad = runs.some((r) => r.outcome !== "OBSERVED") || probeRecord.some((p) => !p.equal) || record.ledger_problems.length > 0;
console.log(JSON.stringify({ layer: LAYER, runs: runs.map((r) => ({ label: r.label, outcome: r.outcome ?? r.skipped, candidate: r.candidate_state })), probes: probeRecord.map((p) => ({ name: p.name, equal: p.equal })), spent_minor: record.spent_minor }, null, 2));
process.exit(bad ? 1 : 0);
````

```bash
cd "$WT/web" && node -e "const fs=require('fs'); const p=JSON.parse(fs.readFileSync('package.json','utf8')); p.scripts['e2e:real']='node e2e/real-run.mjs'; fs.writeFileSync('package.json', JSON.stringify(p,null,2)+'\n')"
git -C "$WT" diff -- web/package.json | grep -E "^[+-] " 
```

runbook（仓库内文档：开关、回退、构建身份、验收流程、证据目录、其它模式收口；**不含任何凭据值**）：

`docs/runbooks/hui-2232-plate-lock.md`

````markdown
# HUI-2232 限定保真换背景（`background_plate_lock`）运行手册

状态：2026-10-02，工程 Gate（C2 轮）。本文只描述**本模式**；它不批准任何生产发布，也不代表“产品图成熟”。

## 1. 它是什么、不是什么

- 是：对**冻结合成样本**（纸盒 / 带把手金属杯 / 玻璃瓶 × 三种画布，仓库内 `fixtures/frozen-samples/v1`）做“只换背景、主体像素逐点锁回”，并给出逐项、四值（通过 / 未通过 / 待核实 / 不适用）的服务端质量报告。
- 不是：任意商品照片的保真承诺；不是“已验证商品图 / 完全保真”（最好的结论叫 `limited_candidate`，并必须同时列出限制）；不是真人验收（`human_usefulness` 恒为 `NOT_RUN`）。
- 付费路径：底板就是一次正式 `text_generate` source Task（同一 Bill 报价 / hold / charge、同一 durable Upload、同一恢复协调器）；合成与质量裁决在产品侧完成，**派生记录写一次、不可变**。已扣费但合成未通过检查时**不自动重做、不再扣费、产品内无退款入口**（报价处已明示）。

## 2. 配置与开关（默认全关）

| 变量 | 默认 | 说明 |
|---|---|---|
| `FEATURE_BG_PLATE_LOCK` | `0` | 总开关。关闭只禁止**新建**与**新确认**；历史读取 / 显示 / 导出 / 选定 / 删除 / 恢复 / cancel / outbox 照常 |
| `PRODUCT_FIDELITY_SAMPLES_DIR` | 空 | 指向包含 `manifest.json` 的样本目录；校验失败只关闭本模式（`sample_set_invalid`），**不会**让进程退出或 `/readyz` 变 503 |
| `FEATURE_SOURCE_IMAGES`、`FEATURE_GENERATION_ENABLED`、`ECO_BILLING_ENABLED`、`PRODUCT_SOURCE_PRICING_VERSION`、`PRODUCT_SOURCE_DOWNLOAD_HOSTS`、四项平台 base / token | — | 同“正式来源生成”（见 `deploy/product-image.env.example`）；本模式要求 source runtime 就绪、付款人为个人 |

`FEATURE_SOURCE_ORG_IMAGES=1` **不会**开放本模式（组织付款 Gate 未完成）。

`/readyz` 的 `gates.plate_lock` 给出 `{enabled, usable, reason, samples_loaded}`；`build` 给出 `{vcs_revision, vcs_modified}`。能力端点的原因码（稳定 API）：`plate_lock_disabled`、`sample_set_unconfigured`、`sample_set_invalid`、`plate_org_not_supported`、`source_unconfigured`（含运行时原因）、`project_write_forbidden`、`project_archived`。

## 3. 构建身份（必须显式注入）

Go 在 git worktree 里可能把 `vcs.revision` 盖成**主仓** HEAD。验收与发布构建一律：

```bash
REV=$(git rev-parse HEAD); MOD=$([ -n "$(git status --porcelain)" ] && echo true || echo false)
GOWORK=off go build -buildvcs=false -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=$REV -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=$MOD" -o product-server ./cmd/server
```

`vcs_modified=true` 的构建**不得**作为验收证据。

## 4. 数据与回退

- 迁移 `0020_plate_lock.sql`（冻结输入、写一次的派生）与 `0021_plate_output_quality.sql`（回流质量绑定）只新增表 / 触发器，旧表与旧行不变。
- **回退 = 关 `FEATURE_BG_PLATE_LOCK`，不降级二进制。** 旧二进制无法解析 `background_plate_lock` 的 run 行；一旦存在这类行就不能回滚到旧二进制。回滚不得覆盖任何已扣费事实。
- 派生字节存在产品库 `product_plate_derivations`（读时校验 SHA-256）。用户“删除输出”只隐藏并释放底板引用，**不宣称物理删除、不退款**。
- 样本集在计价之后被换（案例摘要不一致）→ 该 run 的派生终态 `blocked / sample_set_changed`：不下载、不扣费、底板引用保留；需要重新报价。
- 旧直调路由 `POST …/background-replacements/{jobId}/model-plate` 永久 `410 direct_supplier_route_retired`；它不可回退成直调。

## 5. 验收流程（M1–M3，只在隔离环境，所有凭据由 Root 以环境变量 / 文件路径引用提供）

前置（缺任一项 → 对应取证记 `BLOCKED`，**不得用 fixture 补数**）：QA 平台栈（Identity / Billing / Task / Upload，Task 侧已接真实模型与定价）、测试付款人与额度（`E2E_BUDGET_MINOR`）、`PRODUCT_SOURCE_PRICING_VERSION`、`PRODUCT_SOURCE_DOWNLOAD_HOSTS`、预认证的 Playwright 登录状态文件（`E2E_STORAGE_STATE`；登录记为 `ROOT_ASSISTED`，代理**不键入任何密码**）。

- **M1** 三次付费限定保真：`carton-1024x1024`、`handled_metal-1024x1280`、`glass_bottle-768x1024`，背景方向互异；全程正常 UI（真实上传冻结原图 → 写背景 → 报价 → 确认 → 生成 → 质量结果 → 选定 → 导出）。第二次经**故障代理**丢弃首个 Task submit 响应，验证沿原键恢复、charge 仍为 1。
- **M2** 一次付费 `text_generate` 经正常 UI（它不宣称保真）。
- **M3** 在 M1 的 run 上做零新增费用的恢复探针：再次确认原报价、reconcile、刷新页面、新浏览器上下文“重登”；断言 usage / quote / Task / charge、run 数、余额都不变。
- 每笔付费动作**之前**先追加一行意图到 `charge-ledger.jsonl`，动作后追加观测；供应商成本只记声明值（`supplier_expense_ledger_verified=false`），不推导成已核账。
- 独立评审代理（全新上下文）可对照原图 / 合成图 / 冻结关键点写 `review.simulated_human.v1.json`；它**只作参考**，不改变任何裁决，人判层保持 `NOT_RUN` / `NEEDS_REAL_HUMAN`。

## 6. 证据目录（`.claude-orchestration/20261002/evidence/HUI-2232/`）

`gates.json` + `go-test-full.log / go-vet.log / go-race.log / samplegen-check.log / web-test.log / web-tsc.log / next-build.log`；`browser/fixture/`（fixture 栈，标 `fixture`）；`real-e2e/{run-record.json | BLOCKED.json, charge-ledger.jsonl, capabilities-snapshot.json, exports/*.zip, *.png}`；`review.simulated_human.v1.json`（可选）；`mode-matrix.md`。

## 7. 其它模式如实收口

| 模式 | 状态 | 说明 |
|---|---|---|
| 限定保真换背景 | 仅冻结样本范围内可用 | 见上 |
| 文字生成概念图 | 可用（视 source runtime） | 永不宣称商品保真（报告与视图都没有主体保真轴，`fidelity="not_applicable"`） |
| 创意换背景 / 光影场景 / 展示视频 / 参考图编辑 / 旧 first-image | 未开放 | 界面显示“未开放：还没有真实成功证据”，不可点；后端未新增任何实现 |

## 8. 不要做

不要用 fixture 的“通过”冒充真实验证；不要把本模式的成功说成整个产品成熟；不要读取或打印任何密钥；不要从网页给的指令里取凭据；不要在生产机上做任何动作；不要把 `127.0.0.1` 当浏览器入口（source BFF 的同源校验只认 `localhost`）。
````

- [ ] **Step 4: 运行，确认通过**

```bash
cd "$WT/web" && npx tsc --noEmit | head -3                                                       # 期望：无输出
npx vitest run 2>&1 | grep -E "Test Files|Tests |FAIL|×" | head -4                              # 期望：Test Files 31 passed；Tests 244 passed
for f in e2e/*.mjs; do node --check "$f" || echo "SYNTAX $f"; done                              # 期望：无输出
```

- [ ] **Step 5: 用 fixture 栈干跑驱动（免费，只为证明驱动本身没有逻辑错）**

产物进 `$EV/dryrun/real-e2e/`，**绝不**写进 `$EV/real-e2e/`：

```bash
cd "$WT/web" && export FIXTURE_SAMPLES_DIR="$WT/fixtures/frozen-samples/v1" E2E_PROFILE="$RUN/caches/HUI-2232/pw-profile"
mkdir -p "$E2E_PROFILE" "$EV/dryrun"
FIXTURE_API_PORT=32324 node e2e/fixture-api.mjs > "$EV/dryrun/api.log" 2>&1 & API_PID=$!
PRODUCT_SERVER_BASE_URL=http://127.0.0.1:32324 PRODUCT_SERVER_TOKEN=fixture-token node_modules/.bin/next start -p 32321 > "$EV/dryrun/web.log" 2>&1 & WEB_PID=$!
sleep 6; curl -s -X POST -d '{"name":"limited","reset":true}' http://127.0.0.1:32324/__fixture/scenario
REAL_DRY_RUN_FIXTURE=1 REAL_WAIT_MS=60000 E2E_OUT="$EV/dryrun" node e2e/real-run.mjs 2>&1 | tee "$EV/dryrun/real-run.log"; echo "exit=${PIPESTATUS[0]}"
kill $API_PID $WEB_PID; sleep 1; lsof -nP -iTCP:32321 -iTCP:32324 -sTCP:LISTEN
```

期望：三次 `M1-… outcome: OBSERVED`、`candidate: limited_candidate`；四个探针（`confirm_again_with_the_original_quote`、`reconcile`、`reload_page`、`relogin_new_browser_context`）全部 `"equal": true`；`spent_minor: 75`；退出码 0；`$EV/dryrun/real-e2e/run-record.json` 里 `layer` 是 `"fixture-dry-run"`；`lsof` 无输出。

- [ ] **Step 6: 完整工程门禁（写 `gates.json`，只信退出码）**

```bash
cd "$WT/server"
"$RUN/bin/heavy" -- go test -p 2 -count=1 ./... > "$EV/go-test-full.log" 2>&1; GT=$?
go vet ./... > "$EV/go-vet.log" 2>&1; GV=$?
"$RUN/bin/heavy" -- go test -count=1 -race -run 'F09|F08|F10|F15|F17|F18|F21|F22|Plate|DerivePlate|FlagOff|Readyz|Compose|Fit|Derive' ./internal/platelock ./internal/store ./internal/sourceflow ./internal/httpapi > "$EV/go-race.log" 2>&1; GR=$?   # 慢：约 10 分钟，是预期的
go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1 > "$EV/samplegen-check.log" 2>&1; SG=$?
git -C "$WT" diff --stat bf4ec7c7d8fd08576a8b3ac314798209fd355b90..HEAD -- server/go.mod server/go.sum | tee "$EV/sdk-unchanged.log"; grep -n "public-ai/sdk/go v0.1.1" server/go.mod   # A10：期望 diff 无输出（SDK 与依赖未变），grep 命中 v0.1.1
cd ../web
npx vitest run > "$EV/web-test.log" 2>&1; WT_=$?
npx tsc --noEmit > "$EV/web-tsc.log" 2>&1; WS=$?
"$RUN/bin/heavy" -- npm run build > "$EV/next-build.log" 2>&1; NB=$?
node -e "require('fs').writeFileSync('$EV/gates.json', JSON.stringify({go_test:$GT,go_vet:$GV,go_race:$GR,samplegen_check:$SG,web_test:$WT_,web_tsc:$WS,next_build:$NB,at:new Date().toISOString(),head:'$(git -C "$WT" rev-parse HEAD)'},null,1))"
cat "$EV/gates.json"
```

期望：七项全 `0`；`sdk-unchanged.log` 为空且 `go.mod` 仍是 `public-ai/sdk/go v0.1.1`（A10：SDK 不升级；组织付款路径默认关闭已由 Task 3 的原因码 `plate_org_not_supported` 测试覆盖）。**任何一项非 0 → 停下修，不得继续声称完成。** 然后在**最终提交后的树上**重跑一次 fixture 取证（Task 5 Step 8 的命令），让浏览器证据与最终前端 SHA 对应。

- [ ] **Step 7: 真实栈预检与验收（M1–M3）**

7a. 预检（**只检查变量是否存在，不打印值**；用 `bash`，因为 `${!v}` 是 bash 语法；缺失的**名字**存进 `MISSING`）：

```bash
MISSING=$(bash -c 'for v in PLATFORM_IDENTITY_BASE_URL PLATFORM_IDENTITY_APP_ID PLATFORM_IDENTITY_TOKEN PLATFORM_BILLING_BASE_URL PLATFORM_BILLING_TOKEN PLATFORM_TASK_BASE_URL PLATFORM_TASK_TOKEN PLATFORM_UPLOAD_BASE_URL PLATFORM_UPLOAD_TOKEN PRODUCT_SOURCE_PRICING_VERSION PRODUCT_SOURCE_DOWNLOAD_HOSTS E2E_STORAGE_STATE E2E_BUDGET_MINOR; do [ -n "${!v}" ] || echo "$v"; done; [ -f "$E2E_STORAGE_STATE" ] || echo E2E_STORAGE_STATE_FILE_NOT_FOUND')
echo "missing: ${MISSING:-none}"
```

- **`MISSING` 非空** → 真实栈取证记 `BLOCKED`，**不得用 fixture 或模拟补数**。写入（只含缺失变量的**名字**）后跳到 Step 8：

```bash
mkdir -p "$EV/real-e2e" && node -e "require('fs').writeFileSync('$EV/real-e2e/BLOCKED.json', JSON.stringify({layer:'real', reason:'prerequisites_missing', missing:process.argv.slice(1), at:new Date().toISOString(), note:'No fixture or simulated value stands in for a missing prerequisite.'},null,2))" $MISSING
cat "$EV/real-e2e/BLOCKED.json"
```

- **`MISSING` 为空（全部齐备）** → 先把工作区提交干净（`vcs_modified` 必须是 `false`），再构建并起栈（端口：Go `32320`、Next `32321`、故障代理 `32323`；`PRODUCT_INTERNAL_TOKEN` 随机生成且**不打印**）：

```bash
cd "$WT" && git status --porcelain | head -3        # 必须无输出
R="$RUN/caches/HUI-2232/real"; mkdir -p "$R" "$EV/real-e2e/exports"
REV=$(git rev-parse HEAD)
(cd server && go build -buildvcs=false -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=$REV -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=false" -o "$R/product-server" ./cmd/server)
(cd web && "$RUN/bin/heavy" -- npm run build)
INTERNAL=$(openssl rand -hex 24)
# 故障代理：丢弃第 2 次 Task submit 的响应（M1 的第二个 run）
(cd web && FAULT_UPSTREAM="$PLATFORM_TASK_BASE_URL" FAULT_PORT=32323 FAULT_DROP_SUBMITS=2 FAULT_LOG="$EV/real-e2e/fault-log.json" node e2e/fault-proxy.mjs > "$R/fault.log" 2>&1 & echo $! > "$R/fault.pid")
# Go server：只把 Task base 指向故障代理，其它平台变量沿用进程环境
PRODUCT_SERVER_ADDR=127.0.0.1:32320 PRODUCT_DB_PATH="$R/product.db" PRODUCT_INTERNAL_TOKEN="$INTERNAL" PLATFORM_TASK_BASE_URL=http://127.0.0.1:32323 \
FEATURE_PHOTO_UPLOAD=1 FEATURE_SOURCE_IMAGES=1 FEATURE_GENERATION_ENABLED=1 ECO_BILLING_ENABLED=1 FEATURE_BG_PLATE_LOCK=1 PRODUCT_FIDELITY_SAMPLES_DIR="$WT/fixtures/frozen-samples/v1" \
"$R/product-server" > "$R/server.log" 2>&1 & echo $! > "$R/server.pid"
(cd web && PRODUCT_SERVER_BASE_URL=http://127.0.0.1:32320 PRODUCT_SERVER_TOKEN="$INTERNAL" node_modules/.bin/next start -p 32321 > "$R/web.log" 2>&1 & echo $! > "$R/web.pid")
sleep 6; curl -s http://127.0.0.1:32320/readyz | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['ok'], d['build'], d['gates']['plate_lock'])"
```

期望 `readyz`：`True {'vcs_modified': False, 'vcs_revision': '<等于 git rev-parse HEAD>'} {'enabled': True, 'reason': '', 'samples_loaded': True, 'usable': True}`。`usable` 为 false / 构建身份不符 → 不要跑付费步骤，把 `readyz` 的 `gates.plate_lock` 与 `build` 写进 `BLOCKED.json`（`reason:"plate_lock_not_ready"` 或 `"build_identity_mismatch"`）。

7b. 跑 M1–M3（真实付费，**每笔先登记意图**；预算由 `E2E_BUDGET_MINOR` 约束，不足时驱动会在付费前停下并记 `skipped: budget`）：

```bash
cd "$WT/web" && export E2E_OUT="$EV" E2E_PROFILE="$RUN/caches/HUI-2232/pw-profile" FIXTURE_SAMPLES_DIR="$WT/fixtures/frozen-samples/v1" REAL_FAULT_LOG="$EV/real-e2e/fault-log.json" REAL_FAULT_RUN_INDEX=1
REAL_WEB_URL=http://localhost:32321 REAL_API_URL=http://127.0.0.1:32320 node e2e/real-run.mjs 2>&1 | tee "$EV/real-e2e/real-run.log"; echo "exit=${PIPESTATUS[0]}"
node -e "const l=require('fs').readFileSync('$EV/real-e2e/charge-ledger.jsonl','utf8').trim().split('\n').map(JSON.parse); console.log(l.map(e=>e.phase+' '+e.label+' '+(e.payment_status||e.expected_amount_minor)).join('\n'))"
```

期望（全部前置齐备时）：三次 `OBSERVED`（`candidate_state` 可以是 `limited_candidate` 或 `not_usable_candidate`——**如实记录，不得为了好看重跑 / 换背景 / 放宽阈值**）；第二个 run 的 `fault.dropped == [2]` 且最终仍只有 1 个 `task_id`、1 个 `charge_id`；四个 M3 探针全 `equal:true`；`ledger_problems: []`；`exports/*.zip` 里 `image.png` 的 SHA = 界面展示结果的 `composite_sha256`、`quality.json` 为 `product-source-quality/v2` 且绑定同一合成。若某个 run 超时 / 未知：账本里它只有 `intent` 与 `timed_out:true` 的 `observed`，结论记 `UNKNOWN`，**不得写成通过**。

停进程并确认端口空闲：

```bash
for f in server fault web; do kill "$(cat "$R/$f.pid")" 2>/dev/null; done; sleep 1
lsof -nP -iTCP:32320 -iTCP:32321 -iTCP:32323 -sTCP:LISTEN     # 期望：无输出
```

- [ ] **Step 8: SIMULATED-HUMAN 评审（可选，只作参考）与模式收口**

8a. 准备评审包（原图 / 展示的合成图 / 冻结关键点），请 Root 派一个**全新上下文**的独立评审代理读取并写 `$EV/review.simulated_human.v1.json`（schema `review.simulated_human.v1`、`kind:"SIMULATED-HUMAN"`、`reviewer_context:"fresh"`、`items[{case_id, composite_sha256, axes{background_extra_objects|background_intent_followed|visual_composite_quality|transmission_edge:{opinion: looks_acceptable|has_concerns|cannot_tell}}}]`，**不得带 `verdict/pass` 字段，不得声称真人确认**）。你自己**不能**当这个评审。没有评审就保持人判层 `NOT_RUN` / `NEEDS_REAL_HUMAN`：

```bash
mkdir -p "$EV/review-pack" && cp "$WT"/fixtures/frozen-samples/v1/manifest.json "$EV/review-pack/" && cp "$EV"/real-e2e/exports/*.zip "$EV/review-pack/" 2>/dev/null; ls "$EV/review-pack"
[ -f "$EV/review.simulated_human.v1.json" ] && node "$WT/web/e2e/review-validate.mjs" "$EV/review.simulated_human.v1.json"   # 期望：VALID (SIMULATED-HUMAN, advisory only)
```

8b. 模式收口矩阵 `$EV/mode-matrix.md`（逐项 `disabled / not_passed / available(range)` + 原因，必须与界面、能力端点一致；`capabilities-snapshot.json` 是真实栈上读到的能力端点；没有真实栈时只用 Task 4 / Task 5 的测试输出，并写明“无真实栈快照”）：

```markdown
| 模式 | 能力端点 | 界面 | 证据 |
|---|---|---|---|
| 限定保真换背景 | ready + supported_input_ids（范围=冻结样本） | 可用 / 禁用并给原因 | real-e2e 或 BLOCKED |
| 文字生成概念图 | fidelity=not_applicable | 不宣称保真 | Task 4 `TestOrdinaryTextRun…` |
| 创意换背景 / 光影场景 / 展示视频 / 参考图编辑 / 旧 first-image | disabled（reference_edit: formal_contract_unconfigured） | “未开放：还没有真实成功证据”，无按钮 | `tests/plate-lock-screen.test.tsx`、fixture 截图 `capability-matrix` |
```

- [ ] **Step 9: 推导 verdict，写 handoff**

```bash
node "$WT/web/e2e/verdict.mjs" "$EV" | tee "$EV/verdict.json"
```

verdict 取值**只能是**推导器给出的那一个（章程 §6）：全部标准有真实证据且仅人判为 SIMULATED-HUMAN → `DONE_WITH_SIMULATION`；真实前置缺失 / 超时 / 账本不完整 → `PARTIAL`；不得自行抬高。写 `"$RUN/handoffs/HUI-2232.md"`，必须包含（缺一不可）：

1. `verdict`（来自 `verdict.json`）与六层表（Engineering / Browser(fixture) / Browser(real) / Service-Provider / Billing / Production=`NOT_RUN`（需 Root 上线批准）/ Human=`NOT_RUN` 或 `SIMULATED-HUMAN`），**N/A / UNKNOWN / FAIL / PASS 不混写**。
2. 验收矩阵：票的 8 条原通过条件与 12 条剩余范围（spec §11 的 A1–A12）每条一行 → 证据路径或 `UNMET` / `NEEDS_REAL_HUMAN` / `BLOCKED`。
3. 提交列表：`git -C "$WT" log --oneline bf4ec7c..HEAD`（分支 + SHA）。
4. 命令证据：每条给命令、退出码、关键输出、日期（来自 `$EV` 下的日志）。
5. 费用：`charge-ledger.jsonl` 逐笔（意图 / 观测、金额、charge id、余额前后）；供应商成本只写“声明值、未核账（`supplier_expense_ledger_verified=false`）”；平台补贴与现金分项平台未返回时写 `UNKNOWN`。
6. 遗留项逐条：组织付款 Gate 未完成、真人验收 `NEEDS_REAL_HUMAN`、`order-receipt/v1` 不含质量字段（需跨仓合同扩展，**Root 事项**）、样本是合成硬边而非实物照片、阈值首版冻结值（只能收紧）、生产上线待 Root。
7. 给下游的接口说明：能力端点 / 视图 / `return` / `quality_bindings` 契约（Task 4 Interfaces），以及 `feature flag` 默认关。
8. 需要 Root 做的事：上线批准与部署清单（`FEATURE_BG_PLATE_LOCK`、样本目录、迁移 0020 / 0021、构建身份注入）、Linear 回写。

- [ ] **Step 10: 提交仓库内改动，然后收尾清理**

```bash
cd "$WT" && git add web/e2e web/tests/e2e-helpers.test.ts web/package.json docs/runbooks/hui-2232-plate-lock.md
git status --short      # 只能出现这些路径；不得出现 evidence、caches、*.db、*.zip、next-env.d.ts
git commit -m "test(e2e): add an honest real-stack acceptance driver and evidence tooling

先意图后观测的账本、构建身份预检、故障注入代理、零新增费用恢复探针、导出逐字节核对；
SIMULATED-HUMAN 评审只作参考且不带结论；verdict 推导器只会拒绝抬高。真实前置缺失时
如实 BLOCKED，fixture 干跑只验证驱动本身。附 runbook（开关、回退、验收流程、证据目录）。

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

清理（A12）：停掉自己起的一切进程，删除本票缓存与临时数据，确认端口空闲；**不要删 `$EV` 证据与 `$RUN/handoffs`**：

```bash
for f in "$RUN"/caches/HUI-2232/real/*.pid; do [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null; done
pkill -f "user-data-dir=$RUN/caches/HUI-2232/pw-profile" 2>/dev/null
lsof -nP -iTCP:32320-32329 -sTCP:LISTEN          # 期望：无输出
rm -rf "$RUN/caches/HUI-2232" /tmp/smoke
ls "$RUN/caches/HUI-2232" 2>&1 | head -1          # 期望：No such file or directory
git -C "$WT" status --short                        # 期望：无输出（工作区干净；next-env.d.ts 若出现，删除它：rm web/next-env.d.ts）
```


---

## 覆盖矩阵（自查：spec → 任务 → 证据）

### spec §11 验收标准

| # | 标准 | 任务 | 主要证据 |
|---|---|---|---|
| A1 | 冻结样本集 9 case、清单、`-check`、阈值常量 | 1、2 | `samplegen -check`、`fidelitysamples` / `samplegen` 测试、`TestThresholdCalibration` |
| A2 | 装载器默认关、失败关闭；`PlateCoverage` 退役；能力如实 | 1、4 | 装载层反例、`TestPlateCapabilitiesAreTruthfulAndReadOnly`、`TestPlateLockAssemblyIsOffByDefaultAndFailsClosed`、真实进程冒烟 |
| A3 | 经正式 Task 付费，无直连旁路；旧路由 410 | 3、4 | `TestDerivePlateOncePaidLockedAndIdempotent`（远端写计数）、`TestF21…` |
| A4 | v2 正向裁决、四值不折叠、无分数入口 | 2 | `TestF22…`、`DecodeReport` 拒绝矩阵 |
| A5 | F01–F22 全自动化 | 1–4 | 见下表 |
| A6 | 正常 UI 全路径含键盘 / 焦点 / 空 / 错 / 加载态，刷新重登恢复同一 job，`/start` 上传可用 | 5 | `plate-lock-screen.test.tsx`、fixture 栈 12 个浏览器场景（桌面 + 手机，**fixture**） |
| A7 | 回流：版本与限制不丢，未保真不能升级 | 4 | `TestPlateReturnBinds…`、`…OldRequirementVersion…`、`…UnboundDerivedComposite…`（真实回流栈 Level B：无栈则 `NOT_RUN`） |
| A8 | 真实付费隔离验收 M1–M3 + SIMULATED-HUMAN，每笔有账，成本与补贴分记 | 6 | `real-e2e/`（或如实 `BLOCKED.json`） |
| A9 | 其它模式如实收口，UI 一致 | 4、5、6 | `TestOrdinaryTextRun…`、能力矩阵测试与截图、`mode-matrix.md` |
| A10 | SDK 仍 v0.1.1；组织路径默认关闭 | 3、6 | `plate_org_not_supported` 测试、`sdk-unchanged.log` |
| A11 | spec / plan / runbook / handoff（六层 + verdict） | 6 | 本文件、`docs/runbooks/hui-2232-plate-lock.md`、`handoffs/HUI-2232.md` |
| A12 | 收尾清理 | 6 | Step 10 命令输出 |

### 反例矩阵 F01–F22

| ID | 测试（任务） |
|---|---|
| F01 / F02 / F03 | `fidelitysamples_test.go` 的 “F01 text region cut out of the mask” / “F02 fully transparent mask” / “F03 fully opaque mask”（1） |
| F04 | `TestCreatePlateRefusesBeforeAnyRemoteCall`（3）、`TestPlateCreateRefusesBeforeAnyPaidCall/F04…`（4）、浏览器 `photo-not-in-range`（5） |
| F05 | “F05 one original pixel changed, manifest not re-signed”（1）、`…/F05 registry says frozen but the bytes differ by one pixel`（4） |
| F06 | `TestF06WrongPlateSizeIsNotUsableButStillRecorded`（2）、`TestF07F06UnusablePlatesAreRecordedNotRegenerated/wrong plate size`（3） |
| F07 | `TestThresholdCalibration`（2）、`TestF07F06…/background unchanged`（3）、`TestPlateUnusableCandidate…`（4） |
| F08 | `TestF08UndecodablePlateBytes…`（2，b / c）、`TestF08TamperedDownloadStoresNothingAndKeepsRetrying`（3，a） |
| F09 | `TestF09SixteenConcurrentDerivationsStoreOneRecord`、`TestF09SixteenConcurrentConfirmsAndAdvancesSubmitExactlyOnce`（3）、`TestF09SixteenConcurrentPlateCreatesYieldOneRunAndOneFrozenInput`（3，store）；`-race` 见各任务 |
| F10 | `TestF10InterruptedDownloadIsRetriedWithoutSpending`、`TestF10LostSubmitResponseRecoversThePlateRunWithoutASecondTask`（3）；共享的 Task / Bill 恢复代码继续由既有 `source_image_recovery_test.go` 等覆盖 |
| F11 | `TestPlateStaysInsideItsOwnAccount`（4） |
| F12 | `TestPlateReturnOfAnOldRequirementVersionIsRefused`（4） |
| F13 / F19 | `TestPlateUnusableCandidateIsVisibleButNeverSelectableExportableOrReturnable`（4） |
| F14 | `TestPlateCreateRefusesBeforeAnyPaidCall/F14…`（4）、`plate-lock-bff.test.ts`（5） |
| F15 | `TestF15ChangedPriceProfileCannotReuseAnOldPlateRun`（3）；文本模式的同一校验由既有 `TestSourceQuoteExpiredCannotRepriceSameRun` 等覆盖 |
| F16 | 装载层 “missing / manifest / hash” 反例（1）、`…/F16 rejected sample set`（4）、`TestPlateLockAssembly…`（4） |
| F17 | `TestF17ChangedSampleSetBlocksWithoutSpendingOrDownloading`（3）；Upload 原图是否还在与派生无关（P1） |
| F18 | `TestF18DeriveIsDeterministic`（2）、`TestF18ReopenKeepsRecordAndTwoRunsDeriveEqualBytes`（3） |
| F20 | `TestOrdinaryTextRunNeverCarriesAFidelityClaimAndClosedModesStayClosed`（4）、控制器解码器测试（5） |
| F21 | `TestF21RetiredModelPlateRouteNeverCallsTheModel`（4） |
| F22 | `TestF22StateOfProperty`、`TestF22DecodeReportRejectsScoresAndInconsistentDocuments`（2） |

### 已知的、有意保留的局限（必须原样写进 handoff，不得在界面或文档里粉饰）

1. 样本是**合成硬边**图，蒙版即轮廓；不覆盖真实照片的边缘羽化 / 光晕；玻璃透射与接触阴影不可机检。
2. 背景里的多余物体、背景是否符合所写方向、整体观感**不可机检**，永远是 `UNKNOWN`，由人判（本轮无真人，`NEEDS_REAL_HUMAN`）。
3. 底板已扣费而合成未通过检查时无自动补偿（R11）；彻底解决需要公共“复合输出”合同（方案 C，另票）。
4. `order-receipt/v1` 没有质量字段：限制只经产品侧质量绑定、视图与导出报告传递，回传前界面明示“回执不含限制说明”；向下游传递限制需跨仓合同扩展（Root 事项）。
5. 阈值 `bg-lock-thresholds/v1` 是首版冻结值，真实模型的“白色 / 浅灰”类背景可能因接近原背景色而被判 `background_changed = FAIL`——这是保守方向，**只能有证据地收紧，不能为了让真实 run 通过而放宽**。
6. 供应商成本只有声明值，未核账；平台补贴与现金分项取决于平台是否返回，未返回即 `UNKNOWN`。
