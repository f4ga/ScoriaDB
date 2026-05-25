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

import "time"

// WALOptions содержит параметры для настройки Write-Ahead Log.
type WALOptions struct {
	// GroupCommitEnabled включает групповой коммит (буферизацию записей).
	// По умолчанию false — каждая запись синхронно сбрасывается на диск.
	// Включение группового коммита значительно увеличивает пропускную способность записи,
	// но снижает durability: записи, сделанные после последнего сброса на диск,
	// могут быть потеряны при краше процесса или отключении питания.
	// Рекомендуется для workload'ов, где допустима потеря последних миллисекунд записей
	// (логи, метрики, кэши).
	GroupCommitEnabled bool

	// GroupCommitInterval определяет интервал сброса буфера при включённом групповом коммите.
	// По умолчанию 10 мс.
	// Меньшие значения уменьшают задержку подтверждения записи, но увеличивают нагрузку на диск.
	// Большие значения улучшают пропускную способность, но увеличивают риск потери данных при краше.
	GroupCommitInterval time.Duration

	// MaxBufferSize максимальный размер буфера в байтах перед принудительным сбросом.
	// Если 0, ограничения нет.
	// При достижении этого размера буфер будет сброшен на диск независимо от таймера.
	// Полезно для ограничения потребления памяти.
	MaxBufferSize int
}

// DefaultWALOptions возвращает настройки WAL по умолчанию.
func DefaultWALOptions() WALOptions {
	return WALOptions{
		GroupCommitEnabled:  false,
		GroupCommitInterval: 10 * time.Millisecond,
		MaxBufferSize:       0,
	}
}

type EngineOptions struct {
	DataDir string
	WALOpts WALOptions
}

func DefaultEngineOptions(dataDir string) EngineOptions {
	return EngineOptions{
		DataDir: dataDir,
		WALOpts: DefaultWALOptions(),
	}
}
