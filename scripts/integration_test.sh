#!/bin/bash
# Cloud Media 集成测试脚本
# 需要先启动 docker compose 服务

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "========================================="
echo "Cloud Media 集成测试"
echo "========================================="

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查 Docker Compose 服务是否运行
check_services() {
    log_info "检查服务状态..."

    if ! docker compose ps > /dev/null 2>&1; then
        log_error "Docker Compose 未运行，请先执行: docker compose up -d"
        exit 1
    fi

    # 检查服务健康状态
    local services=("postgres" "rabbitmq" "minio")
    for service in "${services[@]}"; do
        if ! docker compose ps --format json "$service" | grep -q '"Health":"healthy"'; then
            log_warn "服务 $service 可能未就绪，继续尝试..."
        fi
    done

    log_info "服务检查完成"
}

# 编译项目
build_project() {
    log_info "编译项目..."

    if ! go build -o bin/api-server ./cmd/api-server; then
        log_error "编译 api-server 失败"
        exit 1
    fi

    if ! go build -o bin/worker ./cmd/worker; then
        log_error "编译 worker 失败"
        exit 1
    fi

    log_info "编译完成"
}

# 运行单元测试
run_unit_tests() {
    log_info "运行单元测试..."

    if ! go test -v ./internal/usecase/... ./internal/infrastructure/transcoder/...; then
        log_error "单元测试失败"
        exit 1
    fi

    log_info "单元测试通过"
}

# 主流程
main() {
    log_info "开始集成测试..."

    # 检查命令行参数
    local skip_services=false
    local skip_build=false
    local skip_tests=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            --skip-services)
                skip_services=true
                shift
                ;;
            --skip-build)
                skip_build=true
                shift
                ;;
            --skip-tests)
                skip_tests=true
                shift
                ;;
            *)
                log_error "未知参数: $1"
                exit 1
                ;;
        esac
    done

    # 执行步骤
    if [ "$skip_services" = false ]; then
        check_services
    fi

    if [ "$skip_build" = false ]; then
        build_project
    fi

    if [ "$skip_tests" = false ]; then
        run_unit_tests
    fi

    echo ""
    log_info "========================================="
    log_info "集成测试准备完成！"
    log_info "========================================="
    echo ""
    log_info "下一步操作："
    log_info "1. 启动 API Server: ./bin/api-server"
    log_info "2. 启动 Worker: ./bin/worker"
    log_info "3. 使用 curl 或客户端提交测试任务"
    echo ""
}

main "$@"
