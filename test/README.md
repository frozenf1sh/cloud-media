# 端到端测试

## 前置条件

1. 启动 Docker Compose 服务：
```bash
docker compose up -d
```

2. 确认服务运行正常：
```bash
docker compose ps
```

3. 编译项目（可选）：
```bash
go build -o bin/api-server ./cmd/api-server
go build -o bin/worker ./cmd/worker
```

## 运行测试

### 启动服务

在两个终端窗口中分别运行：

**终端 1 - 启动 API Server:**
```bash
go run ./cmd/api-server
```

**终端 2 - 启动 Worker:**
```bash
go run ./cmd/worker
```

### 运行 E2E 测试

在第三个终端中运行：

```bash
# 基本用法
go run ./test/e2e -video /path/to/your/video.mp4

# 指定 API 地址
go run ./test/e2e -video /path/to/video.mp4 -api-addr http://localhost:8080

# 指定任务 ID
go run ./test/e2e -video /path/to/video.mp4 -task-id my-custom-id

# 指定输出 HTML 文件
go run ./test/e2e -video /path/to/video.mp4 -output result.html

# 使用自定义配置文件
go run ./test/e2e -video /path/to/video.mp4 -config ./config.yaml
```

### 查看结果

测试完成后，会在当前目录生成 `test_result.html`（或你指定的文件名），在浏览器中打开即可查看：

```bash
# macOS
open test_result.html

# Linux
xdg-open test_result.html

# Windows
start test_result.html
```

## 测试流程

1. **检查视频文件** - 验证本地视频文件存在
2. **初始化 MinIO 客户端** - 连接对象存储
3. **上传视频** - 将视频上传到 MinIO 的 `media-input` bucket
4. **提交任务** - 调用 API 提交转码任务
5. **轮询状态** - 每 2 秒查询一次任务状态
6. **生成播放 URL** - 任务成功后生成访问地址
7. **生成 HTML 报告** - 创建可视化测试报告

## 命令行参数

| 参数 | 说明 | 默认值 | 是否必需 |
|------|------|--------|----------|
| `-video` | 视频文件路径 | - | ✅ 必需 |
| `-task-id` | 任务 ID | 自动生成 | 可选 |
| `-api-addr` | API 服务器地址 | http://localhost:8080 | 可选 |
| `-config` | 配置文件路径 | - | 可选 |
| `-output` | 输出 HTML 路径 | test_result.html | 可选 |

## 故障排查

### 连接被拒绝

确保 API Server 和 Worker 都已启动，并且端口没有被占用。

### MinIO 访问错误

检查 `config.yaml` 中的 MinIO 配置是否正确，确保 Docker Compose 中的 MinIO 服务正常运行。

### FFmpeg 未找到

Worker 需要 FFmpeg 才能工作。确保系统已安装 FFmpeg：

```bash
# Ubuntu/Debian
sudo apt install ffmpeg

# macOS
brew install ffmpeg
```

### 任务一直 pending

检查 RabbitMQ 连接，确认 Worker 正在运行并能正常消费消息。
