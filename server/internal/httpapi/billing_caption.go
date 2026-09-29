package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/bianjiefilm/product-image-engine/server/internal/billassemble"
	"github.com/bianjiefilm/product-image-engine/server/internal/billconsume"
)

const legacyBillingLabel = "计费待确认"

// captionFor 只在配置齐全且尺寸有效时向端口报价。配置缺失或未实现都不发请求。
// 报价失败仍返回可展示的文案，调用方继续创建作品。
func (s *Server) captionFor(ctx context.Context, payer billconsume.Payer, capability string, quantity int64, width, height int, businessRef, idem string) (string, string) {
	if billassemble.Connection(s.billLink()) == billassemble.ConnMissing {
		return billassemble.ConnMissing, billassemble.SettlementUnknown
	}
	if s.Bills == nil {
		return billassemble.ConnUnimplemented, billassemble.SettlementUnknown
	}
	if quantity <= 0 || width <= 0 || height <= 0 || payer.AccountRef == "" || strings.TrimSpace(capability) == "" || strings.TrimSpace(idem) == "" {
		return "待确认", billassemble.SettlementUnknown
	}
	q, err := s.Bills.Quote(ctx, billassemble.QuoteCall{
		PayerAccountID: payer.AccountRef, Capability: capability, Quantity: quantity,
		PricingVersion: billassemble.PricingVersion, BusinessRef: businessRef, IdempotencyKey: idem,
	})
	if err != nil {
		switch {
		case errors.Is(err, billassemble.ErrConfigMissing):
			return billassemble.ConnMissing, billassemble.SettlementUnknown
		case errors.Is(err, billassemble.ErrUnimplemented):
			return billassemble.ConnUnimplemented, billassemble.SettlementUnknown
		default:
			return "待确认", billassemble.SettlementUnknown
		}
	}
	label, err := presentQuote(q)
	if err != nil || !realQuote(label) {
		return "待确认", billassemble.SettlementUnknown
	}
	return label, billassemble.SettlementUnknown
}

// shownBilling 把已保存的文案改写成当前连接状态。没有账单时不把结算写成通过。
func (s *Server) shownBilling(stored, status, feeRef string) (string, string) {
	settlement := billassemble.SettlementUnknown
	if strings.TrimSpace(feeRef) == "" && (status == "completed" || status == "succeeded") {
		settlement = billassemble.PendingCheck
	}
	if billassemble.Connection(s.billLink()) == billassemble.ConnMissing {
		return billassemble.ConnMissing, settlement
	}
	if s.Bills == nil {
		return billassemble.ConnUnimplemented, settlement
	}
	label := strings.TrimSpace(stored)
	if label == "" || label == legacyBillingLabel {
		label = "待确认"
	}
	return label, settlement
}
