package billassemble

import (
	"context"
	"sync"
)

// Record 是测试双。它记下报价和预占，不连接平台。
// 预占必须用 Release 清掉，NetReserved 才回到零。
type Record struct {
	UnitMinor    int64
	BalanceMinor int64
	BalanceKnown bool
	Included     bool
	Entitled     []string

	mu           sync.Mutex
	quotes       int
	Settles      int
	BalanceReads int
	reserved     int64
	byKey        map[string]QuoteFact
	holds        map[string]int64
	holdByKey    map[string]string
	TaskStatus   map[string]string
	ChargeByTask map[string]string
}

func (r *Record) NetReserved() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reserved
}

func (r *Record) Quote(_ context.Context, in QuoteCall) (QuoteFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byKey == nil {
		r.byKey = map[string]QuoteFact{}
	}
	if fact, ok := r.byKey[in.IdempotencyKey]; ok {
		r.quotes++
		return fact, nil
	}
	amount := r.UnitMinor * in.Quantity
	fact := QuoteFact{
		UsageID: "usg_" + shortKey(in.IdempotencyKey), AmountMinor: amount, Currency: "CNY",
		IncludedAllowance: r.Included, BalanceKnown: r.BalanceKnown, BalanceMinor: r.BalanceMinor,
		Sent: false, Connection: ConnReady, EntitlementsKnown: true,
		Entitlements: append([]string(nil), r.Entitled...),
	}
	if r.BalanceKnown && !r.Included && r.BalanceMinor < amount {
		fact.FundsShort = true
	}
	r.byKey[in.IdempotencyKey] = fact
	r.quotes++
	return fact, nil
}

func (r *Record) Hold(_ context.Context, in HoldCall) (HoldFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holds == nil {
		r.holds = map[string]int64{}
		r.holdByKey = map[string]string{}
	}
	if id, ok := r.holdByKey[in.IdempotencyKey]; ok {
		return HoldFact{HoldID: id, AmountMinor: r.holds[id], FundsAction: "recorded"}, nil
	}
	if in.AmountMinor <= 0 || in.IdempotencyKey == "" {
		return HoldFact{}, ErrUsageInvalid
	}
	id := "uhold_" + shortKey(in.IdempotencyKey)
	r.holds[id] = in.AmountMinor
	r.holdByKey[in.IdempotencyKey] = id
	r.reserved += in.AmountMinor
	return HoldFact{HoldID: id, AmountMinor: in.AmountMinor, FundsAction: "recorded"}, nil
}

func (r *Record) Release(_ context.Context, in ReleaseCall) (ReleaseFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	amount, ok := r.holds[in.HoldID]
	if !ok {
		return ReleaseFact{}, ErrUsageInvalid
	}
	delete(r.holds, in.HoldID)
	r.reserved -= amount
	return ReleaseFact{ReleasedMinor: amount, FundsAction: "recorded"}, nil
}

func (r *Record) Settle(context.Context, SettleCall) (SettleFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Settles++
	return SettleFact{Sent: false, FundsAction: FundsNotExecuted}, ErrFundsNotExecuted
}

func (r *Record) Lookup(_ context.Context, in LookupCall) (LookupFact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, found := r.TaskStatus[in.TaskID]
	fact := LookupFact{TaskID: in.TaskID, TaskStatus: status, FoundTask: found && in.TaskID != "", Settlement: SettlementUnknown}
	if charge := r.ChargeByTask[in.TaskID]; charge != "" && in.TaskID != "" {
		fact.FoundCharge = true
		fact.ChargeRef = charge
	}
	return fact, nil
}

func (r *Record) ReadBalance(context.Context, string) (Balance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.BalanceReads++
	return Balance{Known: r.BalanceKnown, Minor: r.BalanceMinor, Connection: ConnReady}, nil
}

func shortKey(raw string) string {
	raw = stringsTrim(raw)
	if len(raw) > 12 {
		return raw[:12]
	}
	if raw == "" {
		return "none"
	}
	return raw
}

func stringsTrim(raw string) string {
	for len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\n') {
		raw = raw[1:]
	}
	return raw
}
