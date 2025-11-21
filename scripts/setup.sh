#!/bin/bash

# Script de setup inicial do projeto

set -e

echo "=== Setup do Sistema Distribuído ==="
echo ""

# Verificar se Go está instalado
if ! command -v go &> /dev/null; then
    echo "ERRO: Go não está instalado. Por favor, instale Go 1.21 ou superior."
    exit 1
fi

echo "✓ Go encontrado: $(go version)"
echo ""

# Verificar se NATS está instalado
if ! command -v nats-server &> /dev/null; then
    echo "AVISO: NATS Server não encontrado no PATH."
    echo "Por favor, instale o NATS Server:"
    echo "  - Linux: curl -L https://github.com/nats-io/nats-server/releases/download/v2.10.7/nats-server-v2.10.7-linux-amd64.zip -o nats-server.zip"
    echo "  - Ou use Docker: docker run -d --name nats-server -p 4222:4222 -p 8222:8222 nats:latest"
    echo ""
    read -p "Deseja continuar mesmo assim? (s/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Ss]$ ]]; then
        exit 1
    fi
else
    echo "✓ NATS Server encontrado: $(nats-server -v 2>&1 | head -1)"
fi
echo ""

# Instalar dependências
echo "Instalando dependências Go..."
go mod download
go mod tidy
echo "✓ Dependências instaladas"
echo ""

# Compilar componentes
echo "Compilando componentes..."
make build
echo "✓ Componentes compilados"
echo ""

# Criar diretórios necessários
echo "Criando diretórios..."
mkdir -p logs
mkdir -p bin
echo "✓ Diretórios criados"
echo ""

# Tornar scripts executáveis
echo "Tornando scripts executáveis..."
chmod +x scripts/*.sh
echo "✓ Scripts configurados"
echo ""

echo "=== Setup concluído! ==="
echo ""
echo "Próximos passos:"
echo "1. Inicie o NATS Server: nats-server"
echo "   (ou: docker start nats-server)"
echo ""
echo "2. Execute os componentes:"
echo "   Terminal 1: ./bin/cloud"
echo "   Terminal 2: ./bin/edge"
echo "   Terminal 3: ./bin/sensor"
echo ""
echo "3. Ou execute os testes:"
echo "   make test1  # Escalabilidade"
echo "   make test2  # Latência"
echo "   make test3  # Falha de Edge Node"
echo "   make test4  # Filtragem de Ruído"
echo "   make test5  # Consumo de Recursos"
echo "   make all-tests  # Todos os testes"
echo ""

