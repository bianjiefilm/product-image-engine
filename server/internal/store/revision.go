package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bianjiefilm/product-image-engine/server/internal/revision"
)

// SaveRevisionLedger 只追加新版本，并更新采用指针与 Brief 差异。
// 已存在版本的内容哈希不得改变。
func (s *Store) SaveRevisionLedger(ctx context.Context, tenantID, projectID string, snap revision.Snapshot) error {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return err
	}
	adoptedHash := ""
	if snap.AdoptedID != "" {
		found := false
		for _, v := range snap.Versions {
			if v.Label == snap.AdoptedID {
				if v.Outcome != revision.StatusCandidate || len(v.PNG) == 0 {
					return fmt.Errorf("%w: adopted revision is not a pixel candidate", ErrValidation)
				}
				adoptedHash = v.ContentHash
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: adopted revision is missing", ErrValidation)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: 开启修订事务失败: %w", err)
	}
	defer tx.Rollback()
	now := Now().Format("2006-01-02T15:04:05Z")
	for _, v := range snap.Versions {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT content_sha256 FROM revision_versions
			WHERE tenant_id = ? AND project_id = ? AND label = ?`, tenantID, projectID, v.Label).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			locks := v.Locks
			if locks == nil {
				locks = []string{}
			}
			rawLocks, err := json.Marshal(locks)
			if err != nil {
				return fmt.Errorf("%w: locks", ErrValidation)
			}
			png := v.PNG
			if png == nil {
				png = []byte{}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO revision_versions (
				tenant_id, project_id, label, version_no, outcome, brief_version, content_sha256,
				asset_ref, origin, incremental_cost_cents, parent_label, action, locks_json,
				region, target_version, downstream, png, created_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				tenantID, projectID, v.Label, v.No, v.Outcome, v.BriefVersion, v.ContentHash,
				v.AssetRef, v.Origin, v.IncrementalCostCents, v.ParentLabel, v.Action, string(rawLocks),
				v.Region, v.TargetVersion, v.Downstream, png, now); err != nil {
				return fmt.Errorf("store: 写入修订版本失败: %w", err)
			}
		case err != nil:
			return fmt.Errorf("store: 读取修订版本失败: %w", err)
		default:
			if existing != v.ContentHash {
				return fmt.Errorf("%w: revision %s", ErrConflict, v.Label)
			}
		}
	}
	brief, err := json.Marshal(nilFacts(snap.BriefFacts))
	if err != nil {
		return fmt.Errorf("%w: brief", ErrValidation)
	}
	pending, err := json.Marshal(nilFacts(snap.PendingFacts))
	if err != nil {
		return fmt.Errorf("%w: pending brief", ErrValidation)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO revision_heads (
		tenant_id, project_id, adopted_label, adopted_sha256, brief_version, brief_json,
		pending_brief_version, pending_brief_json, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id, project_id) DO UPDATE SET
		  adopted_label = excluded.adopted_label,
		  adopted_sha256 = excluded.adopted_sha256,
		  brief_version = excluded.brief_version,
		  brief_json = excluded.brief_json,
		  pending_brief_version = excluded.pending_brief_version,
		  pending_brief_json = excluded.pending_brief_json,
		  updated_at = excluded.updated_at`,
		tenantID, projectID, snap.AdoptedID, adoptedHash, snap.BriefVersion, string(brief),
		snap.PendingBriefVersion, string(pending), now); err != nil {
		return fmt.Errorf("store: 写入修订指针失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: 提交修订失败: %w", err)
	}
	return nil
}

// LoadRevisionLedger 按租户读取账本。跨租户与不存在同为 ErrNotFound。
func (s *Store) LoadRevisionLedger(ctx context.Context, tenantID, projectID string) (revision.Snapshot, error) {
	if _, err := s.GetProject(ctx, tenantID, projectID); err != nil {
		return revision.Snapshot{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT label, version_no, outcome, brief_version, content_sha256,
		asset_ref, origin, incremental_cost_cents, parent_label, action, locks_json, region,
		target_version, downstream, png
		FROM revision_versions WHERE tenant_id = ? AND project_id = ? ORDER BY version_no`, tenantID, projectID)
	if err != nil {
		return revision.Snapshot{}, fmt.Errorf("store: 列出修订失败: %w", err)
	}
	defer rows.Close()
	snap := revision.Snapshot{BriefFacts: map[string]string{}, PendingFacts: map[string]string{}}
	for rows.Next() {
		var v revision.Version
		var locks string
		if err := rows.Scan(&v.Label, &v.No, &v.Outcome, &v.BriefVersion, &v.ContentHash,
			&v.AssetRef, &v.Origin, &v.IncrementalCostCents, &v.ParentLabel, &v.Action, &locks,
			&v.Region, &v.TargetVersion, &v.Downstream, &v.PNG); err != nil {
			return revision.Snapshot{}, fmt.Errorf("store: 扫描修订失败: %w", err)
		}
		if err := json.Unmarshal([]byte(locks), &v.Locks); err != nil {
			return revision.Snapshot{}, fmt.Errorf("store: 修订锁损坏: %w", err)
		}
		snap.Versions = append(snap.Versions, v)
	}
	if err := rows.Err(); err != nil {
		return revision.Snapshot{}, err
	}
	var briefJSON, pendingJSON, storedHash string
	err = s.db.QueryRowContext(ctx, `SELECT adopted_label, adopted_sha256, brief_version, brief_json,
		pending_brief_version, pending_brief_json FROM revision_heads
		WHERE tenant_id = ? AND project_id = ?`, tenantID, projectID).Scan(
		&snap.AdoptedID, &storedHash, &snap.BriefVersion, &briefJSON, &snap.PendingBriefVersion, &pendingJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return snap, nil
	}
	if err != nil {
		return revision.Snapshot{}, fmt.Errorf("store: 读取修订指针失败: %w", err)
	}
	if snap.AdoptedID != "" {
		matched := false
		for _, v := range snap.Versions {
			if v.Label == snap.AdoptedID && v.ContentHash == storedHash {
				matched = true
			}
		}
		if !matched {
			return revision.Snapshot{}, fmt.Errorf("%w: adopted revision hash drifted", ErrConflict)
		}
	}
	if err := json.Unmarshal([]byte(briefJSON), &snap.BriefFacts); err != nil {
		return revision.Snapshot{}, fmt.Errorf("store: brief 损坏: %w", err)
	}
	if err := json.Unmarshal([]byte(pendingJSON), &snap.PendingFacts); err != nil {
		return revision.Snapshot{}, fmt.Errorf("store: pending brief 损坏: %w", err)
	}
	if snap.BriefFacts == nil {
		snap.BriefFacts = map[string]string{}
	}
	if snap.PendingFacts == nil {
		snap.PendingFacts = map[string]string{}
	}
	return snap, nil
}

func nilFacts(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}
