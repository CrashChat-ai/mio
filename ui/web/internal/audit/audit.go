package audit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ResultSuccess = "success"
	ResultFailure = "failure"
	ResultDenied  = "denied"
)

type Event struct {
	OperatorEmail string
	OperatorRole  string
	Action        string
	TargetType    string
	TargetID      string
	Result        string
	Error         string
	CreatedAt     time.Time
}

type Logger interface {
	Record(ctx context.Context, event Event) error
	List(ctx context.Context, limit int) ([]Event, error)
}

const DefaultListLimit = 100

type MemoryLogger struct {
	mu     sync.Mutex
	events []Event
}

func NewMemoryLogger() *MemoryLogger {
	return &MemoryLogger{}
}

func (m *MemoryLogger) Record(_ context.Context, event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	m.mu.Lock()
	m.events = append(m.events, event)
	m.mu.Unlock()
	return nil
}

func (m *MemoryLogger) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

func (m *MemoryLogger) List(_ context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, 0, limit)
	for i := len(m.events) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.events[i])
	}
	return out, nil
}

type PostgresLogger struct {
	pool *pgxpool.Pool
}

func NewPostgresLogger(pool *pgxpool.Pool) *PostgresLogger {
	return &PostgresLogger{pool: pool}
}

func (p *PostgresLogger) CheckSchema(ctx context.Context) error {
	var table *string
	if err := p.pool.QueryRow(ctx, `SELECT to_regclass('public.web_operator_audit')::text`).Scan(&table); err != nil {
		return fmt.Errorf("audit: check web_operator_audit schema: %w", err)
	}
	if table == nil || *table == "" {
		return errors.New("audit: web_operator_audit table missing; run gateway migrations")
	}
	return nil
}

func (p *PostgresLogger) Record(ctx context.Context, event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
INSERT INTO web_operator_audit (
  operator_email, operator_role, action, target_type, target_id, result, error, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		event.OperatorEmail,
		event.OperatorRole,
		event.Action,
		event.TargetType,
		event.TargetID,
		event.Result,
		event.Error,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("audit: record event: %w", err)
	}
	return nil
}

func (p *PostgresLogger) List(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	rows, err := p.pool.Query(ctx, `
SELECT operator_email, operator_role, action, target_type, target_id, result, error, created_at
FROM web_operator_audit
ORDER BY created_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: list events: %w", err)
	}
	defer rows.Close()

	out := make([]Event, 0, limit)
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.OperatorEmail, &e.OperatorRole, &e.Action, &e.TargetType, &e.TargetID, &e.Result, &e.Error, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("audit: scan event: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: list events: %w", err)
	}
	return out, nil
}
