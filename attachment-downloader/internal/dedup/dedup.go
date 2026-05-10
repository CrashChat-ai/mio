// Package dedup provides HEAD-before-PUT helpers for content-addressable
// storage. Race-free across concurrent callers via the backend's IfNotExists
// option (translated to GCS DoesNotExist precondition / S3 If-None-Match).
package dedup

import (
	"context"
	"errors"

	"github.com/crashchat-ai/mio/attachment-downloader/internal/storage"
)

// Result describes the outcome of a dedup-aware persist.
type Result struct {
	// AlreadyExisted is true if the object existed before this call started.
	AlreadyExisted bool
	// CollisionResolved is true if IfNotExists raced with another writer.
	CollisionResolved bool
}

// Stat returns the existing object at key, or nil if absent.
// Errors other than ErrNotFound bubble up.
func Stat(ctx context.Context, s storage.Storage, key string) (*storage.Object, error) {
	obj, err := s.Stat(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return obj, nil
}

// PersistIfAbsent runs a HEAD; if the key is absent, calls writeFn to stream
// the bytes through Put with IfNotExists=true. The race between two callers
// resolves cleanly via ErrAlreadyExists, surfaced as Result.CollisionResolved.
//
// writeFn is called at most once. It must close any input it owns.
func PersistIfAbsent(
	ctx context.Context,
	s storage.Storage,
	key string,
	writeFn func() error,
) (Result, error) {
	if obj, err := Stat(ctx, s, key); err != nil {
		return Result{}, err
	} else if obj != nil {
		return Result{AlreadyExisted: true}, nil
	}

	if err := writeFn(); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			return Result{CollisionResolved: true}, nil
		}
		return Result{}, err
	}
	return Result{}, nil
}
