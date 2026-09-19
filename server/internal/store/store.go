// Package store 提供产品图领域模型的 SQLite 持久化。
// 每次读写都按 tenant 作用域过滤——跨租户不可见是本包的硬不变式。
package store

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store 包装 *sql.DB。
type Store struct {
	db *sql.DB
}

// Open 打开(必要时创建)SQLite 库并执行嵌入迁移。
// DSN 启用 WAL、外键与忙等待;文件路径由调用方配置(PRODUCT_DB_PATH)。
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: 打开数据库失败: %w", err)
	}
	// SQLite 单写者:串行化访问最稳妥。
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("store: 枚举迁移失败: %w", err)
	}
	sort.Strings(entries)
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("store: 建 schema_migrations 失败: %w", err)
	}
	for _, e := range entries {
		version := strings.TrimPrefix(strings.TrimSuffix(e, ".sql"), "migrations/")
		var one int
		if err := s.db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&one); err == nil {
			continue // 已应用
		} else if err != sql.ErrNoRows {
			return fmt.Errorf("store: 查询迁移状态失败: %w", err)
		}
		body, err := fs.ReadFile(migrationsFS, e)
		if err != nil {
			return fmt.Errorf("store: 读迁移 %s 失败: %w", e, err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("store: 开事务失败: %w", err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: 应用迁移 %s 失败: %w", version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, Now().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: 记录迁移 %s 失败: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: 提交迁移 %s 失败: %w", version, err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// Now 可测试注入的时间源。
var Now = func() time.Time { return time.Now().UTC() }

// newID 生成带前缀的随机标识。
func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("store: 随机源不可用: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b)
}
