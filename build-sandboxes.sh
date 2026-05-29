#!/usr/bin/env bash
set -euo pipefail

echo "==> Building goexec-sandbox-go ..."
docker build -f Dockerfile.sandbox.go -t goexec-sandbox-go:latest .

echo "==> Building goexec-sandbox-python ..."
docker build -f Dockerfile.sandbox.python -t goexec-sandbox-python:latest .

echo "==> Building goexec-sandbox-cpp ..."
docker build -f Dockerfile.sandbox.cpp -t goexec-sandbox-cpp:latest .

echo ""
echo "All sandbox images built. Run: go run ./cmd/server"


