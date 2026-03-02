package main

import (
	"log/slog"
	"net/http"

	"github.com/frozenf1sh/cloud-media/pkg/config"
	"github.com/frozenf1sh/cloud-media/pkg/interceptor"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("Failed to load config, using defaults", "error", err)
		cfg = config.Default()
	}

	// 2. 初始化日志
	logger.Init(cfg.Log.Level)
	slog.Info("Logger initialized", "level", cfg.Log.Level)

	// 3. 使用 Wire 生成的依赖注入函数
	server, err := InitializeVideoServer(cfg)
	if err != nil {
		slog.Error("Failed to initialize server", "error", err)
		panic(err)
	}

	slog.Info("Service path", "path", server.Path)

	// 4. 注册路由并启动 HTTP 服务器
	mux := http.NewServeMux()
	mux.Handle(server.Path, server.Handler)

	// 5. 使用追踪拦截器包装 mux
	handler := interceptor.TracingInterceptor(mux)

	addr := cfg.Server.Address()
	slog.Info("Server starting", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("Server failed to start", "error", err)
		panic(err)
	}
}
