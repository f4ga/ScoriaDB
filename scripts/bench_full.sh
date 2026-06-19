#!/bin/bash
# scripts/bench_color.sh — цветной запуск всех бенчмарков по отдельности

# Цвета ANSI
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Настройки
BENCHTIME=${1:-10s}
CPU=${2:-8}
COUNT=${3:-3}
OUTPUT_DIR="benchmarks/$(date +%Y-%m-%d_%H-%M-%S)"

mkdir -p "$OUTPUT_DIR"

# Функция очистки
clean_temp() {
    rm -rf /tmp/scoriadb-* 2>/dev/null
    rm -rf /tmp/TestVLogRecoveryAfterCrash* 2>/dev/null
    rm -rf /tmp/scoriadb-latency-bench-* 2>/dev/null
    rm -rf /tmp/scoria-* 2>/dev/null
}

# Функция запуска одного бенчмарка
run_bench() {
    local BENCH=$1
    local LABEL=$2
    local COLOR=$3
    
    echo ""
    echo -e "${BOLD}${COLOR}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}${COLOR}  📊 $LABEL${NC}"
    echo -e "${BOLD}${COLOR}  🔬 $BENCH${NC}"
    echo -e "${BOLD}${COLOR}  ⏱️  $BENCHTIME  |  🧵 $CPU  |  🔄 $COUNT${NC}"
    echo -e "${BOLD}${COLOR}═══════════════════════════════════════════════════════════${NC}"
    echo ""
    
    # Очистка перед запуском
    clean_temp
    
    # Запуск бенчмарка
    local START_TIME=$(date +%s)
    go test -bench="$BENCH" -benchtime="$BENCHTIME" -cpu="$CPU" -count="$COUNT" ./internal/engine 2>&1 | tee "$OUTPUT_DIR/${BENCH}.log"
    local EXIT_CODE=$?
    local END_TIME=$(date +%s)
    local DURATION=$((END_TIME - START_TIME))
    
    # Очистка после запуска
    clean_temp
    
    # Проверка результата
    if [ $EXIT_CODE -eq 0 ]; then
        echo ""
        echo -e "${GREEN}✅ $LABEL — PASSED (${DURATION}s)${NC}"
        
        # Извлечение перцентилей
        echo ""
        echo -e "${YELLOW}📈 Результаты:${NC}"
        grep -E "(Throughput|p50|p95|p99|p999)" "$OUTPUT_DIR/${BENCH}.log" 2>/dev/null | head -30 | while read line; do
            if [[ $line == *"Throughput"* ]]; then
                echo -e "  ${CYAN}$line${NC}"
            elif [[ $line == *"p50"* ]]; then
                echo -e "  ${GREEN}$line${NC}"
            elif [[ $line == *"p95"* ]]; then
                echo -e "  ${YELLOW}$line${NC}"
            elif [[ $line == *"p99"* ]]; then
                echo -e "  ${MAGENTA}$line${NC}"
            elif [[ $line == *"p999"* ]]; then
                echo -e "  ${RED}$line${NC}"
            else
                echo -e "  $line"
            fi
        done
    else
        echo ""
        echo -e "${RED}❌ $LABEL — FAILED (${DURATION}s)${NC}"
    fi
    
    return $EXIT_CODE
}

# ============================================================
# Заголовок
# ============================================================

echo -e "${BOLD}${WHITE}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${WHITE}║  🚀 SCORIADB — BENCHMARK SUITE                            ║${NC}"
echo -e "${BOLD}${WHITE}║  📅 $(date)                            ║${NC}"
echo -e "${BOLD}${WHITE}║  ⏱️  $BENCHTIME  |  🧵 $CPU  |  🔄 $COUNT                  ║${NC}"
echo -e "${BOLD}${WHITE}╚══════════════════════════════════════════════════════════════╝${NC}"

# ============================================================
# 1. Синхронная запись
# ============================================================

