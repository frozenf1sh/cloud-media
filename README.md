# cloud-media

云原生媒体高并发处理平台 - 视频 HLS 切片与转码服务

## 功能特性

- 🎬 **视频 HLS 切片** - 支持多码率自适应播放
- 🖼️ **封面自动生成** - 从视频第一帧提取缩略图
- 📨 **异步任务处理** - 基于 RabbitMQ 的高并发任务队列
- 💾 **持久化存储** - PostgreSQL 任务状态追踪
- 📦 **对象存储** - MinIO 双 Endpoint 模式，支持内外网分离
- 🔌 **整洁架构** - 领域驱动设计，依赖倒置
- 📐 **智能宽高比** - 横屏/竖屏自适应，保持原始比例
- ⚠️ **极端比例防护** - 拒绝 1:16 以外或 16:1 以外的极端比例视频
- 🧪 **E2E 测试** - 端到端测试 + 可视化 HTML 报告

## 技术栈

- **RPC 框架**: [Connect RPC](https://connectrpc.com/)
- **IDL**: Protobuf + [Buf v2](https://buf.build/)
- **依赖注入**: [Google Wire](https://github.com/google/wire)
- **ORM**: [GORM](https://gorm.io/)
- **消息队列**: RabbitMQ
- **对象存储**: MinIO（双 Endpoint 模式）
- **数据库**: PostgreSQL
- **容器化**: Docker Compose

## 项目架构

采用标准的**整洁架构 (Clean Architecture)** + **golang-standards/project-layout** 布局。

### 目录结构

```
cloud-media/
├── cmd/                          # 应用入口
│   ├── api-server/              # API 服务器
│   │   ├── main.go
│   │   ├── wire.go              # Wire 配置
│   │   ├── wire_gen.go          # Wire 生成 (不要编辑)
│   │   └── server.go
│   └── worker/                  # Worker 服务（消费 MQ + 转码）
│       ├── main.go
│       ├── wire.go
│       └── wire_gen.go
├── internal/                     # 私有应用代码
│   ├── domain/                  # 领域层 (Enterprise Business Rules)
│   │   └── video.go             # 实体和接口定义
│   ├── usecase/                 # 用例层 (Application Business Rules)
│   │   ├── video_usecase.go     # API 业务逻辑
│   │   └── worker.go            # Worker 业务逻辑
│   ├── adapter/                 # 适配器层 (Interface Adapters)
│   │   └── rpc/                 # RPC 适配器
│   └── infrastructure/          # 基础设施层 (Frameworks & Drivers)
│       ├── broker/              # RabbitMQ
│       ├── persistence/         # PostgreSQL + GORM
│       ├── storage/             # MinIO 对象存储
│       └── transcoder/          # FFmpeg 转码器
├── pkg/                          # 公共库（可被外部引用）
│   ├── config/                  # 配置管理
│   ├── logger/                  # 日志系统
│   ├── interceptor/             # 拦截器
│   └── ffmpeg/                  # FFmpeg 基础封装
│       ├── ffmpeg.go            # FFmpeg 命令封装
│       ├── ffprobe.go           # FFprobe 命令封装
│       ├── video_info.go        # 视频信息解析
│       ├── scale.go             # 缩放计算和宽高比验证
│       └── progress.go          # 进度解析
├── proto/                        # Protobuf 定义
├── test/                         # 测试
│   └── e2e/                     # 端到端测试
│       ├── main.go              # E2E 测试程序
│       └── template.html        # HTML 报告模板
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
│  基础设施层 (Infrastructure) ◄──  PostgreSQL/RabbitMQ/MinIO  │
│                               ────  GORM 实现                  │
└─────────────────────────────────────────────────────────────┘
```

### 数据库设计

- **video_tasks** - 视频任务主表，支持 JSONB 灵活扩展 HLS 输出信息
- **task_status_logs** - 任务状态变更历史

详见 [doc/DATABASE_DESIGN_V2.md](doc/DATABASE_DESIGN_V2.md)

### MinIO 双 Endpoint 设计

**亮点**

采用**双客户端模式**，分离内外网：

| 客户端 | Endpoint | 用途 |
|---------|----------|------|
| **coreClient** | 内网 | 上传、下载、Bucket 管理 |
| **signerClient** | 外网 | 仅生成预签名 URL |

**配置示例**：

```go
Config{
    InternalEndpoint: "minio:9000",   // 内网
    ExternalEndpoint: "", // 外网
}
```

## 快速开始

### 前置要求

- Go 1.25+
- Docker & Docker Compose
- Wire CLI (`go install github.com/google/wire/cmd/wire@latest`)
- Buf CLI

### 方式一：Docker Compose 一键启动（推荐）

```bash
# 1. 构建镜像
docker build --target api-server -t cloud-media-api-server .
docker build --target worker -t cloud-media-worker .

# 2. 启动全部服务
docker compose up -d
```

全部服务:
- API Server: http://localhost:8080
- MinIO: http://localhost:9001 (rootadmin / rootpassword)
- RabbitMQ: http://localhost:15672 (guest / guest)
- PostgreSQL: localhost:5432 (postgres / password)
- Grafana: http://localhost:3000 (admin / password)

### 方式二：本地开发

#### 1. 启动基础设施

```bash
docker compose up -d minio rabbitmq postgres
```

#### 2. 生成代码

```bash
# 生成 protobuf 代码
./scripts/bufgen.sh

# 生成依赖注入
cd cmd/api-server && wire
cd ../worker && wire
```

#### 3. 运行服务

**启动 API Server:**
```bash
go run ./cmd/api-server
```

**启动 Worker:**
```bash
go run ./cmd/worker
```

API 服务将在 `http://localhost:8080` 启动。

#### 4. 运行 E2E 测试

```bash
go run ./test/e2e -video /path/to/video.mp4
```

测试完成后会生成 `test_result.html` 报告。

详见 [test/README.md](test/README.md)

### Docker 构建

项目根目录包含多阶段构建的 `Dockerfile`，支持两个服务：

**构建 API Server 镜像：**
```bash
docker build --target api-server -t cloud-media-api-server .
```

**构建 Worker 镜像：**
```bash
docker build --target worker -t cloud-media-worker .
```

**镜像特性：**
- 基于 Alpine Linux，体积小
- 包含 FFmpeg
- 多阶段构建，最终镜像仅包含二进制文件

**运行示例：**
```bash
# API Server
docker run -d --name cloud-media-api \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  cloud-media-api-server

# Worker
docker run -d --name cloud-media-worker \
  -v $(pwd)/config.yaml:/app/config.yaml \
  cloud-media-worker
```

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
- ✅ Infrastructure 层 - 数据库 + MQ + MinIO 完成
- ✅ API 层 - 完整
- ✅ MinIO 集成 - 已完成（双 Endpoint 模式）
- ✅ Worker 服务 - 已完成
- ✅ FFmpeg 转码 - 已完成（HLS 切片、多码率、智能宽高比）
- ✅ pkg/ffmpeg - FFmpeg/FFprobe 基础封装
- ✅ E2E 测试 - 端到端测试 + HTML 报告

详见 [doc/TODO.md](doc/TODO.md)

### 智能宽高比处理

**横屏视频**（宽 ≥ 高）：
- 固定目标高度（1080/720/480）
- 按比例计算宽度

**竖屏视频**（高 > 宽）：
- 固定目标宽度（1080/720/480）
- 按比例计算高度

**宽高比限制**：
- 允许范围：1:16 ~ 16:1
- 超出范围拒绝处理

### 重新生成代码

```bash
# Buf 生成 proto 代码
./scripts/bufgen.sh

# Wire 生成依赖注入
cd cmd/api-server && wire
cd ../worker && wire
```

### 项目布局参考

- [golang-standards/project-layout](https://github.com/golang-standards/project-layout)
- [Clean Architecture](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)

## License

[Apache License 2.0](LICENSE)
