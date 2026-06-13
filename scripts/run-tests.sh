#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go mod tidy
go test ./... -count=1
go test ./... -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out
