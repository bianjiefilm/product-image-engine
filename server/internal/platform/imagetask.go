package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

// ImageTaskRequest is built only from a server-frozen quote and verified payer.
// The existing platform Task owns the hold, spend and refund operations.
type ImageTaskRequest struct {
	AccountID, ProjectID, IdempotencyKey, Provider string
	Params                                         json.RawMessage
	AmountMinor                                    int64
}

func (c *TaskClient) SubmitImage(ctx context.Context, in ImageTaskRequest) (TaskStatusResult, error) {
	var params map[string]json.RawMessage
	if c == nil || strings.TrimSpace(c.AppID) == "" || strings.TrimSpace(c.Token) == "" || strings.TrimSpace(in.AccountID) == "" || strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.Provider) == "" || in.AmountMinor <= 0 || json.Unmarshal(in.Params, &params) != nil || len(params) == 0 {
		return TaskStatusResult{}, fmt.Errorf("image task: verified request and positive quote required")
	}
	body, err := json.Marshal(map[string]any{
		"app_id": c.AppID, "account_id": in.AccountID, "project_id": in.ProjectID,
		"capability": "image.generate", "idempotency_key": in.IdempotencyKey,
		"provider": in.Provider, "params": in.Params,
		"billing": map[string]any{"mode": "hold", "amount_cny": json.Number(fmt.Sprintf("%d.%02d", in.AmountMinor/100, in.AmountMinor%100)), "reason": "product-image:model_plate_lock"},
	})
	if err != nil {
		return TaskStatusResult{}, err
	}
	client := &platformconsumer.Client{Caller: platformconsumer.Caller{Mode: "app", AppID: c.AppID, Token: c.Token}, Bases: platformconsumer.Bases{Task: c.BaseURL}, HTTP: c.httpClient()}
	res, err := client.Call(ctx, platformconsumer.Call{Operation: "task.submit", Body: body})
	if err != nil || !res.Sent || res.StatusCode < 200 || res.StatusCode >= 300 {
		// No raw upstream body, URL or SDK error can leak a credential here.
		return TaskStatusResult{}, fmt.Errorf("%w: image task submit status %d", ErrUnavailable, res.StatusCode)
	}
	return decodeTaskStatus(res.Body, "")
}

func decodeTaskStatus(body []byte, expectedID string) (TaskStatusResult, error) {
	var raw struct {
		TaskID     string          `json:"task_id"`
		Status     string          `json:"status"`
		HoldID     string          `json:"hold_id"`
		Capability string          `json:"capability"`
		Result     json.RawMessage `json:"result"`
		ResultJSON json.RawMessage `json:"result_json"`
	}
	if json.Unmarshal(body, &raw) != nil || strings.TrimSpace(raw.TaskID) == "" || (expectedID != "" && raw.TaskID != expectedID) {
		return TaskStatusResult{}, fmt.Errorf("task status: invalid task identity")
	}
	resultBody := raw.Result
	if raw.ResultJSON != nil {
		// Presence is authoritative, including an empty or null result. Never
		// merge legacy asset/byte fields into the platform's current result.
		var encoded string
		if err := json.Unmarshal(raw.ResultJSON, &encoded); err != nil {
			return TaskStatusResult{}, fmt.Errorf("task status: invalid result_json")
		}
		resultBody = []byte(strings.TrimSpace(encoded))
	}
	var result map[string]any
	if len(resultBody) > 0 {
		if err := json.Unmarshal(resultBody, &result); err != nil {
			return TaskStatusResult{}, fmt.Errorf("task status: invalid result object")
		}
	}
	return TaskStatusResult{TaskID: raw.TaskID, Status: strings.ToLower(raw.Status), HoldID: raw.HoldID, Capability: raw.Capability, Result: result}, nil
}
