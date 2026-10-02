// Package sourceimage holds the product's immutable source request facts.
// It does not grant funds, dispatch providers or authorize browser identities.
package sourceimage

import "errors"

const Owner = "product_source_v1"

// Modes the source owner can run. Both share one provider, model, size,
// quantity and price profile; only background_plate_lock adds a frozen input.
const (
	ModeTextGenerate = "text_generate"
	ModePlateLock    = "background_plate_lock"
)
const TaskRequestBudgetSeconds int64 = 20

var (
	ErrInvalid      = errors.New("source image: invalid request")
	ErrConflict     = errors.New("source image: immutable fact conflict")
	ErrNotFound     = errors.New("source image: not found")
	ErrUnavailable  = errors.New("source image: unavailable")
	ErrInvariant    = errors.New("source image: persisted invariant")
	ErrUnconfigured = errors.New("source image: unconfigured")
	ErrForbidden    = errors.New("source image: forbidden")
	// ErrNotFrozen: the registered input is not a frozen, approved sample.
	ErrNotFrozen = errors.New("source image: input is not a frozen sample")
	// ErrNotUsable: the derived result failed a required quality axis.
	ErrNotUsable = errors.New("source image: candidate not usable")
)

type Scope struct{ AppID, TenantID, ProjectID, UserID, PrincipalAccountID, PayerAccountID string }
type Intent struct {
	Scope                                                                       Scope
	RequestKey, Mode, Provider, Model, Capability, Size, Prompt, PricingVersion string
	Quantity                                                                    int64
	// PlateDigest binds a background_plate_lock run to its frozen input record.
	// Empty (and omitted from the JSON, keeping old rows byte-stable) otherwise.
	PlateDigest string `json:",omitempty"`
}
type Quote struct {
	UsageID, UsageKey, QuoteID, PricingVersion, BusinessRef, Currency  string
	Quantity, UnitPriceMinor, AmountMinor, QuotedAtUnix, ExpiresAtUnix int64
}
type Output struct {
	AssetID, ReferenceID, ProjectID, AppID, PrincipalType, PrincipalID string
	SHA256, ContentType, Status, ReferenceState                        string
	SizeBytes                                                          int64
}
type BillFact struct {
	Scope                                                              Scope
	UsageID, UsageKey, Capability, PricingVersion, BusinessRef, Status string
	Quantity                                                           int64
	Quote                                                              *Quote
	HoldID, ChargeID, ReleaseID, RefundID                              string
	ChargedMinor                                                       *int64
}
type TaskFact struct {
	HoldID, ChargeID, ReleaseID                             string
	ID, IdempotencyKey, Phase, Status, Provider, Capability string
	Scope                                                   Scope
	Quote                                                   Quote
	Output                                                  *Output
}
type Run struct {
	SubmitStartedAt, SubmitBudgetSeconds                           int64
	ID, Owner, Fingerprint, UsageKey, TaskKey, BusinessRef         string
	Intent                                                         Intent
	Quote                                                          *Quote
	ConfirmationHash                                               string
	ConfirmedAt                                                    int64
	Phase, TaskID                                                  string
	Bill                                                           *BillFact
	ChargedBill                                                    *BillFact
	Task                                                           *TaskFact
	Output                                                         *Output
	Selected, Deleted, CancelRequested, SubmitAttempted            bool
	Revision, LeaseEpoch, LeaseUntil, RetryAt, Attempts, UpdatedAt int64
	LastError, QualityJSON, EventsJSON                             string
}
type Outbox struct {
	ID, RunID                                 string
	Scope                                     Scope
	Output                                    Output
	State, Reason, LastError                  string
	Attempts, RetryAt, LeaseEpoch, LeaseUntil int64
}

type ContextFact struct {
	Resolved, PayerKnown                                                           bool
	AppID, UserID, TenantID, Role, PayerAccountID, PayerSource, MemberRole, Reason string
}
type ContextQuery struct {
	AppID, UserID, TenantID, SourceRef string
	ReadOnly                           bool
}
