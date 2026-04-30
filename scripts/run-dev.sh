#!/bin/bash
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"
cd "$PROJECT_ROOT"

if [ ! -f ".env" ]; then
    echo "[ERROR] .env 文件不存在，请先创建"
    exit 1
fi

set -a
source .env
set +a

go env -w GOPROXY=https://goproxy.cn,direct

export CGO_CFLAGS="-Wno-deprecated-declarations -Wno-gnu-folding-constant"
export CGO_LDFLAGS="-Wl,-no_warn_duplicate_libraries"

LDFLAGS="$(./scripts/get_version.sh ldflags) -X 'google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=warn'"

echo "[INFO] 启动后端，监听 :${APP_PORT:-8080}"
go run -ldflags="$LDFLAGS" ./cmd/server
