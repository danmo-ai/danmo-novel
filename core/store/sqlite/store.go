package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// 写入按阶段密集（run + candidate + 正文），busy_timeout 防止读轮询期间
	// 写事务直接 SQLITE_BUSY 丢一次 SaveRun。
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS llm_configs (
			id TEXT PRIMARY KEY,
			provider TEXT NOT NULL,
			name TEXT NOT NULL,
			api_key TEXT,
			base_url TEXT,
			default_dialect TEXT,
			models TEXT,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS books (
			id TEXT PRIMARY KEY,
			root_path TEXT NOT NULL,
			title TEXT,
			read_only INTEGER NOT NULL DEFAULT 0,
			revision TEXT,
			imported_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			book_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS candidates (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			book_id TEXT NOT NULL,
			status TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS model_assignments (
			role TEXT PRIMARY KEY,
			model_id TEXT NOT NULL
		)`,
		// 连续重写（chain）已整体移除：旧库里遗留的编排记录不再有任何读取方，
		// 留着只会误导（像是还有个能继续的任务）。这里顺手清掉。
		`DROP TABLE IF EXISTS chains`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// ---- LLM configs ----

func (s *Store) GetAll(ctx context.Context) ([]domain.LLMProviderConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider, name, api_key, base_url, default_dialect, models, created_at, updated_at FROM llm_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LLMProviderConfig
	for rows.Next() {
		cfg, err := scanLLM(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}

func (s *Store) GetByID(ctx context.Context, id string) (domain.LLMProviderConfig, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, provider, name, api_key, base_url, default_dialect, models, created_at, updated_at FROM llm_configs WHERE id = ?`, id)
	return scanLLM(row)
}

func (s *Store) Upsert(ctx context.Context, cfg domain.LLMProviderConfig) error {
	b, _ := json.Marshal(cfg.Models)
	_, err := s.db.ExecContext(ctx, `INSERT INTO llm_configs(id, provider, name, api_key, base_url, default_dialect, models, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, name=excluded.name, api_key=excluded.api_key,
		base_url=excluded.base_url, default_dialect=excluded.default_dialect, models=excluded.models, updated_at=excluded.updated_at`,
		cfg.ID, string(cfg.Provider), cfg.Name, cfg.APIKey, cfg.BaseURL, cfg.DefaultProtocolDialect, string(b), cfg.CreatedAt, cfg.UpdatedAt)
	return err
}

func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM llm_configs WHERE id = ?`, id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLLM(row rowScanner) (domain.LLMProviderConfig, error) {
	var cfg domain.LLMProviderConfig
	var provider, modelsJSON string
	if err := row.Scan(&cfg.ID, &provider, &cfg.Name, &cfg.APIKey, &cfg.BaseURL, &cfg.DefaultProtocolDialect, &modelsJSON, &cfg.CreatedAt, &cfg.UpdatedAt); err != nil {
		return cfg, err
	}
	cfg.Provider = domain.LLMProviderType(provider)
	_ = json.Unmarshal([]byte(modelsJSON), &cfg.Models)
	return cfg, nil
}

// ---- Books registry ----

func (s *Store) UpsertBook(ctx context.Context, id, root, title, revision string, readOnly bool) error {
	ro := 0
	if readOnly {
		ro = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO books(id, root_path, title, read_only, revision, imported_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET root_path=excluded.root_path, title=excluded.title, read_only=excluded.read_only, revision=excluded.revision, imported_at=excluded.imported_at`,
		id, root, title, ro, revision, now)
	return err
}

