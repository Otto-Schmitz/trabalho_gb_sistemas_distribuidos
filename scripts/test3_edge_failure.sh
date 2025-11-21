#!/bin/bash

# Teste 3: Teste Edge Node caindo
# Testa com e sem JetStream

NATS_URL="nats://localhost:4222"
TEST_DURATION=30
NUM_SENSORS=5

echo "=== TESTE 3: FALHA DE EDGE NODE ==="
echo ""

# Criar diretório de logs
mkdir -p logs

# Iniciar Cloud Processor
echo "Iniciando Cloud Processor..."
./bin/cloud -nats "$NATS_URL" > logs/cloud_failure.log 2>&1 &
CLOUD_PID=$!
sleep 2

# Teste 3.1: SEM JetStream (mensagens se perdem)
echo "--- TESTE 3.1: Edge Node sem JetStream ---"
echo ""

echo "Iniciando Edge Node SEM JetStream..."
./bin/edge -nats "$NATS_URL" -jetstream=false -id edge-nojs > logs/edge_nojs.log 2>&1 &
EDGE_PID=$!
sleep 2

echo "Iniciando $NUM_SENSORS sensores..."
for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 500ms -base 50.0 -noise 5.0 > logs/sensor_nojs_$i.log 2>&1 &
done

sleep 5
echo "Derrubando Edge Node..."
kill $EDGE_PID
sleep 5

echo "Sensores continuam publicando por 10s..."
sleep 10

pkill -f "sensor"
sleep 2

MSG_SENT=$(grep -c "Published:" logs/sensor_nojs_*.log 2>/dev/null | awk '{sum+=$1} END {print sum}')
MSG_RECEIVED=$(grep -c "FilteredReading\|Alert" logs/cloud_failure.log 2>/dev/null | head -1 || echo "0")

echo "Resultados (SEM JetStream):"
echo "  Mensagens enviadas: $MSG_SENT"
echo "  Mensagens recebidas no Cloud: $MSG_RECEIVED"
echo "  Mensagens perdidas: $((MSG_SENT - MSG_RECEIVED))"
echo ""

# Teste 3.2: COM JetStream (mensagens acumulam)
echo "--- TESTE 3.2: Edge Node COM JetStream ---"
echo ""

echo "Reiniciando Edge Node COM JetStream..."
./bin/edge -nats "$NATS_URL" -jetstream=true -id edge-js > logs/edge_js.log 2>&1 &
EDGE_PID=$!
sleep 3  # Dar tempo para JetStream configurar

echo "Iniciando $NUM_SENSORS sensores..."
for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 500ms -base 50.0 -noise 5.0 > logs/sensor_js_$i.log 2>&1 &
done

sleep 5
echo "Derrubando Edge Node..."
kill $EDGE_PID
sleep 5

echo "Sensores continuam publicando por 10s..."
sleep 10

pkill -f "sensor"
sleep 2

echo "Reiniciando Edge Node (deve processar mensagens acumuladas)..."
./bin/edge -nats "$NATS_URL" -jetstream=true -id edge-js > logs/edge_js_restart.log 2>&1 &
sleep 10

MSG_SENT_JS=$(grep -c "Published:" logs/sensor_js_*.log 2>/dev/null | awk '{sum+=$1} END {print sum}')
MSG_RECEIVED_JS=$(grep -c "FilteredReading\|Alert" logs/cloud_failure.log 2>/dev/null | tail -1 || echo "0")

echo "Resultados (COM JetStream):"
echo "  Mensagens enviadas: $MSG_SENT_JS"
echo "  Mensagens recebidas no Cloud após reinício: $MSG_RECEIVED_JS"
echo "  Mensagens recuperadas: $((MSG_RECEIVED_JS - MSG_RECEIVED))"
echo ""

# Função para limpar processos
cleanup() {
    echo "Limpando processos..."
    kill $CLOUD_PID 2>/dev/null
    pkill -f "edge\|sensor" 2>/dev/null
}
trap cleanup EXIT

echo "=== TESTE 3 CONCLUÍDO ==="

