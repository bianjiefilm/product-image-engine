package matrixhandoff

import (
	"context"
	"errors"
	"sync"
)

// Envelope 是交给矩阵接口的草稿。它不是上传，也不是已发布。
type Envelope struct {
	Document             Document
	Role                 string
	ExistingVideoAssetID string
}

// Ack 是矩阵接口的应答。夹具应答不是在线服务，也不代表已发布。
type Ack struct {
	DraftRef       string
	DraftVersion   int
	Accepted       bool
	Published      bool
	Uploaded       bool
	VideoCreated   bool
	Role           string
	Copy           string
	QualityVerdict string
	Service        string
	LiveMatrix     bool
}

// Client 是矩阵接口。实现可以是夹具，夹具不是在线矩阵服务。
type Client interface {
	SubmitDraft(ctx context.Context, env Envelope) (Ack, error)
}

// FixtureClient 记录草稿提交。它不上传、不造视频、不把结论改成真实商品无改动。
type FixtureClient struct {
	Err     error
	mu      sync.Mutex
	Submits []Envelope
}

// Live 恒为 false。夹具编号不是 Service PASS，也不是在线矩阵。
func (c *FixtureClient) Live() bool { return false }

// SubmitDraft 收下草稿。文案若被写成真实商品无改动，则拒绝且不记录。
// 调用方预设的错误原样返回，不把草稿标成已发布。
func (c *FixtureClient) SubmitDraft(ctx context.Context, env Envelope) (Ack, error) {
	if ctx == nil {
		return rejectedAck(), ErrInvalidDocument
	}
	if err := ctx.Err(); err != nil {
		return rejectedAck(), err
	}
	if env.Document.Copy == ForbiddenUnchangedCopy {
		return rejectedAck(), errors.New(FailCopy)
	}
	if c == nil {
		return rejectedAck(), ErrInvalidDocument
	}
	if c.Err != nil {
		return rejectedAck(), c.Err
	}
	c.mu.Lock()
	c.Submits = append(c.Submits, env)
	c.mu.Unlock()
	return Ack{
		DraftRef:       env.Document.DraftRef,
		DraftVersion:   env.Document.DraftVersion,
		Accepted:       true,
		Published:      false,
		Uploaded:       false,
		VideoCreated:   false,
		Role:           env.Role,
		Copy:           env.Document.Copy,
		QualityVerdict: env.Document.QualityVerdict,
		Service:        ServiceNotVerified,
		LiveMatrix:     false,
	}, nil
}

func rejectedAck() Ack {
	return Ack{
		Published:    false,
		Uploaded:     false,
		VideoCreated: false,
		Service:      ServiceNotVerified,
		LiveMatrix:   false,
	}
}
