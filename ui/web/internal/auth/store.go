package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoSession = errors.New("auth: session not found")

type Operator struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatarUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Session struct {
	TokenHash string
	Operator  Operator
}

type Store interface {
	Create(ctx context.Context, identity Identity, ttl time.Duration) (rawToken string, session Session, err error)
	Get(ctx context.Context, rawToken string) (Session, error)
	Delete(ctx context.Context, rawToken string) error
}

type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: map[string]Session{}}
}

func (m *MemoryStore) Create(_ context.Context, identity Identity, ttl time.Duration) (string, Session, error) {
	raw, hash, err := newSessionToken()
	if err != nil {
		return "", Session{}, err
	}
	session := Session{
		TokenHash: hash,
		Operator: Operator{
			Email:     identity.Email,
			Name:      identity.Name,
			AvatarURL: identity.AvatarURL,
			ExpiresAt: time.Now().UTC().Add(ttl),
		},
	}
	m.mu.Lock()
	m.sessions[hash] = session
	m.mu.Unlock()
	return raw, session, nil
}

func (m *MemoryStore) Get(_ context.Context, rawToken string) (Session, error) {
	hash := sessionHash(rawToken)
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[hash]
	if !ok {
		return Session{}, ErrNoSession
	}
	if time.Now().UTC().After(session.Operator.ExpiresAt) {
		delete(m.sessions, hash)
		return Session{}, ErrNoSession
	}
	return session, nil
}

func (m *MemoryStore) Delete(_ context.Context, rawToken string) error {
	m.mu.Lock()
	delete(m.sessions, sessionHash(rawToken))
	m.mu.Unlock()
	return nil
}

type PostgresStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool, now: func() time.Time { return time.Now().UTC() }}
}

func (p *PostgresStore) CheckSchema(ctx context.Context) error {
	var table *string
	if err := p.pool.QueryRow(ctx, `SELECT to_regclass('public.web_operator_sessions')::text`).Scan(&table); err != nil {
		return fmt.Errorf("auth: check web session schema: %w", err)
	}
	if table == nil || *table == "" {
		return errors.New("auth: web_operator_sessions table missing; run gateway migrations")
	}
	return nil
}

func (p *PostgresStore) Create(ctx context.Context, identity Identity, ttl time.Duration) (string, Session, error) {
	raw, hash, err := newSessionToken()
	if err != nil {
		return "", Session{}, err
	}
	expiresAt := p.now().Add(ttl)
	_, err = p.pool.Exec(ctx, `
INSERT INTO web_operator_sessions (
  id_hash, operator_email, operator_name, operator_avatar_url, expires_at
) VALUES ($1, $2, $3, $4, $5)`,
		hash, identity.Email, identity.Name, identity.AvatarURL, expiresAt)
	if err != nil {
		return "", Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return raw, Session{
		TokenHash: hash,
		Operator: Operator{
			Email:     identity.Email,
			Name:      identity.Name,
			AvatarURL: identity.AvatarURL,
			ExpiresAt: expiresAt,
		},
	}, nil
}

func (p *PostgresStore) Get(ctx context.Context, rawToken string) (Session, error) {
	hash := sessionHash(rawToken)
	var session Session
	session.TokenHash = hash
	err := p.pool.QueryRow(ctx, `
SELECT operator_email, operator_name, operator_avatar_url, expires_at
FROM web_operator_sessions
WHERE id_hash = $1 AND expires_at > NOW()`, hash).Scan(
		&session.Operator.Email,
		&session.Operator.Name,
		&session.Operator.AvatarURL,
		&session.Operator.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrNoSession
		}
		return Session{}, fmt.Errorf("auth: get session: %w", err)
	}
	_, _ = p.pool.Exec(ctx, `
UPDATE web_operator_sessions
SET last_seen_at = NOW()
WHERE id_hash = $1`, hash)
	return session, nil
}

func (p *PostgresStore) Delete(ctx context.Context, rawToken string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM web_operator_sessions WHERE id_hash = $1`, sessionHash(rawToken))
	if err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

func newSessionToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("auth: generate session token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, sessionHash(raw), nil
}

func sessionHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
