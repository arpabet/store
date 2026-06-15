/*
 * Copyright (c) 2025-2026 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package memstore

import (
	"github.com/jellydator/ttlcache/v3"
	"reflect"
)

func OpenDatabase(options ...Option) *ttlcache.Cache[string, []byte] {

	conf := &Config{
		DefaultExpiration: ttlcache.NoTTL,
	}

	for _, opt := range options {
		opt.apply(conf)
	}

	opts := []ttlcache.Option[string, []byte]{
		// store semantics: reading a key must not extend its TTL
		ttlcache.WithDisableTouchOnHit[string, []byte](),
	}
	if conf.DefaultExpiration > 0 {
		opts = append(opts, ttlcache.WithTTL[string, []byte](conf.DefaultExpiration))
	}

	return ttlcache.New[string, []byte](opts...)
}

func ObjectType() reflect.Type {
	return CacheStoreClass
}
