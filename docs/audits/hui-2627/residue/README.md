# HUI-2627 项目页残差（本轮只动工程详情）

不改写 fix2 的结论。fix2 记录的 route 级剩余是 117（table/form 50、unstyled 65、hand_filled 2）。本仓没有 `cmd/uifinish-scan`，本轮没有重跑该命令，所以不把 117 写成 0。

## 本页改了什么

`web/src/app/projects/[id]/page.tsx` 的主路径改为事实列表和定义列表：简报、输出版本、来源、成果、尺寸变体。原来大约在 673、826、864、995、1132 行的五张表还在，但都放进默认折叠的 `details[data-technical="true"]`。素材引用、内部 id、sha256 校验句和回执原文也在这些详情里。默认视图仍有空态和中文状态（已采用 / 未采用 / 状态未知 / 尚未挂接素材 / 暂无输出版本 / 尚未登记成果 / 独立制作）。

保存简报、上传并挂接、挂接素材、登记成果、生成变体、提交生成、读取费用事实都在对应字段前面。请求路径和动词没有改。`sourceTenantId: null` 仍原样保留（fix2 记的 hand_filled 误报）。

样式只在 `project-detail.module.css`，全仓只有这一页 import。本页按钮、输入、选择、摘要和链接的最小命中区是 44px。顶栏「应用 / 个人 / 账号」不在这一页的壳里，1440 下测到 44×32，本轮不能改共享导航。

## 浏览器

fixture，不是验收。`next dev` 18941，`fixture-api` 18942，看完已关，两个端口都已释放。工程 `proj_fx` 的夹具是空输入、空版本。默认视图能看到个人商品图、进行中、电商主图、1024×1280、尚未挂接素材、暂无输出版本、独立制作、尚未登记成果。点「读取费用事实」看到余额 ¥100.00。默认视图没有 table，也没有 sha256 / provider / qwen / run_id / asset_id。有数据时的表只存在于源码的技术详情里，这次夹具没有行，所以没有在浏览器里展开一张有数据的表。没有付费生成。

## 没动的表

- `web/src/app/projects/page.tsx`
- `web/src/app/batches/page.tsx`
- `web/src/app/handoff/page.tsx`
- `web/src/components/SubjectFidelityPanel.tsx`
- `web/src/components/background-replace/Panel.tsx`
- `web/src/components/light-scene/Panel.tsx`

server、handoff.go、campaign、matrixhandoff、showcasevideo、`web/src/lib` 本轮没有改。
