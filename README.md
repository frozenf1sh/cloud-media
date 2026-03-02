# cloud-media

云原生媒体高并发处理平台

## 技术栈

- **RPC 框架**: [Connect RPC](https://connectrpc.com/)
- **IDL**: Protobuf + [Buf v2](https://buf.build/)
- **依赖注入**: [Google Wire](https://github.com/google/wire)
- **消息队列**: RabbitMQ
- **对象存储**: MinIO
- **数据库**: PostgreSQL
- **容器化**: Docker Compose

## 项目架构

采用标准的**整洁架构 (Clean Architecture)** + **golang-standards/project-layout** 布局。

### 目录结构

```
cloud-media/
├── cmd/                          # 应用入口
│   └── api-server/              # API 服务器
│       ├── main.go
│       ├── wire.go              # Wire 配置
│       ├── wire_gen.go          # Wire 生成 (不要编辑)
│       └── server.go
├── internal/                     # 私有应用代码
│   ├── domain/                  # 领域层 (Enterprise Business Rules)
│   │   └── video.go             # 实体和接口定义
│   ├── usecase/                 # 用例层 (Application Business Rules)
│   │   └── video_usecase.go     # 业务逻辑
│   ├── adapter/                 # 适配器层 (Interface Adapters)
│   │   ├── rpc/                 # RPC 适配器
│   │   ├── grpc/                # (预留) gRPC 适配器
│   │   └── rest/                # (预留) REST 适配器
│   └── infrastructure/          # 基础设施层 (Frameworks & Drivers)
│       ├── broker/              # 消息队列
│       ├── persistence/         # (预留) 数据库持久化
│       └── cache/               # (预留) 缓存
├── proto/                        # Protobuf 定义
│   ├── api/v1/
│   │   └── video.proto
│   ├── gen/                     # 生成的代码 (不要编辑)
│   ├── buf.yaml
│   └── buf.gen.yaml
├── scripts/
│   └── bufgen.sh                # Buf 代码生成脚本
├── docker-compose.yml            # 本地开发环境
├── go.mod
└── go.sum
```

### 架构图

```
┌─────────────────────────────────────────────────────────────┐
│  适配器层 (Adapter)         ◄──  RPC/REST/GRPC              │
│                               ────  转换数据格式              │
├─────────────────────────────────────────────────────────────┤
│  用例层 (Use Case)           ◄──  业务逻辑                   │
│                               ────  编排领域对象              │
├─────────────────────────────────────────────────────────────┤
│  领域层 (Domain)             ◄──  实体 & 接口                │
│                               ────  核心业务规则              │
├─────────────────────────────────────────────────────────────┤
│  基础设施层 (Infrastructure) ◄──  RabbitMQ/DB/Cache         │
│                               ────  实现技术细节              │
└─────────────────────────────────────────────────────────────┘
```

## 快速开始

### 前置要求

- Go 1.25+
- Docker & Docker Compose
- Wire CLI (`go install github.com/google/wire/cmd/wire@latest`)
- Buf CLI

### 1. 启动基础设施

```bash
docker-compose up -d
```

服务:
- MinIO: http://localhost:9001 (rootadmin / rootpassword)
- RabbitMQ: http://localhost:15672 (guest / guest)
- PostgreSQL: localhost:5432 (postgres / password)

### 2. 生成代码

```bash
# 生成 protobuf 代码
./scripts/bufgen.sh

# 生成依赖注入
cd cmd/api-server && wire
```

### 3. 运行服务

```bash
go run ./cmd/api-server
```

服务将在 `http://localhost:8080` 启动。

## 开发指南

### 重新生成代码

```bash
# Buf 生成 proto 代码
./scripts/bufgen.sh

# Wire 生成依赖注入
cd cmd/api-server && wire
```

### 项目布局参考

- [golang-standards/project-layout](https://github.com/golang-standards/project-layout)
- [Clean Architecture](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)

## License

[Apache License 2.0](LICENSE)
