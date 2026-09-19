package platform

// 上传 / 任务 / 计费 client:本票(I0)只做 client + 配置 + fail-closed。
// 调用门槛在 httpapi 层由 ECO_BILLING_ENABLED / FEATURE_GENERATION_ENABLED
// 与配置齐备性把关;真实生成链路由后续 FEAT 票接入。
//
// 端点路径标注 PROVISIONAL:平台 task/upload 的内部端点形状以平台侧实现为准,
// FEAT 票接入时如不一致,只改这里的 path 常量与请求体,不影响上层。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TaskClient platform-task(异步生成任务)客户端。
type TaskClient struct {
	BaseURL string // 例:http://127.0.0.1:18103
	AppID   string
	Token   string // TASK_APP_TOKENS 中本 app 专用
	HTTP    *http.Client
}

// SubmitRequest 提交生成任务(PROVISIONAL 形状,FEAT 票细化)。
type SubmitRequest struct {
	IdempotencyKey string         `json:"idempotency_key"` // 幂等键(app_id 范围内)
	Kind           string         `json:"kind"`            // 任务类型,如 product-image-generate
	Payload        map[string]any `json:"payload"`         // 输入/参数(来源标注 standalone/order/campaign)
}

// SubmitResult 任务受理事实(只存 ID 与状态,资金事实在 billing)。
type SubmitResult struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

