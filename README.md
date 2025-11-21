# Sistema Distribuído com NATS - Sensores, Edge Nodes e Cloud Processor

Sistema distribuído em Go usando NATS para comunicação entre sensores, nós de edge e processador de nuvem.

## 📋 Visão Geral

O sistema é composto por 3 camadas principais:

1. **Sensores (Producers)**: Simulam dispositivos embarcados que publicam leituras em `sensors.readings`
2. **Edge Nodes (Processadores Locais)**: Filtram ruído, detectam limites locais, fazem agregação parcial e reduzem tráfego para a nuvem. Publicam em `edge.filtered` e `edge.alerts`
3. **Cloud Processor (Nuvem)**: Agrega tudo, calcula métricas globais, armazena/analisa e emite alertas globais. Assina tudo de `edge.*`

## 🏗️ Arquitetura

```
+-----------+        +-----------------+        +------------------+
|  Sensor   | -----> |    Edge Node    | -----> |   Cloud Server   |
| (N nós)   |        | (M processadores)|       | (1 serviço)       |
+-----------+        +-----------------+        +------------------+
       \                   /                              |
        \  Pub/Sub via    /     Pub/Sub via NATS          |
         ------ NATS BROKER -------------------------------
```

## 🚀 Pré-requisitos

- Go 1.21 ou superior
- NATS Server instalado e rodando

### Instalando NATS Server

```bash
# Linux/macOS
curl -L https://github.com/nats-io/nats-server/releases/download/v2.10.7/nats-server-v2.10.7-linux-amd64.zip -o nats-server.zip
unzip nats-server.zip
sudo mv nats-server-v2.10.7-linux-amd64/nats-server /usr/local/bin/

# Ou via Docker
docker run -d --name nats-server -p 4222:4222 -p 8222:8222 nats:latest
```

## 📦 Instalação

1. Clone o repositório:
```bash
git clone <repo-url>
cd sistemas_distribuidos_gb
```

2. Instale as dependências:
```bash
make install-deps
```

3. Compile os componentes:
```bash
make build
```

Isso criará os binários em `bin/`:
- `bin/sensor` - Producer de sensores
- `bin/edge` - Edge Node processor
- `bin/cloud` - Cloud Processor

## 🔧 Uso

### Iniciar NATS Server

```bash
# Terminal 1
nats-server
# Ou se estiver usando Docker
docker start nats-server
```

### Executar Componentes Manualmente

**Cloud Processor** (Terminal 1):
```bash
./bin/cloud -nats nats://localhost:4222
```

**Edge Node** (Terminal 2):
```bash
./bin/edge -nats nats://localhost:4222
# Com JetStream para persistência:
./bin/edge -nats nats://localhost:4222 -jetstream=true
```

**Sensor** (Terminal 3):
```bash
./bin/sensor -nats nats://localhost:4222
# Com opções personalizadas:
./bin/sensor -nats nats://localhost:4222 -interval 1s -base 50.0 -noise 5.0 -anomaly 0.1
```

### Opções de Linha de Comando

#### Sensor
- `-id`: ID do sensor (auto-gerado se não fornecido)
- `-nats`: URL do servidor NATS (padrão: `nats://localhost:4222`)
- `-interval`: Intervalo de publicação (padrão: `1s`)
- `-base`: Valor base para leituras (padrão: `50.0`)
- `-noise`: Nível de ruído (desvio padrão) (padrão: `5.0`)
- `-anomaly`: Probabilidade de anomalia 0-1 (padrão: `0.0`)

#### Edge Node
- `-id`: ID do edge node (auto-gerado se não fornecido)
- `-nats`: URL do servidor NATS (padrão: `nats://localhost:4222`)
- `-min`: Limite mínimo para alertas (padrão: `0.0`)
- `-max`: Limite máximo para alertas (padrão: `200.0`)
- `-noise`: Limite de filtro de ruído (desvios padrão) (padrão: `3.0`)
- `-window`: Tamanho da janela de agregação (padrão: `10`)
- `-aggregate`: Intervalo de agregação (padrão: `5s`)
- `-jetstream`: Usar JetStream para persistência (padrão: `false`)

