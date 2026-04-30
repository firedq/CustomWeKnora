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

echo "[INFO] 启动前端，监听 :5173"
cd frontend
npm run dev
