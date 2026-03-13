#!/bin/bash

# 1. 定义前缀和镜像映射 (格式: 镜像名:构建目标)
REGISTRY="ghcr.io/frozenf1sh"
APPS=("cloud-media-worker:worker" "cloud-media-api-server:api-server")

for ITEM in "${APPS[@]}"; do
  # 拆分变量
  BASE_NAME="${ITEM%%:*}"
  TARGET="${ITEM#*:}"

  # 拼接完整镜像名
  FULL_IMAGE_NAME="$REGISTRY/$BASE_NAME"

  echo "🏗️  正在构建: $FULL_IMAGE_NAME (Target: $TARGET)"

  # 2. 构建镜像 (直接打上 ghcr.io 前缀的标签)
  docker build --target "$TARGET" -t "$FULL_IMAGE_NAME" .

  # echo "📦 正在同步到 K3s..."

  # 3. 通过管道直接导入到 K3s，不产生中间文件
  # - 指定 -n k8s.io 确保镜像进入 K3s 默认命名空间
  # docker save "$FULL_IMAGE_NAME" | sudo k3s ctr images import -

  echo "✅ $BASE_NAME 已就绪"
  echo "-----------------------------------"
done

# echo "🚀 所有镜像已成功导入 K3s！"
