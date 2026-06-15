/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package nutsdbstore

import (
	"context"
	"errors"
	"github.com/nutsdb/nutsdb"
	"go.arpabet.com/store"
	"sync"
)

// txSession bundles a native transaction with the set of buckets already
// created within it. nutsdb's ExistBucket only reflects committed state, so
// calling NewBucket twice for the same name inside one transaction corrupts the
// commit; the set guarantees each bucket is created at most once per transaction.
type txSession struct {
	tx      *nutsdb.Tx
	mu      *sync.Mutex
	created map[string]bool
}

func newTxSession(tx *nutsdb.Tx) *txSession {
	return &txSession{tx: tx, mu: &sync.Mutex{}, created: make(map[string]bool)}
}

// ensureBucket creates the BTree bucket if needed, exactly once per transaction.
func (s *txSession) ensureBucket(bucket string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created[bucket] {
		return nil
	}
	if s.tx.ExistBucket(bucketDataStructure, bucket) {
		s.created[bucket] = true
		return nil
	}
	if err := s.tx.NewBucket(bucketDataStructure, bucket); err != nil && !errors.Is(err, nutsdb.ErrBucketAlreadyExist) {
		return err
	}
	s.created[bucket] = true
	return nil
}

// implNutsdbTransaction wraps a native nutsdb transaction and buffers the watch
// events produced by operations run within it. The events are published to the
// hub only on a successful Commit and dropped on Rollback, so watchers never see
// changes that were never durably written.
type implNutsdbTransaction struct {
	store    *implNutsdbStore
	tx       *nutsdb.Tx
	readOnly bool

	mu      sync.Mutex
	notices []*store.WatchEvent

	session *txSession // persists the created-bucket set across all ops in this tx
}

func (t *implNutsdbStore) newTransaction(tx *nutsdb.Tx, readOnly bool) *implNutsdbTransaction {
	return &implNutsdbTransaction{store: t, tx: tx, readOnly: readOnly, session: newTxSession(tx)}
}

func (x *implNutsdbTransaction) ReadOnly() bool {
	return x.readOnly
}

func (x *implNutsdbTransaction) Commit() error {
	if err := x.tx.Commit(); err != nil {
		return wrapError(err)
	}
	x.mu.Lock()
	notices := x.notices
	x.notices = nil
	x.mu.Unlock()
	for _, ev := range notices {
		x.store.hub.Notify(ev)
	}
	return nil
}

func (x *implNutsdbTransaction) Rollback() {
	x.mu.Lock()
	x.notices = nil
	x.mu.Unlock()
	_ = x.tx.Rollback()
}

func (x *implNutsdbTransaction) Instance() interface{} {
	return x.tx
}

func (x *implNutsdbTransaction) appendNotice(ev *store.WatchEvent) {
	x.mu.Lock()
	x.notices = append(x.notices, ev)
	x.mu.Unlock()
}

func (t *implNutsdbStore) BeginTransaction(ctx context.Context, readOnly bool) context.Context {
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		// reuse a compatible parent transaction (a read-only request can ride on
		// any parent; a write request needs a writable parent)
		if readOnly || !tx.ReadOnly() {
			return store.WithTransaction(ctx, t.name, store.NewInnerTransaction(tx))
		}
	}
	tx, err := t.db.Begin(!readOnly)
	if err != nil {
		// surface the failure lazily: subsequent ops on this nil-backed context
		// will return ErrInvalidTransactionInContext rather than panic.
		return ctx
	}
	return store.WithTransaction(ctx, t.name, t.newTransaction(tx, readOnly))
}

func (t *implNutsdbStore) EndTransaction(ctx context.Context, errOps error) error {
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		if errOps == nil {
			return tx.Commit()
		}
		tx.Rollback()
		return errOps
	}
	return errOps
}

// inWrite runs fn in a writable transaction: the caller's transaction when one
// is present (and writable), otherwise a fresh atomic db.Update. The session
// carries the per-transaction created-bucket set so ensureBucket stays idempotent.
func (t *implNutsdbStore) inWrite(ctx context.Context, fn func(sess *txSession) error) error {
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		if tx.ReadOnly() {
			return store.ErrReadOnlyTxn
		}
		if nt, ok := tx.(*implNutsdbTransaction); ok {
			return fn(nt.session)
		}
		// nested (inner) transaction: reuse the underlying tx with a fresh session
		ntx, ok := tx.Instance().(*nutsdb.Tx)
		if !ok {
			return ErrInvalidTransactionInContext
		}
		return fn(newTxSession(ntx))
	}
	return t.db.Update(func(ntx *nutsdb.Tx) error {
		return fn(newTxSession(ntx))
	})
}

// inRead runs fn in a read view: the caller's transaction when one is present,
// otherwise a fresh db.View.
func (t *implNutsdbStore) inRead(ctx context.Context, fn func(tx *nutsdb.Tx) error) error {
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		ntx, ok := tx.Instance().(*nutsdb.Tx)
		if !ok {
			return ErrInvalidTransactionInContext
		}
		return fn(ntx)
	}
	return t.db.View(fn)
}

// emit publishes a watch event. Inside an explicit transaction the event is
// buffered on that transaction and flushed on Commit; otherwise (the standalone
// db.Update path, already committed by now) it is published immediately.
func (t *implNutsdbStore) emit(ctx context.Context, fullKey, value []byte, eventType store.WatchEventType, version int64) {
	ev := &store.WatchEvent{Key: cloneBytes(fullKey), Type: eventType, Version: version}
	if value != nil {
		ev.Value = cloneBytes(value)
	}
	if tx, ok := store.GetTransaction(ctx, t.name); ok {
		if nt, ok := tx.(*implNutsdbTransaction); ok {
			nt.appendNotice(ev)
			return
		}
		// nested (inner) transaction: the root wrapper is not reachable here, so
		// deliver eagerly (best-effort, matching the hub's at-most-once contract).
	}
	t.hub.Notify(ev)
}
