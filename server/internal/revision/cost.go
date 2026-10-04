package revision

// 唯一的增量费用函数。单位是整数分。调用方不得再加一次。
const (
	centsBackground = 40
	centsLight      = 20
)

// IncrementalCostCents 对同一意图返回同一个整数分。
// preserve_subject=false 拒绝计价，避免把整图重画标成一笔费用后静默执行。
func IncrementalCostCents(intent Intent) (int64, error) {
	if !intent.PreserveSubject {
		return 0, ErrPreserveSubject
	}
	switch intent.Action {
	case ActionSimplifyBackground, ActionOutdoor:
		return centsBackground, nil
	case ActionBrightenKeepProduct:
		return centsLight, nil
	case ActionRollback, ActionAdopt, ActionSendDownstream:
		return 0, nil
	default:
		return 0, ErrUnrecognized
	}
}
