#!/bin/bash
# Copyright 2026 Ekaterina Godulyan
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

DISK="nvme0n1"
OPS=100000
KEY_SIZE=16
VAL_SIZE=16
PAYLOAD=$((OPS * (KEY_SIZE + VAL_SIZE)))
ITERATIONS=3
RESULTS=()

echo "=== Точный замер Write Amplification ScoriaDB ==="
echo "Диск: $DISK"
echo "Операций: $OPS"
echo "Полезных данных: $PAYLOAD байт (~$((PAYLOAD / 1024 / 1024)) МБ)"
echo "Повторов: $ITERATIONS"
echo

# Проверяем наличие основного диска (major:minor с minor=0)
if ! awk '$2 == 0 && $3 == "'"$DISK"'" {found=1; exit} END {exit !found}' /proc/diskstats; then
    echo "❌ Диск $DISK не найден"
    exit 1
fi

for i in $(seq 1 $ITERATIONS); do
    echo "--- Прогон $i ---"

    rm -rf ./data
    mkdir -p ./data

    SECTOR_BEFORE=$(awk '$2 == 0 && $3 == "'"$DISK"'" {print $10}' /proc/diskstats)
    echo "  Секторов до: $SECTOR_BEFORE"

    ./bench-put $OPS > /dev/null 2>&1
    if [ $? -ne 0 ]; then
        echo "❌ bench-put завершился с ошибкой"
        exit 1
    fi

    sleep 30
    sync

    SECTOR_AFTER=$(awk '$2 == 0 && $3 == "'"$DISK"'" {print $10}' /proc/diskstats)
    echo "  Секторов после: $SECTOR_AFTER"

    SECTOR_DELTA=$((SECTOR_AFTER - SECTOR_BEFORE))
    BYTES_DISK=$((SECTOR_DELTA * 512))
    WA=$(echo "scale=3; $BYTES_DISK / $PAYLOAD" | bc -l)

    echo "  Записано на диск: $BYTES_DISK байт"
    echo "  WA = $WA"
    RESULTS+=("$WA")
    echo
done

MIN_WA=$(printf '%s\n' "${RESULTS[@]}" | sort -n | head -1)
echo "========================================"
echo "Минимальный WA из $ITERATIONS прогонов = $MIN_WA"
echo "========================================"
