package store

import (
	"context"
	"fmt"
)

// UndeliverableModelSHA 报告这份字节是尚未保真的模型结果。
// 背景结果还要求 deliverable 为 0。文字模型结果没有保真通过位，来源本身即不可用。
func (s *Store) UndeliverableModelSHA(ctx context.Context, tenantID, projectID, sha string) (bool, error) {
	if len(sha) != 64 {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT b.asset_id AS id
			FROM bg_result_blobs b
			INNER JOIN bg_replace_jobs j
				ON j.id = b.job_id AND j.tenant_id = b.tenant_id AND j.project_id = b.project_id
			WHERE b.tenant_id = ? AND b.project_id = ? AND b.sha256 = ? AND b.origin = ? AND j.deliverable = 0
			UNION ALL
			SELECT t.asset_id AS id
			FROM text_image_blobs t
			WHERE t.tenant_id = ? AND t.project_id = ? AND t.sha256 = ? AND t.origin = ?
		) hits`,
		tenantID, projectID, sha, "model_http",
		tenantID, projectID, sha, "model_http").Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: 读取模型字节失败: %w", err)
	}
	return n > 0, nil
}