run_bench "BenchmarkPutLatency_Sync" "🔴 SYNC WRITE (no group commit)" "$RED"

# ============================================================
# 2. Групповой коммит (10ms)
# ============================================================

run_bench "BenchmarkPutLatency_GroupCommit" "🟡 GROUP COMMIT (10ms)" "$YELLOW"

# ============================================================
# 3. Групповой коммит (1ms)
# ============================================================

run_bench "BenchmarkPutLatency_GroupCommit_ShortInterval" "🟢 GROUP COMMIT (1ms)" "$GREEN"

# ============================================================
# 4. Групповой коммит + разные ключи
# ============================================================

run_bench "BenchmarkPutLatency_GroupCommit_Varied" "🔵 GROUP COMMIT + VARIED KEYS" "$BLUE"

# ============================================================
# 5. Групповой коммит + 4KB значения
# ============================================================

run_bench "BenchmarkPutLatency_GroupCommit_4KB" "🟣 GROUP COMMIT + 4KB VALUES" "$MAGENTA"

# ============================================================
# 6. Чтение из MemTable
# ============================================================

run_bench "BenchmarkGetLatency" "📖 GET (MemTable hit)" "$CYAN"

# ============================================================
# 7. Чтение отсутствующего ключа
# ============================================================

run_bench "BenchmarkGetLatency_Missing" "📖 GET (Missing key)" "$WHITE"

# ============================================================
# 8. Сканирование
# ============================================================

run_bench "BenchmarkScanLatency" "📋 SCAN (10k keys)" "$MAGENTA"

# ============================================================
# 9. Смешанная нагрузка
# ============================================================

run_bench "BenchmarkMixedWorkload" "🔄 MIXED (80% Read / 20% Write)" "$CYAN"

# ============================================================
# Итог
# ============================================================

echo ""
echo -e "${BOLD}${WHITE}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}${WHITE}║  ✅ ВСЕ БЕНЧМАРКИ ЗАВЕРШЕНЫ                              ║${NC}"
echo -e "${BOLD}${WHITE}║  📁 Результаты: $OUTPUT_DIR${NC}"
echo -e "${BOLD}${WHITE}╚══════════════════════════════════════════════════════════════╝${NC}"

echo ""
echo -e "${YELLOW}📊 Сводка:${NC}"
echo ""

for BENCH in "BenchmarkPutLatency_Sync" "BenchmarkPutLatency_GroupCommit" "BenchmarkPutLatency_GroupCommit_ShortInterval" "BenchmarkPutLatency_GroupCommit_Varied" "BenchmarkPutLatency_GroupCommit_4KB" "BenchmarkGetLatency" "BenchmarkGetLatency_Missing" "BenchmarkScanLatency" "BenchmarkMixedWorkload"; do
    if [ -f "$OUTPUT_DIR/${BENCH}.log" ]; then
        echo -n "  "
        if grep -q "PASS" "$OUTPUT_DIR/${BENCH}.log" 2>/dev/null; then
            echo -n "${GREEN}✅${NC} "
        else
            echo -n "${RED}❌${NC} "
        fi
        echo -n "$BENCH: "
        
        # Извлекаем черезput
        TP=$(grep "Throughput" "$OUTPUT_DIR/${BENCH}.log" 2>/dev/null | head -1 | awk -F': ' '{print $2}' | awk '{print $1}')
        if [ -n "$TP" ]; then
            echo -n "${CYAN}$TP ops/s${NC}"
        fi
        
        # Извлекаем p99
        P99=$(grep "p99:" "$OUTPUT_DIR/${BENCH}.log" 2>/dev/null | head -1 | awk -F': ' '{print $2}')
        if [ -n "$P99" ]; then
            echo -n "  |  p99: ${MAGENTA}$P99${NC}"
        fi
        
        echo ""
    fi
done

echo ""
echo -e "${GREEN}🎉 Готово!${NC}"