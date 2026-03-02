// +build ignore

package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	pb "github.com/frozenf1sh/cloud-media/proto/gen/api/v1"
	v1connect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
)

func main() {
	// 创建客户端
	client := v1connect.NewVideoServiceClient(
		http.DefaultClient,
		"http://localhost:8080",
	)

	// 准备请求
	req := connect.NewRequest(&pb.SubmitTaskRequest{
		TaskId:   "test-task-001",
		VideoKey: "videos/input/test.mp4",
	})

	// 设置超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 调用服务
	fmt.Println("📤 发送请求到服务器...")
	resp, err := client.SubmitTask(ctx, req)
	if err != nil {
		fmt.Printf("❌ 错误: %v\n", err)
		fmt.Println("\n提示: 请确保 API 服务器正在运行 (go run ./cmd/api-server)")
		return
	}

	fmt.Println("✅ 请求成功!")
	fmt.Printf("  TaskId:  %s\n", resp.Msg.TaskId)
	fmt.Printf("  Status:  %s\n", resp.Msg.Status)
	fmt.Printf("  Message: %s\n", resp.Msg.Message)
}
