# HUI-2232 图像模型地址与凭证成对

## 决定

文生图、背景、光影各自有凭证环境变量，但没有成对的模型地址。`pairedModelEndpoint` 因此一直返回空，有凭证也不会拨号。

地址只和同一模式的凭证一起使用：

- `PRODUCT_TEXT_IMAGE_MODEL_URL` 配 `PRODUCT_TEXT_IMAGE_MODEL_CREDENTIAL`
- `PRODUCT_BG_MODEL_URL` 配 `PRODUCT_BG_MODEL_CREDENTIAL`
- `PRODUCT_LIGHT_MODEL_URL` 配 `PRODUCT_LIGHT_MODEL_CREDENTIAL`

只有地址、或只有凭证，都不拨号。第一组成对的模式被采用，未成对的地址不被拨号。

回环主机上的 PNG 会记录状态码、长度和 SHA-256，`Passed` 仍为 false，错误类为 `loopback`。状态保持 `真实出图未完成`。`production_generation_passed` 和 `billing_passed` 保持 false。

## 测试

`GOWORK=off go test -count=1 ./internal/engwalk/ ./internal/config/`
