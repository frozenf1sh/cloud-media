# cloud-media

云原生媒体高并发处理平台 - 视频 HLS 切片与转码服务

## 功能特性

- 🎬 **视频 HLS 切片** - 支持多码率自适应播放
- 🖼️ **封面自动生成** - 从视频第一帧提取缩略图
- 📨 **异步任务处理** - 基于 RabbitMQ 的高并发任务队列
- 💾 **持久化存储** - PostgreSQL 任务状态追踪
- 🔌 **整洁架构** - 领域驱动设计，依赖倒置

## 技术栈

- **RPC 框架**: [Connect RPC](https://connectrpc.com/)
- **IDL**: Protobuf + [Buf v2](https://buf.build/)
- **依赖注入**: [Google Wire](https://github.com/google/wire)
- **ORM**: [GORM](https://gorm.io/)
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
│   │   └── rpc/                 # RPC 适配器
│   └── infrastructure/          # 基础设施层 (Frameworks & Drivers)
│       ├── broker/              # RabbitMQ
│       └── persistence/         # PostgreSQL + GORM
├── proto/                        # Protobuf 定义
├── doc/                          # 文档
│   ├── DATABASE_DESIGN.md       # 数据库设计文档
│   ├── DATABASE_DESIGN_V2.md    # HLS 扩展设计
│   └── TODO.md                  # 开发任务列表
├── docker-compose.yml            # 本地开发环境
└── go.mod
```

### 架构图

```
┌─────────────────────────────────────────────────────────────┐
│  适配器层 (Adapter)         ◄──  Connect RPC                │
│                               ────  转换数据格式              │
├─────────────────────────────────────────────────────────────┤
│  用例层 (Use Case)           ◄──  业务逻辑                   │
│                               ────  编排领域对象              │
├─────────────────────────────────────────────────────────────┤
│  领域层 (Domain)             ◄──  VideoTask, Repository      │
│                               ────  核心业务规则              │
├─────────────────────────────────────────────────────────────┤
│  基础设施层 (Infrastructure) ◄──  PostgreSQL/RabbitMQ        │
│                               ────  GORM 实现                  │
└─────────────────────────────────────────────────────────────┘
```

### 数据库设计

- **video_tasks** - 视频任务主表，支持 JSONB 灵活扩展 HLS 输出信息
- **task_status_logs** - 任务状态变更历史

详见 [doc/DATABASE_DESIGN_V2.md](doc/DATABASE_DESIGN_V2.md)

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

## API 接口

### SubmitTask - 提交转码任务

```bash
curl -X POST http://localhost:8080/api.v1.VideoService/SubmitTask \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "test-001",
    "source_bucket": "media-input",
    "source_key": "videos/test.mp4"
  }'
```

### GetTaskStatus - 获取任务状态

```bash
curl -X POST http://localhost:8080/api.v1.VideoService/GetTaskStatus \
  -H "Content-Type: application/json" \
  -d '{"task_id": "test-001"}'
```

### ListTasks - 列出任务

```bash
curl -X POST http://localhost:8080/api.v1.VideoService/ListTasks \
  -H "Content-Type: application/json" \
  -d '{"page": 1, "page_size": 20}'
```

### CancelTask - 取消任务

```bash
curl -X POST http://localhost:8080/api.v1.VideoService/CancelTask \
  -H "Content-Type: application/json" \
  -d '{"task_id": "test-001"}'
```

## 开发指南

### 项目状态

- ✅ Domain 层 - 完整
- ✅ Infrastructure 层 - 数据库 + MQ 完成
- ✅ API 层 - 完整
- ⏳ MinIO 集成 - 待开发
- ⏳ Worker 服务 - 待开发
- ⏳ FFmpeg 转码 - 待开发

详见 [doc/TODO.md](doc/TODO.md)

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
