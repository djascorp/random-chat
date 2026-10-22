package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Postgres is the durable Store used in production.
type Postgres struct {
	db *sql.DB
}

// NewPostgres opens a pooled connection and verifies it before returning.
func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Postgres{db: db}, nil
}

// Migrate applies every *.sql file found in dir, in filename order. Each file is
// expected to be idempotent (CREATE ... IF NOT EXISTS) so re-running is safe.
func (p *Postgres) Migrate(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := p.db.ExecContext(ctx, string(raw)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func (p *Postgres) UpsertUser(ctx context.Context, u User) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO users (id, status, created_at, last_seen_at, last_ip)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    last_ip = EXCLUDED.last_ip`,
		u.ID, string(u.Status), u.CreatedAt, u.LastSeenAt, u.LastIP)
	return err
}

func (p *Postgres) GetUser(ctx context.Context, id string) (User, error) {
	row := p.db.QueryRowContext(ctx, `
		SELECT id, status, created_at, last_seen_at, suspended_until, suspend_reason, last_ip
		FROM users WHERE id = $1`, id)
	return scanUser(row)
}

func (p *Postgres) SuspendUser(ctx context.Context, id string, until time.Time, reason string) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO users (id, status, created_at, last_seen_at, suspended_until, suspend_reason)
		VALUES ($1, $2, now(), now(), $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET status = EXCLUDED.status,
		    suspended_until = EXCLUDED.suspended_until,
		    suspend_reason = EXCLUDED.suspend_reason`,
		id, string(UserSuspended), until, reason)
	return err
}

func (p *Postgres) CreateSession(ctx context.Context, s *Session) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO sessions (id, started_at, status, mode, max_participants)
		VALUES ($1, $2, $3, $4, $5)`,
		s.ID, s.StartedAt, string(s.Status), s.Mode, s.MaxParticipants)
	if err != nil {
		return err
	}
	for _, peerID := range s.Participants {
		if err := p.AddSessionParticipant(ctx, s.ID, peerID); err != nil {
			return err
		}
	}
	return nil
}

// AddSessionParticipant records a late joiner of a conference.
func (p *Postgres) AddSessionParticipant(ctx context.Context, sessionID, peerID string) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO session_participants (session_id, peer_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`,
		sessionID, peerID)
	return err
}

func (p *Postgres) EndSession(ctx context.Context, id string, at time.Time, status SessionStatus, reason string) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE sessions SET ended_at = $2, status = $3, end_reason = $4 WHERE id = $1`,
		id, at, string(status), reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) CreateReport(ctx context.Context, r *Report) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO reports (id, reporter_id, target_id, session_id, category, reason, details, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.ID, r.ReporterID, r.TargetID, r.SessionID, r.Category, r.Reason, r.Details, string(r.Status), r.CreatedAt)
	return err
}

func (p *Postgres) ListReports(ctx context.Context, status ReportStatus, limit int) ([]Report, error) {
	query := `SELECT id, reporter_id, target_id, session_id, category, reason, details, status, created_at
	          FROM reports`
	args := []any{}
	if status != "" {
		query += ` WHERE status = $1`
		args = append(args, string(status))
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, limit)

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Report{}
	for rows.Next() {
		var r Report
		var details sql.NullString
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.TargetID, &r.SessionID, &r.Category,
			&r.Reason, &details, &r.Status, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Details = details.String
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateReportStatus(ctx context.Context, id string, status ReportStatus) error {
	res, err := p.db.ExecContext(ctx, `UPDATE reports SET status = $2 WHERE id = $1`, id, string(status))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) CountReportsAgainst(ctx context.Context, targetID string, since time.Time) (int, error) {
	var count int
	err := p.db.QueryRowContext(ctx, `
		SELECT count(*) FROM reports WHERE target_id = $1 AND created_at > $2`,
		targetID, since).Scan(&count)
	return count, err
}

func (p *Postgres) CreateBlock(ctx context.Context, b *Block) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO blocks (blocker_id, blocked_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (blocker_id, blocked_id) DO NOTHING`,
		b.BlockerID, b.BlockedID, b.CreatedAt)
	return err
}

func (p *Postgres) IsBlocked(ctx context.Context, a, b string) (bool, error) {
	var exists bool
	err := p.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM blocks
			WHERE (blocker_id = $1 AND blocked_id = $2)
			   OR (blocker_id = $2 AND blocked_id = $1)
		)`, a, b).Scan(&exists)
	return exists, err
}

func (p *Postgres) ListBlocks(ctx context.Context, blockerID string) ([]Block, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT blocker_id, blocked_id, created_at FROM blocks WHERE blocker_id = $1`, blockerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Block{}
	for rows.Next() {
		var b Block
		if err := rows.Scan(&b.BlockerID, &b.BlockedID, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateModerationAction(ctx context.Context, a *ModerationAction) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO moderation_actions (id, target_id, action, reason, actor, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		a.ID, a.TargetID, a.Action, a.Reason, a.Actor, a.CreatedAt)
	return err
}

func (p *Postgres) ListModerationActions(ctx context.Context, limit int) ([]ModerationAction, error) {
	rows, err := p.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, target_id, action, reason, actor, created_at
		FROM moderation_actions ORDER BY created_at DESC LIMIT %d`, limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModerationAction{}
	for rows.Next() {
		var a ModerationAction
		var reason sql.NullString
		if err := rows.Scan(&a.ID, &a.TargetID, &a.Action, &reason, &a.Actor, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Reason = reason.String
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) Stats(ctx context.Context) (map[string]int, error) {
	stats := map[string]int{}
	queries := map[string]string{
		"users":              `SELECT count(*) FROM users`,
		"sessions":           `SELECT count(*) FROM sessions`,
		"reports":            `SELECT count(*) FROM reports`,
		"reports_open":       `SELECT count(*) FROM reports WHERE status = 'open'`,
		"blocks":             `SELECT count(*) FROM blocks`,
		"moderation_actions": `SELECT count(*) FROM moderation_actions`,
	}
	for key, q := range queries {
		var n int
		if err := p.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return nil, err
		}
		stats[key] = n
	}
	return stats, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() error { return p.db.Close() }

func scanUser(row *sql.Row) (User, error) {
	var u User
	var suspendedUntil sql.NullTime
	var reason, ip sql.NullString
	err := row.Scan(&u.ID, &u.Status, &u.CreatedAt, &u.LastSeenAt, &suspendedUntil, &reason, &ip)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if suspendedUntil.Valid {
		t := suspendedUntil.Time
		u.SuspendedUntil = &t
	}
	u.SuspendReason = reason.String
	u.LastIP = ip.String
	return u, nil
}
