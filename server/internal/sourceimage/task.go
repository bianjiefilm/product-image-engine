package sourceimage

func ValidateTaskFact(r Run, f TaskFact) error {
	if r.Owner != Owner || r.Quote == nil || !identifier(f.ID) || f.Scope != r.Intent.Scope || f.IdempotencyKey != r.TaskKey || f.Quote != *r.Quote || f.Provider != r.Intent.Provider || f.Capability != r.Intent.Capability || !identifier(f.Phase) {
		return ErrInvariant
	}
	if r.TaskID != "" && r.TaskID != f.ID {
		return ErrInvariant
	}
	switch f.Status {
	case "queued", "running", "succeeded", "failed", "canceled":
	default:
		return ErrInvariant
	}
	if r.Task != nil {
		old := r.Task
		if old.ID != f.ID {
			return ErrInvariant
		}
		if old.HoldID != "" && old.HoldID != f.HoldID || old.ChargeID != "" && old.ChargeID != f.ChargeID || old.ReleaseID != "" && old.ReleaseID != f.ReleaseID {
			return ErrInvariant
		}
		if (old.Status == "succeeded" || old.Status == "failed" || old.Status == "canceled") && old.Status != f.Status {
			return ErrInvariant
		}
		if old.Output != nil && (f.Output == nil || *old.Output != *f.Output) {
			return ErrInvariant
		}
	}
	if f.Output != nil {
		o := f.Output
		if !identifier(o.AssetID) || !identifier(o.ReferenceID) || o.ProjectID != r.Intent.Scope.ProjectID || o.AppID != r.Intent.Scope.AppID || o.PrincipalType != "user" || o.PrincipalID != r.Intent.Scope.UserID || o.ContentType != "image/png" || o.SizeBytes <= 0 || o.SizeBytes > 16<<20 || len(o.SHA256) != 64 {
			return ErrInvariant
		}
		for _, ch := range o.SHA256 {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				return ErrInvariant
			}
		}
	}
	if f.Status == "succeeded" && (f.Output == nil || f.HoldID == "" || f.ChargeID == "" || f.ReleaseID != "") {
		return ErrInvariant
	}
	return nil
}
