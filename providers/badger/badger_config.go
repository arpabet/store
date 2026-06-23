/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"golang.org/x/xerrors"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"go.uber.org/zap"
	"path/filepath"
	"time"
)

var (
	DefaultValueLogMaxEntries  = uint32(1024 * 1024 * 1024)
	DefaultKeyRotationDuration = time.Hour * 24 * 7

	ErrTransactionCanceled         = xerrors.New("transaction was canceled")
	ErrInvalidTransactionInContext = xerrors.New("incompatible transaction in context")
)

type StoreOptions struct {
	OpenTimeout  time.Duration
	MaxPendingWrites int
	ConcurrentRetryNum int
}

func DefaultStoreOptions() *StoreOptions {
	return &StoreOptions {
		OpenTimeout: time.Second,
		MaxPendingWrites: 4096,
		ConcurrentRetryNum: 5,
	}
}

// Option configures badger using the functional options paradigm
// popularized by Rob Pike and Dave Cheney. If you're unfamiliar with this style,
// see https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design.html and
// https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis.
type Option interface {
	apply(*badger.Options, *StoreOptions)
}

// OptionFunc implements Option interface.
type optionFunc func(*badger.Options, *StoreOptions)

// apply the configuration to the provided config.
func (fn optionFunc) apply(bo *badger.Options, so *StoreOptions) {
	fn(bo, so)
}

// option that do nothing
func WithNope() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
	})
}

func WithReadOnly() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.ReadOnly = true
	})
}

func WithInMemory() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.InMemory = true
	})
}

func WithNumVersionsToKeep(num int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NumVersionsToKeep = num
	})
}

func WithSyncWrites() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.SyncWrites = true
	})
}

func WithOpenTimeout(timeout time.Duration) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		so.OpenTimeout = timeout
	})
}

func WithMaxPendingWrites(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		so.MaxPendingWrites = value
	})
}

func WithConcurrentRetryNum(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		so.ConcurrentRetryNum = value
	})
}

func WithDataDir(dataDir string) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.Dir = dataDir
		opts.ValueDir = dataDir
	})
}

func WithKeyValueDir(dataDir string) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.Dir = filepath.Join(dataDir, "key")
		opts.ValueDir = filepath.Join(dataDir, "value")
	})
}

func WithMetricsEnabled() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.MetricsEnabled = true
	})
}

func WithNumGoroutines(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NumGoroutines = value
	})
}

func WithEncryptionKey(storageKey []byte) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.EncryptionKey = storageKey
		opts.EncryptionKeyRotationDuration = DefaultKeyRotationDuration
	})
}

func WithCompression(zstd bool) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		if zstd {
			opts.Compression = options.ZSTD
			opts.ZSTDCompressionLevel = 9
		} else {
			opts.Compression = options.None
		}
	})
}

func WithSnappy() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.Compression = options.Snappy
	})
}

func WithLogger(debug bool) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.Logger = NewLogger(debug)
	})
}

func WithZapLogger(log *zap.Logger, debug bool) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.Logger = NewZapLogger(log, debug)
	})
}


// Fine tuning options.

func WithMemTableSize(value int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.MemTableSize = value
	})
}

func WithBaseTableSize(value int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.BaseTableSize = value
	})
}

func WithBaseLevelSize(value int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.BaseLevelSize = value
	})
}

func WithLevelSizeMultiplier(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.LevelSizeMultiplier = value
	})
}

func WithTableSizeMultiplier(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.TableSizeMultiplier = value
	})
}

func WithMaxLevels(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.MaxLevels = value
	})
}

func WithVLogPercentile(value float64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.VLogPercentile = value
	})
}

func WithValueThreshold(threshold int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.ValueThreshold = threshold
	})
}

func WithNumMemtables(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NumMemtables = value
	})
}

// Changing BlockSize across DB runs will not break badger. The block size is
// read from the block index stored at the end of the table.
func WithBlockSize(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.BlockSize = value
	})
}

func WithBloomFalsePositive(value float64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.BloomFalsePositive = value
	})
}

func WithBlockCacheSize(value int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.BlockCacheSize = value
	})
}

func WithIndexCacheSize(value int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.IndexCacheSize = value
	})
}

func WithNumLevelZeroTables(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NumLevelZeroTables = value
	})
}

func WithNumLevelZeroTablesStall(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NumLevelZeroTablesStall = value
	})
}

func WithValueLogFileSize(value int64) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.ValueLogFileSize = value
	})
}

func WithValueLogMaxEntries(value uint32) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.ValueLogMaxEntries = value
	})
}

func WithNumCompactors(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NumCompactors = value
	})
}

func WithCompactL0OnClose() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.CompactL0OnClose = true
	})
}

func WithLmaxCompaction() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.LmaxCompaction = true
	})
}

// When set, checksum will be validated for each entry read from the value log file.
func WithVerifyValueChecksum() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.VerifyValueChecksum = true
	})
}

// BypassLockGaurd will bypass the lock guard on badger. Bypassing lock
// guard can cause data corruption if multiple badger instances are using
// the same directory. Use this options with caution.
func WithLBypassLockGuard() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.BypassLockGuard = true
	})
}

// ChecksumVerificationMode decides when db should verify checksums for SSTable blocks.
func WithChecksumVerificationMode(mode options.ChecksumVerificationMode) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.ChecksumVerificationMode = mode
	})
}

// DetectConflicts determines whether the transactions would be checked for
// conflicts. The transactions can be processed at a higher rate when
// conflict detection is disabled.
func WithDetectConflicts() Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.DetectConflicts = true
	})
}

func WithNamespaceOffset(value int) Option {
	return optionFunc(func(opts *badger.Options, so *StoreOptions) {
		opts.NamespaceOffset = value
	})
}
