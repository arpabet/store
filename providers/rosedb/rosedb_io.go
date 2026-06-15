/*
 * Copyright (c) 2025 Karagatan LLC.
 * SPDX-License-Identifier: Apache-2.0
 */

package rosedbstore

import (
	"bufio"
	"encoding/binary"
	"io"
)

// uvarint length-prefixed framing for Backup/Restore (key/value pairs).

func newByteReader(r io.Reader) *bufio.Reader {
	return bufio.NewReader(r)
}

func writeBinary(w io.Writer, b []byte) error {
	var lenBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(b)))
	if _, err := w.Write(lenBuf[:n]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func readBinary(br *bufio.Reader) ([]byte, error) {
	size, err := binary.ReadUvarint(br)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
