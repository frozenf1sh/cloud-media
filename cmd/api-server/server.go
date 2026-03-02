package main

import (
	"github.com/frozenf1sh/cloud-media/internal/adapter/rpc"
	v1connect "github.com/frozenf1sh/cloud-media/proto/gen/api/v1/v1connect"
	"net/http"
)

// Server 持有 HTTP handler 和路径
type Server struct {
	Path    string
	Handler http.Handler
}

func NewServer(videoServer *rpc.VideoServer) *Server {
	path, handler := v1connect.NewVideoServiceHandler(videoServer)
	return &Server{
		Path:    path,
		Handler: handler,
	}
}
