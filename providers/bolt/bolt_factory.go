/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package boltstore

import (
	"github.com/boltdb/bolt"
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
