#!/bin/bash

# Teste 5: Consumo de CPU/Memory
# Compara: sensores publicando 1 msg/s vs 100 msg/s

NATS_URL="nats://localhost:4222"
TEST_DURATION=30
NUM_SENSORS=10

echo "=== TESTE 5: CONSUMO DE RECURSOS ==="
echo ""

# Criar diretório de logs
mkdir -p logs

# Iniciar Cloud Processor
echo "Iniciando Cloud Processor..."
./bin/cloud -nats "$NATS_URL" > logs/cloud_resources.log 2>&1 &
CLOUD_PID=$!
sleep 2

# Iniciar Edge Node
echo "Iniciando Edge Node..."
./bin/edge -nats "$NATS_URL" -jetstream=false > logs/edge_resources.log 2>&1 &
EDGE_PID=$!
sleep 2

# Função para limpar processos
cleanup() {
    echo "Limpando processos..."
    kill $CLOUD_PID $EDGE_PID 2>/dev/null
    pkill -f "sensor" 2>/dev/null
}
trap cleanup EXIT

# Teste 5.1: 1 msg/s
echo "--- TESTE 5.1: Sensores publicando 1 msg/s ---"
echo ""

for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 1s -base 50.0 -noise 5.0 > logs/sensor_1s_$i.log 2>&1 &
done

echo "Monitorando recursos por ${TEST_DURATION}s..."
echo ""

# Monitorar recursos
(
    echo "Timestamp,Process,CPU%,Memory%" > logs/resources_1s.csv
    while true; do
        TIMESTAMP=$(date +%s)
        
        # Cloud Processor
        CLOUD_CPU=$(ps -p $CLOUD_PID -o %cpu --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        CLOUD_MEM=$(ps -p $CLOUD_PID -o %mem --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        echo "$TIMESTAMP,Cloud,$CLOUD_CPU,$CLOUD_MEM" >> logs/resources_1s.csv
        
        # Edge Node
        EDGE_CPU=$(ps -p $EDGE_PID -o %cpu --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        EDGE_MEM=$(ps -p $EDGE_PID -o %mem --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        echo "$TIMESTAMP,Edge,$EDGE_CPU,$EDGE_MEM" >> logs/resources_1s.csv
        
        sleep 1
    done
) &
MONITOR_PID=$!

sleep $TEST_DURATION
kill $MONITOR_PID 2>/dev/null
pkill -f "sensor"
sleep 2

# Calcular médias
if [ -f logs/resources_1s.csv ]; then
    CLOUD_CPU_AVG=$(awk -F',' '/Cloud/ {sum+=$3; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_1s.csv)
    CLOUD_MEM_AVG=$(awk -F',' '/Cloud/ {sum+=$4; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_1s.csv)
    EDGE_CPU_AVG=$(awk -F',' '/Edge/ {sum+=$3; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_1s.csv)
    EDGE_MEM_AVG=$(awk -F',' '/Edge/ {sum+=$4; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_1s.csv)
    
    echo "Resultados (1 msg/s):"
    echo "  Cloud Processor - CPU médio: ${CLOUD_CPU_AVG}%, Memória média: ${CLOUD_MEM_AVG}%"
    echo "  Edge Node - CPU médio: ${EDGE_CPU_AVG}%, Memória média: ${EDGE_MEM_AVG}%"
    echo ""
fi

# Teste 5.2: 100 msg/s (10ms intervalo)
echo "--- TESTE 5.2: Sensores publicando 100 msg/s (10ms) ---"
echo ""

> logs/edge_resources.log
> logs/cloud_resources.log

for i in $(seq 1 $NUM_SENSORS); do
    ./bin/sensor -nats "$NATS_URL" -interval 10ms -base 50.0 -noise 5.0 > logs/sensor_100s_$i.log 2>&1 &
done

echo "Monitorando recursos por ${TEST_DURATION}s..."
echo ""

# Monitorar recursos
(
    echo "Timestamp,Process,CPU%,Memory%" > logs/resources_100s.csv
    while true; do
        TIMESTAMP=$(date +%s)
        
        # Cloud Processor
        CLOUD_CPU=$(ps -p $CLOUD_PID -o %cpu --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        CLOUD_MEM=$(ps -p $CLOUD_PID -o %mem --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        echo "$TIMESTAMP,Cloud,$CLOUD_CPU,$CLOUD_MEM" >> logs/resources_100s.csv
        
        # Edge Node
        EDGE_CPU=$(ps -p $EDGE_PID -o %cpu --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        EDGE_MEM=$(ps -p $EDGE_PID -o %mem --no-headers 2>/dev/null | tr -d ' ' || echo "0")
        echo "$TIMESTAMP,Edge,$EDGE_CPU,$EDGE_MEM" >> logs/resources_100s.csv
        
        sleep 1
    done
) &
MONITOR_PID=$!

sleep $TEST_DURATION
kill $MONITOR_PID 2>/dev/null
pkill -f "sensor"
sleep 2

# Calcular médias
if [ -f logs/resources_100s.csv ]; then
    CLOUD_CPU_AVG=$(awk -F',' '/Cloud/ {sum+=$3; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_100s.csv)
    CLOUD_MEM_AVG=$(awk -F',' '/Cloud/ {sum+=$4; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_100s.csv)
    EDGE_CPU_AVG=$(awk -F',' '/Edge/ {sum+=$3; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_100s.csv)
    EDGE_MEM_AVG=$(awk -F',' '/Edge/ {sum+=$4; count++} END {if(count>0) print sum/count; else print 0}' logs/resources_100s.csv)
    
    echo "Resultados (100 msg/s):"
    echo "  Cloud Processor - CPU médio: ${CLOUD_CPU_AVG}%, Memória média: ${CLOUD_MEM_AVG}%"
    echo "  Edge Node - CPU médio: ${EDGE_CPU_AVG}%, Memória média: ${EDGE_MEM_AVG}%"
    echo ""
fi

echo "=== TESTE 5 CONCLUÍDO ==="
echo "Dados detalhados salvos em logs/resources_*.csv"