func (s *Store) ListBooks(ctx context.Context) ([]domain.BookSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, root_path, title, read_only, revision FROM books ORDER BY imported_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BookSummary
	for rows.Next() {
		var b domain.BookSummary
		var ro int
		var rev string
		if err := rows.Scan(&b.ID, &b.RootPath, &b.Title, &ro, &rev); err != nil {
			return nil, err
		}
		b.ReadOnly = ro == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBookRoot(ctx context.Context, id string) (string, bool, error) {
	var root string
	var ro int
	err := s.db.QueryRowContext(ctx, `SELECT root_path, read_only FROM books WHERE id = ?`, id).Scan(&root, &ro)
	if err == sql.ErrNoRows {
		return "", false, fmt.Errorf("book not found: %s", id)
	}
	return root, ro == 1, err
}

// ---- Runs / candidates ----

func (s *Store) SaveRun(ctx context.Context, run domain.Run) error {
	b, err := json.Marshal(run)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO runs(id, book_id, kind, status, payload, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, payload=excluded.payload, updated_at=excluded.updated_at`,
		run.ID, run.BookID, string(run.Kind), string(run.Status), string(b),
		run.CreatedAt.UTC().Format(time.RFC3339), run.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM runs WHERE id = ?`, id).Scan(&payload)
	if err != nil {
		return domain.Run{}, err
	}
	var run domain.Run
	if err := json.Unmarshal([]byte(payload), &run); err != nil {
		return domain.Run{}, err
	}
	return run, nil
}

func (s *Store) ListRuns(ctx context.Context, bookID string) ([]domain.Run, error) {
	query := `SELECT payload FROM runs`
	args := []any{}
	if bookID != "" {
		query += ` WHERE book_id = ?`
		args = append(args, bookID)
	}
	query += ` ORDER BY updated_at DESC, created_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Run, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var run domain.Run
		if err := json.Unmarshal([]byte(payload), &run); err != nil {
			continue
		}
		out = append(out, run)
	}
	return out, nil
}

// ListUnfinishedRuns returns every run still marked pending/running. execute()
// only lives in process memory, so after a restart these rows can never
// finish; startup recovery must fail them explicitly (they also block new
// runs for the same book/kind/unit via FindActiveRun).
func (s *Store) ListUnfinishedRuns(ctx context.Context) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM runs WHERE status IN ('pending','running')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Run, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var run domain.Run
		if err := json.Unmarshal([]byte(payload), &run); err != nil {
			continue
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) FindActiveRun(ctx context.Context, bookID string, kind domain.TaskKind, unitID string) (domain.Run, bool, error) {
	query := `SELECT payload FROM runs WHERE book_id = ? AND status IN ('pending','running','waiting_accept')`
	args := []any{bookID}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, string(kind))
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return domain.Run{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return domain.Run{}, false, err
		}
		var run domain.Run
		if err := json.Unmarshal([]byte(payload), &run); err != nil {
			continue
		}
		if run.Spec.UnitID == unitID {
			return run, true, nil
		}
	}
	return domain.Run{}, false, nil
}

// CompleteAcceptance persists candidate and run terminal states atomically.
func (s *Store) CompleteAcceptance(ctx context.Context, c domain.Candidate, run domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cb, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE candidates SET status=?, payload=? WHERE id=? AND status='open'`, c.Status, string(cb), c.ID); err != nil {
		return err
	}
	rb, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if run.ID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET status=?, payload=?, updated_at=? WHERE id=?`, run.Status, string(rb), run.UpdatedAt.UTC().Format(time.RFC3339), run.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveCandidate(ctx context.Context, c domain.Candidate) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO candidates(id, run_id, book_id, status, payload, created_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, payload=excluded.payload`,
		c.ID, c.RunID, c.BookID, string(c.Status), string(b), c.CreatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetCandidate(ctx context.Context, id string) (domain.Candidate, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM candidates WHERE id = ?`, id).Scan(&payload)
	if err != nil {
		return domain.Candidate{}, err
	}
	var c domain.Candidate
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return domain.Candidate{}, err
	}
	return c, nil
}

func (s *Store) ListOpenCandidates(ctx context.Context, bookID string) ([]domain.Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM candidates WHERE book_id = ? AND status = 'open' ORDER BY created_at DESC`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Candidate, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var c domain.Candidate
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// ListClosedCandidates returns 已终结候选（accepted/rejected/expired），最新在前。
// 「接受归档」的审校清单只能靠它回看，所以按 book 维度整体取回，由调用方按单元分组。
func (s *Store) ListClosedCandidates(ctx context.Context, bookID string, limit int) ([]domain.Candidate, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload FROM candidates WHERE book_id = ? AND status IN ('accepted','rejected','expired') ORDER BY created_at DESC LIMIT ?`,
		bookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Candidate, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var c domain.Candidate
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
