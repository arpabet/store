/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package bboltstore

import (
	bolt "go.etcd.io/bbolt"
	"os"
	"reflect"
)

func OpenDatabase(dataFile string, dataFilePerm os.FileMode, options ...Option) (*bolt.DB, error) {

	opts := &bolt.Options{}
	for _, opt := range options {
		opt.apply(opts)
	}

	return bolt.Open(dataFile, dataFilePerm, opts)
}

func ObjectType() reflect.Type {
	return BoltStoreClass
}
