# 冻结授权样本集 v1（HUI-2232 C2）

原创合成商品图，由 server/cmd/samplegen 确定性生成，项目内部授权，仅用于验证“限定保真换背景”。
它们不是实物照片：主体为硬边渲染，蒙版即主体轮廓，不覆盖真实照片的边缘羽化/光晕。

- 3 类商品（纸盒/带把金属杯/玻璃瓶）x 3 个画布（1024x1024、1024x1280、768x1024）= 9 个 case。
- manifest.json 记录每个文件的 sha256（文件字节）与 pixel_sha256（解码像素），改一个像素都会被发现。
- 校验：cd server && go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1
- 重新生成（仅在有意修改样本并升版本时）：go run ./cmd/samplegen -write ../fixtures/frozen-samples/v1
- 已验证范围只到本集合；任意用户照片不在范围内。
