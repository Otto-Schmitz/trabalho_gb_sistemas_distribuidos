#!/bin/bash

# Teste 4: Ruído vs Filtragem
# Simula valores absurdos para testar filtro de ruído e detecção de anomalia

NATS_URL="nats://localhost:4222"
TEST_DURATION=20
NUM_SENSORS=5

echo "=== TESTE 4: FILTRAGEM DE RUÍDO ==="
echo ""

# Criar diretório de logs
mkdir -p logs

# Iniciar Cloud Processor
echo "Iniciando Cloud Processor..."
./bin/cloud -nats "$NATS_URL" > logs/cloud_noise.log 2>&1 &
CLOUD_PID=$!
sleep 2

# Iniciar Edge Node com filtro de ruído
echo "Iniciando Edge Node com filtro de ruído (noise=3.0)..."
./bin/edge -nats "$NATS_URL" -jetstream=false -noise 3.0 -min 0.0 -max 200.0 > logs/edge_noise.log 2>&1 &
EDGE_PID=$!
sleep 2

# Função para limpar processos
cleanup() {
    echo "Limpando processos..."
    kill $CLOUD_PID $EDGE_PID 2>/dev/null
    pkill -f "sensor" 2>/dev/null
}
trap cleanup EXIT

# Teste 4.1: Sensores normais
echo "--- TESTE 4.1: Sensores normais (sem anomalias) ---"
echo ""

for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 1s -base 50.0 -noise 5.0 -anomaly 0.0 > logs/sensor_normal_$i.log 2>&1 &
done

sleep $TEST_DURATION
pkill -f "sensor"
sleep 2

FILTERED_NORMAL=$(grep -c "FilteredReading" logs/cloud_noise.log 2>/dev/null || echo "0")
FILTERED_OUT_NORMAL=$(grep -c "Filtered out noise" logs/edge_noise.log 2>/dev/null || echo "0")

echo "Resultados (sensores normais):"
echo "  Leituras filtradas e enviadas: $FILTERED_NORMAL"
echo "  Leituras rejeitadas como ruído: $FILTERED_OUT_NORMAL"
echo ""

# Limpar logs anteriores
> logs/edge_noise.log
> logs/cloud_noise.log

# Teste 4.2: Sensores com anomalias
echo "--- TESTE 4.2: Sensores com anomalias (anomaly=0.3) ---"
echo ""

for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 1s -base 50.0 -noise 5.0 -anomaly 0.3 > logs/sensor_anomaly_$i.log 2>&1 &
done

sleep $TEST_DURATION
pkill -f "sensor"
sleep 2

FILTERED_ANOMALY=$(grep -c "FilteredReading" logs/cloud_noise.log 2>/dev/null || echo "0")
FILTERED_OUT_ANOMALY=$(grep -c "Filtered out noise" logs/edge_noise.log 2>/dev/null || echo "0")
ALERTS=$(grep -c "Alert published" logs/edge_noise.log 2>/dev/null || echo "0")

echo "Resultados (sensores com anomalias):"
echo "  Leituras filtradas e enviadas: $FILTERED_ANOMALY"
echo "  Leituras rejeitadas como ruído: $FILTERED_OUT_ANOMALY"
echo "  Alertas gerados: $ALERTS"
echo ""

# Teste 4.3: Sensores com valores extremos
echo "--- TESTE 4.3: Sensores com valores extremos (base=500, noise=100) ---"
echo ""

> logs/edge_noise.log
> logs/cloud_noise.log

for i in $(seq 1 3); do
    ./bin/sensor -nats "$NATS_URL" -interval 1s -base 500.0 -noise 100.0 -anomaly 0.0 > logs/sensor_extreme_$i.log 2>&1 &
done

sleep $TEST_DURATION
pkill -f "sensor"
sleep 2

FILTERED_EXTREME=$(grep -c "FilteredReading" logs/cloud_noise.log 2>/dev/null || echo "0")
FILTERED_OUT_EXTREME=$(grep -c "Filtered out noise" logs/edge_noise.log 2>/dev/null || echo "0")
ALERTS_EXTREME=$(grep -c "Alert published" logs/edge_noise.log 2>/dev/null || echo "0")

echo "Resultados (valores extremos):"
echo "  Leituras filtradas e enviadas: $FILTERED_EXTREME"
echo "  Leituras rejeitadas como ruído: $FILTERED_OUT_EXTREME"
echo "  Alertas gerados (threshold): $ALERTS_EXTREME"
echo ""

echo "=== TESTE 4 CONCLUÍDO ==="

