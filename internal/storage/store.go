package storage

import (
	"context"
	"log/slog"
	"time"
)

// Store is the persistence port used by the rest of the application.
// The matching engine never touches it on the hot path: queue state lives in
// memory and is only flushed here for auditing and moderation.
type Store interface {
	UpsertUser(ctx context.Context, u User) error
	GetUser(ctx context.Context, id string) (User, error)
	SuspendUser(ctx context.Context, id string, until time.Time, reason string) error

	CreateSession(ctx context.Context, s *Session) error
	AddSessionParticipant(ctx context.Context, sessionID, peerID string) error
	EndSession(ctx context.Context, id string, at time.Time, status SessionStatus, reason string) error

	CreateReport(ctx context.Context, r *Report) error
	ListReports(ctx context.Context, status ReportStatus, limit int) ([]Report, error)
	UpdateReportStatus(ctx context.Context, id string, status ReportStatus) error
	CountReportsAgainst(ctx context.Context, targetID string, since time.Time) (int, error)

	CreateBlock(ctx context.Context, b *Block) error
	IsBlocked(ctx context.Context, a, b string) (bool, error)
	ListBlocks(ctx context.Context, blockerID string) ([]Block, error)

	CreateModerationAction(ctx context.Context, a *ModerationAction) error
	ListModerationActions(ctx context.Context, limit int) ([]ModerationAction, error)

	Stats(ctx context.Context) (map[string]int, error)
	Close() error
}

// Open returns a PostgreSQL store when a DSN is configured, otherwise an
// in-memory store so the MVP can be started with a single command.
func Open(ctx context.Context, dsn, migrationsDir string, autoMigrate bool, logger *slog.Logger) (Store, error) {
	if dsn == "" {
		logger.Warn("DATABASE_URL is not set: using the in-memory store, nothing will be persisted")
		return NewMemory(), nil
	}
	pg, err := NewPostgres(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if autoMigrate {
		if err := pg.Migrate(ctx, migrationsDir); err != nil {
			pg.Close()
			return nil, err
		}
	}
	logger.Info("connected to PostgreSQL")
	return pg, nil
}
