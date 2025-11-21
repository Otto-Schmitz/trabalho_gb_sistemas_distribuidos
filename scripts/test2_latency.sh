#!/bin/bash

# Teste 2: Latência Sensor → Edge → Cloud
# Mede: média, p95, p99

NATS_URL="nats://localhost:4222"
TEST_DURATION=60  # segundos
NUM_SENSORS=10

echo "=== TESTE 2: LATÊNCIA ==="
echo ""

# Iniciar Cloud Processor em background
echo "Iniciando Cloud Processor..."
./bin/cloud -nats "$NATS_URL" -stats 5s > logs/cloud_latency.log 2>&1 &
CLOUD_PID=$!
sleep 2

# Iniciar Edge Node em background
echo "Iniciando Edge Node..."
./bin/edge -nats "$NATS_URL" -jetstream=false > logs/edge_latency.log 2>&1 &
EDGE_PID=$!
sleep 2

# Função para limpar processos
cleanup() {
    echo "Limpando processos..."
    kill $CLOUD_PID $EDGE_PID 2>/dev/null
    pkill -f "sensor" 2>/dev/null
}
trap cleanup EXIT

# Criar diretório de logs
mkdir -p logs

echo "Iniciando $NUM_SENSORS sensores por ${TEST_DURATION}s..."
echo ""

# Iniciar sensores
for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 1s -base 50.0 -noise 5.0 > logs/sensor_$i.log 2>&1 &
done

sleep 2

# Esperar duração do teste
echo "Coletando dados de latência por ${TEST_DURATION}s..."
sleep $TEST_DURATION

echo ""
echo "Resultados de Latência:"
echo ""

# Extrair métricas do log do cloud
if [ -f logs/cloud_latency.log ]; then
    echo "--- Últimas Estatísticas do Cloud ---"
    tail -20 logs/cloud_latency.log | grep -A 15 "GLOBAL STATISTICS" || echo "Métricas não encontradas"
else
    echo "Log do cloud não encontrado"
fi

echo ""
echo "=== TESTE 2 CONCLUÍDO ==="

