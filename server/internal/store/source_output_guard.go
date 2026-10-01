package store

import (
	"context"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
)

// HasVerifiedSourceOutput protects the source owner's association from legacy
// registration. Task metadata alone is insufficient; an attached output or the
// typed tombstone outbox records the previously verified Upload proof.
func (s *Store) HasVerifiedSourceOutput(ctx context.Context, tenant, project, sha, asset string) (bool, error) {
	if tenant == "" || project == "" || sha == "" && asset == "" {
		return false, nil
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+sourceColumns+` FROM product_source_runs WHERE owner=? AND tenant_id=? AND project_id=? AND (
	 (?!='' AND json_extract(observation_json,'$.Output.SHA256')=?) OR (?!='' AND json_extract(observation_json,'$.Output.AssetID')=?) OR
	 EXISTS (SELECT 1 FROM product_source_reference_outbox o WHERE o.run_id=product_source_runs.id AND ((?!='' AND json_extract(o.output_json,'$.SHA256')=?) OR (?!='' AND o.asset_id=?))))`, si.Owner, tenant, project, sha, sha, asset, asset, sha, sha, asset, asset)
	if e != nil {
		return false, e
	}
	var runs []si.Run
	for rows.Next() {
		r, e := scanSource(rows)
		if e != nil {
			rows.Close()
			return false, e
		}
		runs = append(runs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false, e
	}
	match := func(o si.Output) bool { return sha != "" && o.SHA256 == sha || asset != "" && o.AssetID == asset }
	for _, r := range runs {
		if r.Output != nil && match(*r.Output) {
			if si.ValidateOutputAssociation(r, *r.Output) != nil {
				return false, si.ErrInvariant
			}
			return true, nil
		}
		outboxes, e := s.db.QueryContext(ctx, `SELECT `+sourceOutboxColumns+` FROM product_source_reference_outbox WHERE run_id=?`, r.ID)
		if e != nil {
			return false, e
		}
		for outboxes.Next() {
			o, e := scanSourceOutbox(outboxes)
			if e != nil {
				outboxes.Close()
				return false, e
			}
			if !match(o.Output) {
				continue
			}
			if !r.Deleted || o.Scope != r.Intent.Scope || r.Bill == nil || (r.Bill.Status != "charged" && r.Bill.Status != "refunded") || si.ValidateSettlement(r, *r.Bill) != nil || r.Task == nil || r.Task.Status != "succeeded" || r.Task.Output == nil {
				outboxes.Close()
				return false, si.ErrInvariant
			}
			expected := *r.Task.Output
			expected.Status = "ready"
			expected.ReferenceState = "active"
			if expected != o.Output {
				outboxes.Close()
				return false, si.ErrInvariant
			}
			outboxes.Close()
			return true, nil
		}
		e = outboxes.Err()
		outboxes.Close()
		if e != nil {
			return false, e
		}
	}
	return false, nil
}