func (c *TaskClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Submit 提交任务;不可达/5xx 一律 ErrUnavailable(上层 fail-closed,不伪造成功)。
func (c *TaskClient) Submit(ctx context.Context, req SubmitRequest) (SubmitResult, error) {
	var out SubmitResult
	raw, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/internal/v1/tasks/submit", bytes.NewReader(raw)) // PROVISIONAL
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-App-ID", c.AppID)
	httpReq.Header.Set(internalTokenHeader, c.Token)
	res, err := c.httpClient().Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("%w: task submit: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return out, fmt.Errorf("%w: task submit: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, fmt.Errorf("task submit: status %d: %s", res.StatusCode, string(b))
	}
	return out, json.Unmarshal(b, &out)
}

// TaskStatusResult 任务状态事实(只存 ID 与状态;资金事实在 billing)。
// result 保留原始载荷(成功任务的成果引用/测试链路 data_b64 由上层解释)。
type TaskStatusResult struct {
	TaskID string         `json:"task_id"`
	Status string         `json:"status"` // pending|running|succeeded|failed(以平台为准)
	Result map[string]any `json:"result"`
}

// Status 查询任务状态(HUI-1704 批量收集用;PROVISIONAL 形状,同 Submit 口径:
// 不可达/5xx 一律 ErrUnavailable,上层 fail-closed,不伪造状态)。
func (c *TaskClient) Status(ctx context.Context, taskID string) (TaskStatusResult, error) {
	var out TaskStatusResult
	if strings.TrimSpace(taskID) == "" {
		return out, fmt.Errorf("task status: task_id 为空")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(c.BaseURL, "/")+"/internal/v1/tasks/"+url.PathEscape(taskID), nil) // PROVISIONAL
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("X-App-ID", c.AppID)
	httpReq.Header.Set(internalTokenHeader, c.Token)
	res, err := c.httpClient().Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("%w: task status: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return out, fmt.Errorf("%w: task status: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, fmt.Errorf("task status: status %d: %s", res.StatusCode, string(b))
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("task status 解析失败: %w", err)
	}
	if out.TaskID == "" {
		out.TaskID = taskID
	}
	return out, nil
}

// BillingClient platform-billing 客户端(钱包主账在 billing.db,本产品零资金账本)。
type BillingClient struct {
	BaseURL string
	AppID   string
	Token   string // BILLING_APP_TOKENS 中本 app 专用
	HTTP    *http.Client
}

// LedgerResult 账本应答:balance_cny + items(只读展示,本产品不记账)。
type LedgerResult struct {
	BalanceCNY float64          `json:"balance_cny"`
	Items      []map[string]any `json:"items"`
}

func (c *BillingClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// EnsureAccount 确保计费账户存在(按 identity derive 公式,邮箱为准)。
func (c *BillingClient) EnsureAccount(ctx context.Context, email string) error {
	raw, _ := json.Marshal(map[string]string{"email": email})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/internal/v1/billing/accounts/ensure", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-App-ID", c.AppID)
	httpReq.Header.Set(internalTokenHeader, c.Token)
	res, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: billing ensure: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 {
		return fmt.Errorf("%w: billing ensure: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return fmt.Errorf("billing ensure: status %d: %s", res.StatusCode, string(b))
	}
	return nil
}

// Ledger 读账本(只读);不可达/5xx → ErrUnavailable。
func (c *BillingClient) Ledger(ctx context.Context, accountID string) (LedgerResult, error) {
	q := url.Values{"account_id": {accountID}}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(c.BaseURL, "/")+"/internal/v1/billing/ledger?"+q.Encode(), nil)
	if err != nil {
		return LedgerResult{}, err
	}
	httpReq.Header.Set("X-App-ID", c.AppID)
	httpReq.Header.Set(internalTokenHeader, c.Token)
	res, err := c.httpClient().Do(httpReq)
	if err != nil {
		return LedgerResult{}, fmt.Errorf("%w: billing ledger: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return LedgerResult{}, fmt.Errorf("%w: billing ledger: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return LedgerResult{}, fmt.Errorf("billing ledger: status %d: %s", res.StatusCode, string(b))
	}
	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return LedgerResult{}, fmt.Errorf("billing ledger 解析失败: %w", err)
	}
	out := LedgerResult{Items: []map[string]any{}}
	if v, ok := raw["balance_cny"].(json.Number); ok {
		f, err := v.Float64()
		if err != nil {
			return LedgerResult{}, fmt.Errorf("billing ledger: balance_cny 非数值")
		}
		out.BalanceCNY = f
	}
	if items, ok := raw["items"].([]any); ok {
		for _, it := range items {
			if m, ok := it.(map[string]any); ok {
				out.Items = append(out.Items, m)
			}
		}
	}
	return out, nil
}

// UploadClient platform-upload 客户端(I0 只暴露会话预创建的 fail-closed 路径)。
type UploadClient struct {
	BaseURL string
	AppID   string
	Token   string // UPLOAD_APP_TOKENS 中本 app 专用
	HTTP    *http.Client
}

// CreateUploadSession 预创建上传会话(PROVISIONAL 形状,真实分片直传 OSS 由 FEAT 票接)。
func (c *UploadClient) CreateUploadSession(ctx context.Context, fileName, contentType string, size int64) (map[string]any, error) {
	raw, _ := json.Marshal(map[string]any{
		"file_name": fileName, "content_type": contentType, "size": size,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/internal/v1/upload/sessions", bytes.NewReader(raw)) // PROVISIONAL
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-App-ID", c.AppID)
	httpReq.Header.Set(internalTokenHeader, c.Token)
	res, err := (func() *http.Client {
		if c.HTTP != nil {
			return c.HTTP
		}
		return &http.Client{Timeout: 15 * time.Second}
	}()).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: upload session: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: upload session: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("upload session: status %d: %s", res.StatusCode, string(b))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("upload session 解析失败: %w", err)
	}
	return out, nil
}

// RegisterAssetRequest 成果登记请求(I1,PROVISIONAL 形状)。
type RegisterAssetRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	ContentB64  string `json:"content_b64"` // 测试用合法 PNG 等小文件(E2E/非收费链路)
}

// RegisterAssetResult 登记事实:平台 asset_id 引用(本产品不存文件)。
type RegisterAssetResult struct {
	AssetID string `json:"asset_id"`
}

// RegisterAsset 经平台 upload 设施登记成果文件,返回 asset_id 引用。
// 端点路径 PROVISIONAL(I0 报告 §6):integration-guide 未给出 upload 内部端点
// 形状,平台侧真实形状以 FEAT 票核对为准,不一致只改这里的 path 常量与请求体。
func (c *UploadClient) RegisterAsset(ctx context.Context, req RegisterAssetRequest) (RegisterAssetResult, error) {
	var out RegisterAssetResult
	raw, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/internal/v1/upload/assets", bytes.NewReader(raw)) // PROVISIONAL
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-App-ID", c.AppID)
	httpReq.Header.Set(internalTokenHeader, c.Token)
	res, err := (func() *http.Client {
		if c.HTTP != nil {
			return c.HTTP
		}
		return &http.Client{Timeout: 15 * time.Second}
	}()).Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("%w: upload register: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 500 {
		return out, fmt.Errorf("%w: upload register: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, fmt.Errorf("upload register: status %d: %s", res.StatusCode, string(b))
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("upload register 解析失败: %w", err)
	}
	if out.AssetID == "" {
		return out, fmt.Errorf("upload register: 应答缺 asset_id(不伪造登记成功)")
	}
	return out, nil
}
