# 测试工具

本目录包含两个测试工具：
- **e2e**: 端到端功能测试，验证单个任务的完整流程
- **loadtest**: 负载测试/性能测试，验证系统在高并发下的表现

---

## 端到端测试 (E2E)

### 前置条件

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

### 运行 E2E 测试

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

---

## 负载测试 (Load Test)

`loadtest` 工具用于测试系统在高并发场景下的性能表现。它会提交多个任务，等待所有任务完成，并输出详细的性能统计报告。

### 前置条件

与 E2E 测试相同，需要 API Server 和 Worker 正常运行。

### 基本用法

```bash
# 基本负载测试（10个任务，5个并发）
go run ./test/loadtest -video /path/to/video.mp4

# 自定义任务数量和并发度
go run ./test/loadtest -video ./test.mp4 -count 50 -concurrency 10

# 指定 API 地址（用于 k8s 环境）
go run ./test/loadtest -video ./test.mp4 -api-addr http://media-api.frozenf1sh.loc/

# 上传复用模式（只上传一次文件，所有任务复用）
go run ./test/loadtest -video ./test.mp4 -count 100 -reuse-upload

# 设置任务提交间隔（控制提交速率）
go run ./test/loadtest -video ./test.mp4 -count 20 -interval 500ms
```

### 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-video` | 视频文件路径（必需） | - |
| `-count` | 提交的任务总数 | 10 |
| `-concurrency` | 并发提交的 worker 数 | 5 |
| `-api-addr` | API 服务器地址 | http://media-api.frozenf1sh.loc/ |
| `-interval` | 任务提交间隔（如 1s, 500ms） | 0（无间隔） |
| `-reuse-upload` | 上传一次文件，所有任务复用 | false |

### 测试流程

1. **Phase 1: 提交任务** - 并发提交所有任务
2. **Phase 2: 等待完成** - 轮询所有任务状态，每 2 秒更新进度
3. **生成报告** - 所有任务完成后输出性能统计

### 性能统计输出

测试完成后会输出以下统计信息：

```
==============================================
          LOAD TEST SUMMARY
==============================================

--- Submission Phase ---
Submit Duration:    5.234s

--- Processing Phase ---
Poll Duration:      2m15.678s

--- Overall Results ---
Total Tasks:        50
Successful:         48
Failed:             2
Success Rate:       96.00%
Total Wall Time:    2m20.912s
Throughput:         0.35 tasks/sec

--- Queue Time Statistics ---
  Count:    48
  Min:      1.234s
  Max:      15.678s
  Mean:     5.432s
  StdDev:   3.210s
  P50:      4.567s
  P95:      12.345s
  P99:      14.567s

--- Processing Time Statistics ---
  Count:    48
  ...

--- Total Time Statistics ---
  Count:    48
  ...

--- Failed Tasks ---
  Task xxx: error message...

==============================================
```

### 统计指标说明

| 指标 | 说明 |
|------|------|
| **Queue Time** | 任务从提交到开始处理的等待时间 |
| **Processing Time** | 实际转码处理时间 |
| **Total Time** | 从提交到完成的总耗时 |
| **Throughput** | 吞吐量（每秒完成任务数） |
| **P50/P95/P99** | 百分位数（50%/95%/99% 的任务在此时间内完成） |

### 使用场景

1. **功能验证**: `-count 10 -concurrency 5` - 快速验证系统正常工作
2. **性能基准**: `-count 50 -concurrency 10 -reuse-upload` - 获取系统基准性能
3. **压力测试**: `-count 200 -concurrency 20 -reuse-upload` - 测试系统极限能力
4. **KEDA 扩缩容验证**: `-count 100 -concurrency 5 -interval 1s` - 观察 KEDA 根据队列长度自动扩容

### Kubernetes 环境使用示例

```bash
# 使用 k3s 环境的 API 地址
go run ./test/loadtest \
  -video ./test/assets/sample.mp4 \
  -api-addr http://media-api.frozenf1sh.loc/ \
  -count 50 \
  -concurrency 10 \
  -reuse-upload

# 观察 KEDA 扩缩容（另一个终端）
kubectl get pods -n cloud-media -w
```

### 注意事项

1. **视频文件大小**: 大视频文件会增加测试时间，建议使用 10-30 秒的短视频进行测试
2. **-reuse-upload**: 测试大量任务时强烈建议使用，可大幅减少上传时间和存储消耗
3. **Worker 数量**: 确保有足够的 Worker Pod 处理任务，或启用 KEDA 自动扩缩容
4. **资源限制**: 注意观察系统资源（CPU/内存/磁盘）使用情况
