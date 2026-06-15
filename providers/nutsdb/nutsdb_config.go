/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package nutsdbstore

import (
	"errors"
	"github.com/nutsdb/nutsdb"
	"reflect"
	"time"
)

var (
	// BucketSeparator splits a full store key into the nutsdb bucket and the
	// key inside it: "bucket:key". It mirrors the bolt/bbolt providers so keys
	// are portable between the bucket-partitioned backends.
	BucketSeparator = byte(':')

	// bucketDataStructure is the nutsdb data structure backing every bucket this
	// provider creates. BTree gives ordered iteration (PrefixScan/RangeScan),
	// which OrderedCapability relies on.
	bucketDataStructure = nutsdb.DataStructureBTree

	ErrInvalidTransactionInContext = errors.New("incompatible transaction in context")
)

// Option configures the underlying nutsdb.Options using the functional options
// paradigm popularized by Rob Pike and Dave Cheney. If you're unfamiliar with
// this style, see
// https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design.html and
// https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis.
type Option func(*nutsdb.Options)

// WithNope is a no-op option.
func WithNope() Option {
	return func(o *nutsdb.Options) {}
}

// WithSyncEnable calls Sync() on every write: slower but durable.
func WithSyncEnable() Option {
	return func(o *nutsdb.Options) { o.SyncEnable = true }
}

// WithRWMode sets the read/write mode (nutsdb.FileIO or nutsdb.MMap).
func WithRWMode(mode nutsdb.RWMode) Option {
	return func(o *nutsdb.Options) { o.RWMode = mode }
}

// WithSegmentSize sets the size of each data file segment in bytes.
func WithSegmentSize(size int64) Option {
	return func(o *nutsdb.Options) { o.SegmentSize = size }
}

// WithNodeNum sets the node number, range [1,1023].
func WithNodeNum(num int64) Option {
	return func(o *nutsdb.Options) { o.NodeNum = num }
}

// WithMergeInterval sets the interval for automatic merges (0 disables them).
func WithMergeInterval(d time.Duration) Option {
	return func(o *nutsdb.Options) { o.MergeInterval = d }
}

// WithGCWhenClose triggers a GC when Close() is called.
func WithGCWhenClose() Option {
	return func(o *nutsdb.Options) { o.GCWhenClose = true }
}

// WithExpiredDeleteType selects the structure used for native TTL expiry
// (nutsdb.TimeWheel for throughput, nutsdb.TimeHeap for precision). The default
// is TimeWheel, which physically reclaims expired keys without an external
// sweeper, so this provider does not implement Sweepable.
func WithExpiredDeleteType(t nutsdb.ExpiredDeleteType) Option {
	return func(o *nutsdb.Options) { o.ExpiredDeleteType = t }
}

func OpenDatabase(dataDir string, options ...Option) (*nutsdb.DB, error) {
	opts := nutsdb.DefaultOptions
	opts.Dir = dataDir
	for _, opt := range options {
		opt(&opts)
	}
	return nutsdb.Open(opts)
}

func ObjectType() reflect.Type {
	return NutsdbStoreClass
}
