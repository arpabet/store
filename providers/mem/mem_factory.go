/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package memstore

import (
	"github.com/patrickmn/go-cache"
	"reflect"
	"time"
)

func OpenDatabase(options ...Option) *cache.Cache {

	conf := &Config{
		DefaultExpiration: cache.NoExpiration,
		CleanupInterval:  time.Hour,
	}

	for _, opt := range options {
		opt.apply(conf)
	}

	return cache.New(conf.DefaultExpiration, conf.CleanupInterval)
}

func ObjectType() reflect.Type {
	return CacheStoreClass
}



