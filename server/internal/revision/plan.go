package revision

const (
	StepSupplierFullRedraw = "supplier_full_redraw"
	StepSubjectResample    = "subject_resample"
	StepLogoRegen          = "logo_regen"
	StepPackagingTextRegen = "packaging_text_regen"
	StepVideoTranscode     = "video_transcode"
	StepMatrixPublish      = "matrix_publish"
	StepNewUpload          = "new_upload"
	StepDeterministicPlate = "deterministic_plate"
	StepSubjectLock        = "subject_lock"
	StepVersionRecord      = "version_record"
	StepPointerMove        = "pointer_move"
	StepDownstreamRef      = "downstream_ref"
)

// ExpensiveSteps 是局部修改必须跳过的无关步骤。
var ExpensiveSteps = []string{
	StepSupplierFullRedraw,
	StepSubjectResample,
	StepLogoRegen,
	StepPackagingTextRegen,
	StepVideoTranscode,
	StepMatrixPublish,
	StepNewUpload,
}

// ImpactNotice 在供应商局部修改未开启时展示。它不承诺会改像素。
const ImpactNotice = "局部修改供应商没有打开。不会改图，不会把原图或纯色占位说成已经局部重绘，也不会因此扣费。"

// SupplierClosedNote 说明本次不会执行，也不会留下伪造成果。
const SupplierClosedNote = "局部修改供应商未开启。本次不执行，不产生新版本。"

// SupplierUnknownNote 说明能力标记打开时本构建仍不调用供应商。
const SupplierUnknownNote = "真实供应商局部编辑仍是 UNKNOWN；本次使用确定性底板并锁回主体，不调用供应商。"

// Provider 只描述能力。本构建即使 PartialEdit 为真也不调用供应商。
type Provider struct {
	PartialEdit bool
}

// Plan 是执行前的影响范围和步骤。CallsSupplier 在本构建恒为 false。
type Plan struct {
	Intent                  Intent   `json:"intent"`
	Steps                   []string `json:"steps"`
	Skipped                 []string `json:"skipped"`
	ImpactNotice            string   `json:"impact_notice,omitempty"`
	RequiresAcknowledgement bool     `json:"requires_acknowledgement"`
	IncrementalCostCents    int64    `json:"incremental_cost_cents"`
	CallsSupplier           bool     `json:"calls_supplier"`
	SupplierNote            string   `json:"supplier_note,omitempty"`
	Degraded                bool     `json:"degraded,omitempty"`
}

// PlanRevision 只计划。不读像素，不扣费，不调用供应商。
func PlanRevision(intent Intent, provider Provider) (Plan, error) {
	if !intent.PreserveSubject {
		return Plan{}, ErrPreserveSubject
	}
	cost, err := IncrementalCostCents(intent)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		Intent:               intent,
		IncrementalCostCents: cost,
		CallsSupplier:        false,
		Skipped:              append([]string{}, ExpensiveSteps...),
	}
	switch intent.Action {
	case ActionSimplifyBackground, ActionOutdoor, ActionBrightenKeepProduct:
		if !provider.PartialEdit {
			plan.Degraded = true
			plan.ImpactNotice = ImpactNotice
			plan.SupplierNote = SupplierClosedNote
			plan.Steps = []string{}
			plan.Skipped = append(plan.Skipped, StepDeterministicPlate, StepSubjectLock, StepVersionRecord)
			break
		}
		plan.Steps = []string{StepDeterministicPlate, StepSubjectLock, StepVersionRecord}
		plan.SupplierNote = SupplierUnknownNote
	case ActionRollback, ActionAdopt:
		plan.Steps = []string{StepPointerMove}
	case ActionSendDownstream:
		if !KnownDownstream(intent.Downstream) {
			return Plan{}, ErrUnknownDownstream
		}
		plan.Steps = []string{StepDownstreamRef}
	default:
		return Plan{}, ErrUnrecognized
	}
	return plan, nil
}
