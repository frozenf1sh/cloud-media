# =============================================
# 多阶段构建 - Builder 阶段
# =============================================
FROM golang:1.25-alpine AS builder

# 设置 Go 代理
ENV GOPROXY=https://goproxy.cn,direct

# 安装必要的工具
RUN apk add --no-cache git

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译 API Server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/api-server ./cmd/api-server

# 编译 Worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/worker ./cmd/worker

# =============================================
# 运行时镜像 - FFmpeg 基础镜像
# =============================================
FROM alpine:3.21 AS ffmpeg-base

# 安装 FFmpeg
RUN apk add --no-cache ffmpeg

# =============================================
# API Server 镜像
# =============================================
FROM alpine:3.21 AS api-server

# 设置工作目录
WORKDIR /app

# 从 builder 阶段复制二进制文件
COPY --from=builder /app/bin/api-server .

# 暴露端口
EXPOSE 8080

# 运行
ENTRYPOINT ["/app/api-server"]

# =============================================
# Worker 镜像
# =============================================
FROM ffmpeg-base AS worker

# 设置工作目录
WORKDIR /app

# 从 builder 阶段复制二进制文件
COPY --from=builder /app/bin/worker .

# 运行
ENTRYPOINT ["/app/worker"]
