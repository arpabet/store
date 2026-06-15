/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"github.com/dgraph-io/badger/v4"
	"reflect"
	"strings"
	"time"
)

func OpenDatabase(dataDir string, options ...Option) (*badger.DB, *StoreOptions, error) {

	storeOpts := DefaultStoreOptions()

	opts := badger.DefaultOptions(dataDir)
	opts.ValueLogMaxEntries = DefaultValueLogMaxEntries

	for _, opt := range options {
		opt.apply(&opts, storeOpts)
	}

	deadline := time.Now().Add(storeOpts.OpenTimeout)
	for {

		db, err := badger.Open(opts)
		if err != nil {
			if strings.Contains(err.Error(), "Cannot acquire directory lock") && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return nil, storeOpts, err
		}

		return db, storeOpts, nil
	}

}

func ObjectType() reflect.Type {
	return BadgerStoreClass
}
