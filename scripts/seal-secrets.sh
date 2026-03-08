#!/bin/bash

# Sealed Secrets 加密脚本
# 递归查找 k8s/ 目录下所有 secrets.unsealed.yaml 并加密

set -euo pipefail

# === 配置 ===
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PUBKEY_FILE="$PROJECT_ROOT/sealed-secrets-public-key.pem"
BACKUP_KEY_FILE="$PROJECT_ROOT/sealed-secrets-private-key.backup.yaml"

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# === 辅助函数 (输出重定向到 stderr 以避免污染 stdout) ===
log_info() { echo -e "${GREEN}[INFO]${NC} $1" >&2; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1" >&2; }
log_error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }

# === 检查依赖 ===
check_dependencies() {
  if ! command -v kubeseal &>/dev/null; then
    log_error "kubeseal 未安装。请访问: https://github.com/bitnami-labs/sealed-secrets/releases"
    exit 1
  fi

  if ! kubectl cluster-info &>/dev/null; then
    log_error "无法连接到 Kubernetes 集群，请检查 kubeconfig"
    exit 1
  fi

  # 检查 Controller 状态
  log_info "检查 Sealed Secrets Controller..."
  if ! kubectl wait --for=condition=ready pod -n kube-system -l name=sealed-secrets-controller --timeout=10s &>/dev/null; then
    log_warn "Controller 未就绪或未安装，尝试安装..."
    kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.27.1/controller.yaml
    kubectl wait --for=condition=ready pod -n kube-system -l name=sealed-secrets-controller --timeout=60s
  fi
  log_info "环境检查通过"
}

# === 获取公钥 ===
fetch_public_key() {
  log_info "正在获取集群公钥..."
  # 尝试获取公钥，如果失败则退出
  if ! kubeseal --fetch-cert >"$PUBKEY_FILE" 2>/dev/null; then
    log_error "获取公钥失败，请确保 Controller 运行正常且网络连通"
    exit 1
  fi
  log_info "公钥已保存: ${PUBKEY_FILE#$PROJECT_ROOT/}"
}

# === 加密逻辑 ===
seal_file() {
  local unsealed="$1"
  local sealed="${unsealed%.unsealed.yaml}.yaml"
  local rel_unsealed="${unsealed#$PROJECT_ROOT/}"
  local rel_sealed="${sealed#$PROJECT_ROOT/}"

  log_info "正在加密: $rel_unsealed"

  # 加密
  kubeseal --format=yaml --cert="$PUBKEY_FILE" <"$unsealed" >"$sealed"

  echo "$rel_sealed"
}

# === 备份私钥 ===
backup_private_key() {
  log_warn "正在尝试备份私钥..."
  # 使用 label 选择器查找 active 的 key，按创建时间排序取最新的
  local secret_name
  secret_name=$(kubectl get secret -n kube-system -l sealedsecrets.bitnami.com/sealed-secrets-key -o name --sort-by=.metadata.creationTimestamp | tail -1)

  if [ -z "$secret_name" ]; then
    # 回退：尝试旧的命名方式
    if kubectl get secret -n kube-system sealed-secrets-key &>/dev/null; then
      secret_name="secret/sealed-secrets-key"
    else
      log_error "未找到私钥 Secret，跳过备份。请手动检查 kube-system 命名空间。"
      return
    fi
  fi

  kubectl get "$secret_name" -n kube-system -o yaml >"$BACKUP_KEY_FILE"
  log_info "私钥已备份至: ${BACKUP_KEY_FILE#$PROJECT_ROOT/}"
  log_warn "⚠️  警告：请将私钥移至安全位置（如 1Password），切勿提交到 Git！"
}

# === 主程序 ===
main() {
  echo "========================================="
  echo "  Sealed Secrets 加密工具"
  echo "========================================="

  check_dependencies
  fetch_public_key

  # 查找文件
  local files=()
  while IFS= read -r -d '' file; do
    files+=("$file")
  done < <(find "$PROJECT_ROOT/k8s" -name "secrets.unsealed.yaml" -type f -print0 | sort -z)

  if [ ${#files[@]} -eq 0 ]; then
    log_warn "没有发现 secrets.unsealed.yaml 文件"
    exit 0
  fi

  local processed_files=()
  for file in "${files[@]}"; do
    processed_files+=("$(seal_file "$file")")
  done

  backup_private_key

  echo
  echo "========================================="
  echo "  操作完成"
  echo "========================================="
  echo "建议执行："
  echo "  git add $PUBKEY_FILE"
  for f in "${processed_files[@]}"; do
    echo "  git add $f"
  done
  echo "  git commit -m 'chore: update sealed secrets'"
}

main "$@"
