/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: BUSL-1.1
 */

package badgerstore

import (
	"github.com/dgraph-io/badger/v3"
	"log"
)

type loggerAdapter struct {
	debug bool
}

func (t *loggerAdapter) Errorf(format string, args ...interface{}) {
	log.Printf("ERROR "+format, args...)
}

func (t *loggerAdapter) Warningf(format string, args ...interface{}) {
	log.Printf("WARN "+format, args...)
}

func (t *loggerAdapter) Infof(format string, args ...interface{}) {
	log.Printf("INFO "+format, args...)
}

func (t *loggerAdapter) Debugf(format string, args ...interface{}) {
	if t.debug {
		log.Printf("DEBUG "+format, args...)
	}
}

func NewLogger(debug bool) badger.Logger {
	return &loggerAdapter{debug: debug}
}
