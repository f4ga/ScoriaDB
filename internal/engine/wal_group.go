// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"time"
)

// groupCommitWriter реализует буферизованную асинхронную запись с групповым коммитом.
type groupCommitWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	file      *os.File
	ticker    *time.Ticker
	done      chan struct{}
	flushChan chan struct{}
	flushErr  error // последняя ошибка flush, защищена mu
	closed    bool
}

// newGroupCommitWriter создаёт новый groupCommitWriter.
// Интервал flushInterval определяет, как часто данные сбрасываются на диск.
// Если flushInterval <= 0, используется значение по умолчанию 10 мс.
func newGroupCommitWriter(file *os.File, flushInterval time.Duration) *groupCommitWriter {
	if flushInterval <= 0 {
		flushInterval = 10 * time.Millisecond
	}
	gcw := &groupCommitWriter{
		file:      file,
		ticker:    time.NewTicker(flushInterval),
		done:      make(chan struct{}),
		flushChan: make(chan struct{}, 1),
	}
	go gcw.flushLoop()
	return gcw
}

// Write добавляет данные в буфер. Возвращает ошибку только если буфер не может быть записан
// (например, недостаточно памяти). Запись на диск откладывается до следующего flush.
func (w *groupCommitWriter) Write(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("groupCommitWriter closed")
	}
	_, err := w.buf.Write(data)
	return err
}

// Flush принудительно сбрасывает буфер на диск и выполняет Sync.
// Возвращает ошибку, если сброс не удался.
func (w *groupCommitWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

// flushLocked выполняет сброс, предполагая, что мьютекс уже захвачен.
func (w *groupCommitWriter) flushLocked() error {
	if w.buf.Len() == 0 {
		return nil
	}
	data := w.buf.Bytes()
	if _, err := w.file.Write(data); err != nil {
		w.flushErr = err
		return err
	}
	if err := w.file.Sync(); err != nil {
		w.flushErr = err
		return err
	}
	w.buf.Reset()
	return nil
}

// flushLoop — фоновая горутина, которая периодически сбрасывает буфер.
func (w *groupCommitWriter) flushLoop() {
	for {
		select {
		case <-w.ticker.C:
			w.mu.Lock()
			_ = w.flushLocked() // ошибка игнорируется, но сохраняется в flushErr
			w.mu.Unlock()
		case <-w.flushChan:
			w.mu.Lock()
			_ = w.flushLocked() // ошибка игнорируется, но сохраняется в flushErr
			w.mu.Unlock()
			return
		case <-w.done:
			return
		}
	}
}

// Close останавливает фоновую горутину, сбрасывает оставшиеся данные и закрывает файл.
// Файл не закрывается, так как он принадлежит WAL.
func (w *groupCommitWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	close(w.done)
	w.ticker.Stop()
	// Принудительный сброс оставшихся данных
	if err := w.flushLocked(); err != nil {
		return err
	}
	// Не закрываем w.file, так как это делает WAL
	return nil
}

// Error возвращает последнюю ошибку flush (если была).
func (w *groupCommitWriter) Error() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushErr
}
