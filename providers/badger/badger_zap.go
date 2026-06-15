/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package badgerstore

import (
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"go.uber.org/zap"
)

type zapLoggerAdapter struct {
	log   *zap.Logger
	debug bool
}

func (t *zapLoggerAdapter) Errorf(format string, args ...interface{}) {
	t.log.Error("Badger", zap.String("log", fmt.Sprintf(format, args...)))
}

func (t *zapLoggerAdapter) Warningf(format string, args ...interface{}) {
	t.log.Warn("Badger", zap.String("log", fmt.Sprintf(format, args...)))
}

func (t *zapLoggerAdapter) Infof(format string, args ...interface{}) {
	t.log.Info("Badger", zap.String("log", fmt.Sprintf(format, args...)))
}

func (t *zapLoggerAdapter) Debugf(format string, args ...interface{}) {
	if t.debug {
		t.log.Debug("Badger", zap.String("log", fmt.Sprintf(format, args...)))
	}
}

func NewZapLogger(log *zap.Logger, debug bool) badger.Logger {
	return &zapLoggerAdapter{log: log, debug: debug}
}
