/*
 * Copyright (c) 2023 Zander Schwid & Co. LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package store

import (
	"context"
	"github.com/codeallergy/glue"
	"google.golang.org/protobuf/proto"
	"io"
	"reflect"
)

/**
Marker that TTL (time-to-live in seconds) is not defined, therefore not setup, meaning eternal record
*/
const NoTTL = 0

/**
Default batch size, could be overwritten
*/
var DefaultBatchSize = 256

var DataStoreManagerClass = reflect.TypeOf((*DataStoreManager)(nil)).Elem()
type DataStoreManager interface {

	/**
	Compact database with provided discard ratio
	 */

	Compact(discardRatio float64) error

	/**
	Backup database since timestamp
	 */

	Backup(w io.Writer, since uint64) (uint64, error)

	/**
	Restore database from dump
	 */

	Restore(r io.Reader) error

	/**
	Drop all data from database
	 */

	DropAll() error

	/**
	Drop data starts with prefix in database
	 */

	DropWithPrefix(prefix []byte) error

}

type RawEntry struct {
	Key []byte
	Value []byte
	Ttl int
	Version int64
}

type ProtoEntry struct {
	Key []byte
	Value proto.Message
	Ttl int
	Version int64
}

type CounterEntry struct {
	Key []byte
	Value uint64
	Ttl int
	Version int64
}

var DataStoreClass = reflect.TypeOf((*DataStore)(nil)).Elem()
type DataStore interface {
	glue.DisposableBean
	glue.NamedBean

	/**
	Sugar code for GetOperation object creation
	 */

	Get(ctx context.Context) *GetOperation

	/**
	Sugar code for SetOperation object creation
	*/

	Set(ctx context.Context) *SetOperation

	/**
	Sugar code for IncrementOperation object creation
	*/

	// equivalent of i++ operation, always returns previous value
	Increment(ctx context.Context) *IncrementOperation

	/**
	Sugar code for CompareAndSetOperation object creation
	*/

	CompareAndSet(ctx context.Context) *CompareAndSetOperation

	/**
	Sugar code for TouchOperation object ttl reset
	*/

	Touch(ctx context.Context) *TouchOperation

	/**
	Sugar code for RemoveOperation object creation
	*/

	Remove(ctx context.Context) *RemoveOperation

	/**
	Sugar code for EnumerateOperation object creation
	*/

	Enumerate(ctx context.Context) *EnumerateOperation

	/**
	Internal GetRaw method
	Gets binary value with optional ttl and version by key
	*/

	GetRaw(ctx context.Context, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error)

	/**
	Internal SetRaw method
	Sets binary value with optional ttl by key
	*/

	SetRaw(ctx context.Context, key, value []byte, ttlSeconds int) error

	/**
	Internal CompareAndSetRaw method
	Conditionally sets binary value with optional ttl by key only if version of record matches the provided one
	*/

	CompareAndSetRaw(ctx context.Context, key, value []byte, ttlSeconds int, version int64) (bool, error)

	/**
	Internal IncrementRaw method
	Increments binary value by delta with optional ttl and if in case value not exist initialize it by initial
	*/

	IncrementRaw(ctx context.Context, key []byte, initial, delta int64, ttlSeconds int) (int64, error)

	/**
	Internal TouchRaw method
	Touches entry and setup TTL for it
	 */

	TouchRaw(ctx context.Context, key []byte, ttlSeconds int) error

	/**
	Internal IncrementRaw method
	Removes binary value by key
	*/

	RemoveRaw(ctx context.Context, key []byte) error

	/**
	Internal EnumerateRaw method
	Enumerates all keys and optionally with values by batch size with prefix and starting by seek position, optionally in reverse order by calling callback until it returns false
	*/

	EnumerateRaw(ctx context.Context, prefix, seek []byte, batchSize int, onlyKeys bool, reverse bool, cb func(*RawEntry) bool)  error

}

var ManagedDataStoreClass = reflect.TypeOf((*ManagedDataStore)(nil)).Elem()
type ManagedDataStore interface {
	DataStore
	DataStoreManager

	/**
	Returns instance of data store object
	*/

	Instance() interface{}
}

var TransactionClass = reflect.TypeOf((*Transaction)(nil)).Elem()
type Transaction interface {

	/**
	For read-only transaction returns true
	*/

	ReadOnly() bool

	/**
	Commits transaction to data storage, in case of error commit did not happened
	*/

	Commit() error

	/**
	Rollbacks transaction, no changes in data store system
	 */

	Rollback()

	/**
	Returns instance of the transaction object
	 */

	Instance() interface{}
}

var TransactionalManagerClass = reflect.TypeOf((*TransactionalManager)(nil)).Elem()
type TransactionalManager interface {

	/**
	Starts transaction in current context, enhances and creates new context with transaction object within
	 */

	BeginTransaction(ctx context.Context, readOnly bool) context.Context

	/**
	Gets transaction object from context, commits transaction if errOps is nil or rollbacks transaction otherwise
	 */

	// commit if errOps is nil or rollback otherwise
	EndTransaction(ctx context.Context, errOps error) error

}

type transactionKey struct {
	name  string   // name of the bean with associated transaction
}

/**
Gets transaction from context by using data storage bean name

At runtime we can have multiple data stores and current transactions at the same time.
We differentiate them by data storage bean name, since only transactional data store can create transactions it
automatically initialize them with correct name.
 */

func GetTransaction(ctx context.Context, beanName string) (Transaction, bool) {
	val := ctx.Value(transactionKey{ name: beanName })
	tx, ok := val.(Transaction)
	return tx, ok
}

/**
Adds transaction to context with associated bean name. Utility method to help data storage systems to work
with transactions.
 */

func WithTransaction(ctx context.Context, beanName string, tx Transaction) context.Context {
	return context.WithValue(ctx, transactionKey{ name: beanName }, tx)
}

/**
Union interface of DataStore and TransactionalManager
 */

var TransactionalDataStoreClass = reflect.TypeOf((*TransactionalDataStore)(nil)).Elem()
type TransactionalDataStore interface {
	DataStore
	TransactionalManager
}

/**
Union interface of DataStore, DataStoreManager and TransactionalManager
*/

var ManagedTransactionalDataStoreClass = reflect.TypeOf((*ManagedTransactionalDataStore)(nil)).Elem()
type ManagedTransactionalDataStore interface {
	DataStore
	DataStoreManager
	TransactionalManager
}

