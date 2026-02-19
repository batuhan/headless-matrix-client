#!/usr/bin/env bash
set -euo pipefail

mkdir -p bin
go build -o bin/gomuks-beeper-api ./cmd/server
