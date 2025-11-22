#!/bin/bash

# Script de demonstração rápida do sistema

NATS_URL="nats://localhost:4222"

echo "=== Demonstração do Sistema Distribuído ==="
echo ""

# Verificar se NATS está rodando
if ! nc -z localhost 4222 2>/dev/null; then
    echo "ERRO: NATS Server não está rodando na porta 4222"
    echo "Por favor, inicie o NATS Server primeiro:"
    echo "  nats-server"
    echo "  ou"
    echo "  docker start nats-server"
    exit 1
fi

echo "✓ NATS Server está rodando"
echo ""

# Criar diretório de logs
mkdir -p logs

# Limpar processos anteriores
pkill -f "sensor\|edge\|cloud\|dashboard" 2>/dev/null || true
sleep 1

# Iniciar Dashboard Web
echo "Iniciando Dashboard Web..."
./bin/dashboard -nats "$NATS_URL" -port 8080 > logs/demo_dashboard.log 2>&1 &
DASHBOARD_PID=$!
sleep 2
echo "✓ Dashboard Web iniciado (PID: $DASHBOARD_PID)"
echo "  📊 Acesse: http://localhost:8080"
echo ""

# Iniciar Cloud Processor
echo "Iniciando Cloud Processor..."
./bin/cloud -nats "$NATS_URL" -stats 5s > logs/demo_cloud.log 2>&1 &
CLOUD_PID=$!
sleep 2
echo "✓ Cloud Processor iniciado (PID: $CLOUD_PID)"
echo ""

# Iniciar Edge Node
echo "Iniciando Edge Node..."
./bin/edge -nats "$NATS_URL" -jetstream=false > logs/demo_edge.log 2>&1 &
EDGE_PID=$!
sleep 2
echo "✓ Edge Node iniciado (PID: $EDGE_PID)"
echo ""

# Iniciar alguns sensores
echo "Iniciando 3 sensores..."
for i in {1..3}; do
    ./bin/sensor -nats "$NATS_URL" -interval 1s -base 50.0 -noise 5.0 > logs/demo_sensor_$i.log 2>&1 &
    echo "  ✓ Sensor $i iniciado"
done
sleep 2
echo ""

# Função de limpeza
cleanup() {
    echo ""
    echo "Encerrando demonstração..."
    kill $CLOUD_PID $EDGE_PID $DASHBOARD_PID 2>/dev/null
    pkill -f "sensor" 2>/dev/null
    sleep 1
    echo "✓ Processos encerrados"
    echo ""
    echo "Logs disponíveis em:"
    echo "  - logs/demo_cloud.log"
    echo "  - logs/demo_edge.log"
    echo "  - logs/demo_dashboard.log"
    echo "  - logs/demo_sensor_*.log"
}
trap cleanup EXIT

echo "=== Sistema em execução ==="
echo ""
echo "Os componentes estão rodando:"
echo "  - 📊 Dashboard Web: http://localhost:8080"
echo "  - Sensores publicando leituras"
echo "  - Edge Node filtrando e processando"
echo "  - Cloud Processor agregando métricas"
echo ""
echo "💡 DICA: Abra http://localhost:8080 no seu navegador para ver o dashboard em tempo real!"
echo ""
echo "Pressione Ctrl+C para parar a demonstração"
echo ""
echo "Aguardando 30 segundos para coleta de dados..."
sleep 30

echo ""
echo "=== Estatísticas Finais ==="
echo ""
echo "Últimas estatísticas do Cloud Processor:"
tail -20 logs/demo_cloud.log | grep -A 10 "GLOBAL STATISTICS" || echo "Aguardando mais dados..."
echo ""