#### Cloud Processor
- `-nats`: URL do servidor NATS (padrão: `nats://localhost:4222`)
- `-stats`: Intervalo de relatório de estatísticas (padrão: `10s`)
- `-max-readings`: Máximo de leituras a manter em memória (padrão: `10000`)

## 🧪 Testes

O projeto inclui 5 cenários de teste automatizados:

### Teste 1: Escalabilidade
Testa o sistema com 5 → 20 → 50 → 100 sensores e mede mensagens/seg e latência média.

```bash
make test1
# ou
./scripts/test1_scalability.sh
```

### Teste 2: Latência
Mede latência média, p95 e p99 do caminho Sensor → Edge → Cloud.

```bash
make test2
# ou
./scripts/test2_latency.sh
```

### Teste 3: Falha de Edge Node
Testa o comportamento com e sem JetStream quando um edge node cai.

```bash
make test3
# ou
./scripts/test3_edge_failure.sh
```

### Teste 4: Filtragem de Ruído
Simula valores absurdos para testar filtro de ruído e detecção de anomalias.

```bash
make test4
# ou
./scripts/test4_noise_filtering.sh
```

### Teste 5: Consumo de Recursos
Compara consumo de CPU/Memória com sensores publicando 1 msg/s vs 100 msg/s.

```bash
make test5
# ou
./scripts/test5_resource_usage.sh
```

### Executar Todos os Testes

```bash
make all-tests
```

## 📊 Formato das Mensagens

### Sensor Reading (`sensors.readings`)
```json
{
  "sensor_id": "sensor-07",
  "value": 73.2,
  "timestamp": 1732213000
}
```

### Filtered Reading (`edge.filtered`)
```json
{
  "sensor_id": "sensor-07",
  "value": 73.2,
  "timestamp": 1732213000,
  "edge_id": "edge-20240101-120000"
}
```

### Alert (`edge.alerts`)
```json
{
  "sensor_id": "sensor-07",
  "value": 150.5,
  "timestamp": 1732213000,
  "edge_id": "edge-20240101-120000",
  "type": "threshold",
  "message": "Value above maximum threshold"
}
```

## 📁 Estrutura do Projeto

```
sistemas_distribuidos_gb/
├── cmd/
│   ├── sensor/
│   │   └── main.go          # Producer de sensores
│   ├── edge/
│   │   └── main.go          # Edge Node processor
│   └── cloud/
│       └── main.go          # Cloud Processor
├── scripts/
│   ├── test1_scalability.sh
│   ├── test2_latency.sh
│   ├── test3_edge_failure.sh
│   ├── test4_noise_filtering.sh
│   └── test5_resource_usage.sh
├── bin/                      # Binários compilados
├── logs/                     # Logs dos testes
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 🔍 Monitoramento

O Cloud Processor reporta estatísticas globais periodicamente:
- Total de leituras processadas
- Taxa de mensagens/segundo
- Média, desvio padrão, min/max dos valores
- Latência média, p95, p99
- Número de edge nodes ativos
- Total de alertas recebidos

## 🐛 Troubleshooting

### NATS não conecta
Verifique se o servidor NATS está rodando:
```bash
nats-server -p 4222
# ou
docker ps | grep nats
```

### Permissão negada nos scripts
```bash
chmod +x scripts/*.sh
```

### Erros de compilação
```bash
go mod tidy
make clean
make build
```

## 📝 Notas

- O sistema usa pub/sub NATS padrão por padrão
- JetStream pode ser habilitado no Edge Node para persistência de mensagens
- Sensores podem simular anomalias para testar filtros
- Edge Nodes fazem filtragem de ruído baseada em desvio padrão
- Cloud Processor mantém estatísticas em memória (limitado por `-max-readings`)

## 📄 Licença

Este projeto é parte de um trabalho acadêmico sobre sistemas distribuídos.

