package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var namespaceRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// KV is a typed key-value store scoped to one (scope, namespace).
// Construct via app.ProfileKV or app.GlobalKV; do not instantiate
// directly outside this package.
type KV[T any] struct {
	s         Storage
	scope     string
	namespace string
}

// Entry wraps a stored value with its metadata.
type Entry[T any] struct {
	Key       string
	Value     T
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time
}

// NewProfileKV returns a KV scoped to the given profile. Internal:
// app-layer accessors are the public entry point.
func NewProfileKV[T any](s Storage, profile, namespace string) (*KV[T], error) {
	if profile == "" {
		return nil, fmt.Errorf("storage: profile required")
	}
	if !namespaceRe.MatchString(namespace) {
		return nil, fmt.Errorf("storage: invalid namespace %q (want %s)", namespace, namespaceRe)
	}
	return &KV[T]{s: s, scope: s.scopeProfile(profile), namespace: namespace}, nil
}

// NewGlobalKV returns a KV in the global scope. Internal.
func NewGlobalKV[T any](s Storage, namespace string) (*KV[T], error) {
	if !namespaceRe.MatchString(namespace) {
		return nil, fmt.Errorf("storage: invalid namespace %q (want %s)", namespace, namespaceRe)
	}
	return &KV[T]{s: s, scope: s.scopeGlobal(), namespace: namespace}, nil
}

func (kv *KV[T]) Get(ctx context.Context, key string) (T, error) {
	e, err := kv.GetEntry(ctx, key)
	if err != nil {
		var zero T
		return zero, err
	}
	return e.Value, nil
}

func (kv *KV[T]) GetEntry(ctx context.Context, key string) (Entry[T], error) {
	now := time.Now().UnixNano()
	row := kv.s.db().QueryRowContext(ctx, `
		SELECT value, created_at, updated_at, expires_at
		FROM kv
		WHERE scope = ? AND namespace = ? AND key = ?
		  AND (expires_at IS NULL OR expires_at > ?)
	`, kv.scope, kv.namespace, key, now)

	var (
		raw       []byte
		createdAt int64
		updatedAt int64
		expiresAt sql.NullInt64
	)
	if err := row.Scan(&raw, &createdAt, &updatedAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry[T]{}, ErrNotFound
		}
		return Entry[T]{}, fmt.Errorf("storage: kv get: %w", err)
	}

	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return Entry[T]{}, fmt.Errorf("%w: %w", ErrKVDecode, err)
	}

	e := Entry[T]{
		Key:       key,
		Value:     v,
		CreatedAt: time.Unix(0, createdAt),
		UpdatedAt: time.Unix(0, updatedAt),
	}
	if expiresAt.Valid {
		t := time.Unix(0, expiresAt.Int64)
		e.ExpiresAt = &t
	}
	return e, nil
}

func (kv *KV[T]) Put(ctx context.Context, key string, value T) error {
	return kv.put(ctx, key, value, nil)
}

func (kv *KV[T]) PutWithTTL(ctx context.Context, key string, value T, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("storage: ttl must be positive")
	}
	exp := time.Now().Add(ttl).UnixNano()
	return kv.put(ctx, key, value, &exp)
}

func (kv *KV[T]) put(ctx context.Context, key string, value T, expiresAt *int64) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("storage: kv encode: %w", err)
	}
	now := time.Now().UnixNano()

	var expArg any
	if expiresAt != nil {
		expArg = *expiresAt
	}

	_, err = kv.s.db().ExecContext(ctx, `
		INSERT INTO kv(scope, namespace, key, value, created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, namespace, key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at,
			expires_at = excluded.expires_at
	`, kv.scope, kv.namespace, key, raw, now, now, expArg)
	if err != nil {
		return fmt.Errorf("storage: kv put: %w", err)
	}
	return nil
}

func (kv *KV[T]) Delete(ctx context.Context, key string) error {
	_, err := kv.s.db().ExecContext(ctx,
		`DELETE FROM kv WHERE scope = ? AND namespace = ? AND key = ?`,
		kv.scope, kv.namespace, key)
	if err != nil {
		return fmt.Errorf("storage: kv delete: %w", err)
	}
	return nil
}

func (kv *KV[T]) Has(ctx context.Context, key string) (bool, error) {
	now := time.Now().UnixNano()
	var n int
	err := kv.s.db().QueryRowContext(ctx, `
		SELECT count(*) FROM kv
		WHERE scope = ? AND namespace = ? AND key = ?
		  AND (expires_at IS NULL OR expires_at > ?)
	`, kv.scope, kv.namespace, key, now).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("storage: kv has: %w", err)
	}
	return n > 0, nil
}

func (kv *KV[T]) List(ctx context.Context) ([]Entry[T], error) {
	now := time.Now().UnixNano()
	rows, err := kv.s.db().QueryContext(ctx, `
		SELECT key, value, created_at, updated_at, expires_at
		FROM kv
		WHERE scope = ? AND namespace = ?
		  AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY key
	`, kv.scope, kv.namespace, now)
	if err != nil {
		return nil, fmt.Errorf("storage: kv list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Entry[T]
	for rows.Next() {
		var (
			key       string
			raw       []byte
			createdAt int64
			updatedAt int64
			expiresAt sql.NullInt64
		)
		if err := rows.Scan(&key, &raw, &createdAt, &updatedAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("storage: kv list scan: %w", err)
		}
		var v T
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrKVDecode, err)
		}
		e := Entry[T]{
			Key:       key,
			Value:     v,
			CreatedAt: time.Unix(0, createdAt),
			UpdatedAt: time.Unix(0, updatedAt),
		}
		if expiresAt.Valid {
			t := time.Unix(0, expiresAt.Int64)
			e.ExpiresAt = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
