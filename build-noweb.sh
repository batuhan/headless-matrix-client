#!/usr/bin/env bash
set -euo pipefail

mkdir -p bin
go build -o bin/easymatrix ./cmd/server
