// SPDX-License-Identifier: AGPL-3.0-only

package commandprovider

import (
	"bytes"
	"strings"
	"sync"

	"github.com/globulario/sensei/golang/architecture/providerport"
)

type limitedBuffer struct {
	mu       sync.Mutex
	limit    int64
	buffer   bytes.Buffer
	overflow bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	inputLength := len(data)
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.overflow = true
		return inputLength, nil
	}
	kept := data
	if int64(len(kept)) > remaining {
		kept = kept[:remaining]
		b.overflow = true
	}
	_, _ = b.buffer.Write(kept)
	return inputLength, nil
}

func (b *limitedBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

func (b *limitedBuffer) exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflow
}

type observationWriter struct {
	mu       sync.Mutex
	limit    int64
	observer providerport.Observer
	used     int64
	overflow bool
	disabled bool
	pending  string
}

func newObservationWriter(limit int64, observer providerport.Observer) *observationWriter {
	return &observationWriter{limit: limit, observer: observer}
}

func (w *observationWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	inputLength := len(data)
	remaining := w.limit - w.used
	if remaining <= 0 {
		w.overflow = true
		return inputLength, nil
	}
	kept := data
	if int64(len(kept)) > remaining {
		kept = kept[:remaining]
		w.overflow = true
	}
	w.used += int64(len(kept))
	w.pending += string(kept)
	w.emitCompleteLines()
	return inputLength, nil
}

func (w *observationWriter) emitCompleteLines() {
	for !w.disabled {
		index := strings.IndexByte(w.pending, '\n')
		if index < 0 {
			return
		}
		line := strings.TrimSpace(w.pending[:index])
		w.pending = w.pending[index+1:]
		w.emit(line)
	}
}

func (w *observationWriter) emit(detail string) {
	if w.disabled || detail == "" || w.observer == nil {
		return
	}
	if err := w.observer.Observe(detail); err != nil {
		// Observation collection is evidence only. Once the O2 observer refuses
		// more evidence because its own precommitted limits are reached, stderr
		// cannot convert that refusal into command or transition authority.
		w.disabled = true
	}
}

func (w *observationWriter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.emit(strings.TrimSpace(w.pending))
	w.pending = ""
}

func (w *observationWriter) exceeded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overflow
}
