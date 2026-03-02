package main

import (
	"log"
	"net/http"
)

func main() {
	// 使用 Wire 生成的依赖注入函数
	server, err := InitializeVideoServer()
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	log.Println("Service path:", server.Path)

	// 注册路由并启动 HTTP 服务器
	mux := http.NewServeMux()
	mux.Handle(server.Path, server.Handler)

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
