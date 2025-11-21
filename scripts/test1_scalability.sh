#!/bin/bash

# Teste 1: Escalabilidade
# Testa 5 → 20 → 50 → 100 sensores
# Métricas: mensagens/seg, latência média

NATS_URL="nats://localhost:4222"
TEST_DURATION=30  # segundos por teste
SENSOR_COUNTS=(5 20 50 100)

echo "=== TESTE 1: ESCALABILIDADE ==="
echo ""

# Iniciar Cloud Processor em background
echo "Iniciando Cloud Processor..."
./bin/cloud -nats "$NATS_URL" > logs/cloud.log 2>&1 &
CLOUD_PID=$!
sleep 2

# Iniciar Edge Node em background
echo "Iniciando Edge Node..."
./bin/edge -nats "$NATS_URL" -jetstream=false > logs/edge.log 2>&1 &
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

echo "Iniciando testes de escalabilidade..."
echo ""

for count in "${SENSOR_COUNTS[@]}"; do
    echo "--- Testando com $count sensores ---"
    
    # Limpar logs anteriores
    > logs/sensors.log
    
    # Iniciar sensores
    echo "Iniciando $count sensores..."
    for i in $(seq 1 $count); do
        ./bin/sensor -nats "$NATS_URL" -interval 1s -base 50.0 -noise 5.0 >> logs/sensors.log 2>&1 &
    done
    
    sleep 2  # Aguardar inicialização
    
    # Esperar duração do teste
    echo "Aguardando ${TEST_DURATION}s..."
    sleep $TEST_DURATION
    
    # Coletar métricas do cloud
    echo "Coletando métricas..."
    
    # Contar mensagens publicadas
    MSG_COUNT=$(grep -c "Published:" logs/sensors.log 2>/dev/null || echo "0")
    MSG_PER_SEC=$(echo "scale=2; $MSG_COUNT / $TEST_DURATION" | bc)
    
    echo "Resultados para $count sensores:"
    echo "  Total de mensagens: $MSG_COUNT"
    echo "  Mensagens/segundo: $MSG_PER_SEC"
    
    # Extrair latência do log do cloud
    if [ -f logs/cloud.log ]; then
        LATENCY=$(grep "Latency - Avg:" logs/cloud.log | tail -1 | grep -oP "Avg: \K[^,]+" || echo "N/A")
        echo "  Latência média: $LATENCY"
    fi
    
    echo ""
    
    # Parar sensores
    pkill -f "sensor"
    sleep 2
done

echo "=== TESTE 1 CONCLUÍDO ==="

