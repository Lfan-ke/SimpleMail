package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"simplemail/internal/config"
	"simplemail/internal/server"
	"simplemail/internal/svc"
	"simplemail/mail"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/mail.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	printConfig(&c)

	if err := checkPort(c.ListenOn); err != nil {
		fmt.Printf("端口检查失败: %v\n", err)
		os.Exit(1)
	}

	svcCtx := svc.NewServiceContext(&c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var consulRegistered bool
	if svcCtx.Consul != nil && c.Consul.Host != "" {
		fmt.Printf("连接Consul: %s\n", c.Consul.Host)

		serverData := map[string]interface{}{
			"fields": map[string]interface{}{
				"from": map[string]interface{}{
					"required":    false,
					"type":        "string",
					"format":      "email",
					"description": "发件人邮箱地址",
				},
				"to": map[string]interface{}{
					"required": true,
					"type":     "array",
					"items": map[string]interface{}{
						"type":   "string",
						"format": "email",
					},
					"description": "收件人邮箱列表",
				},
				"subject": map[string]interface{}{
					"required":    true,
					"type":        "string",
					"description": "邮件主题",
				},
				"content": map[string]interface{}{
					"required":    true,
					"type":        "string",
					"description": "邮件内容（HTML格式）",
				},
			},
		}

		if err := svcCtx.Consul.RegisterService(
			c.Name,
			c.ListenOn,
			3,
			"邮件发送微服务，支持HTML格式邮件发送",
			serverData,
		); err != nil {
			fmt.Printf("Consul注册失败: %v\n", err)
		} else {
			consulRegistered = true
			fmt.Println("Consul注册成功")
			go svcCtx.Consul.KeepAlive(ctx)
			defer func() {
				if err := svcCtx.Consul.DeregisterService(); err != nil {
					fmt.Printf("Consul注销失败: %v\n", err)
				}
			}()
		}
	}

	if err := startServer(ctx, &c, svcCtx, consulRegistered); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
		os.Exit(1)
	}
}

func printConfig(c *config.Config) {
	fmt.Printf("\n服务配置:\n")
	fmt.Printf("  服务名称: %s\n", c.Name)
	fmt.Printf("  监听地址: %s\n", c.ListenOn)
	fmt.Printf("  运行模式: %s\n", c.Mode)
	if c.Consul.Host != "" {
		fmt.Printf("  Consul地址: %s\n", c.Consul.Host)
	}
	fmt.Printf("  SMTP服务器: %s:%d\n", c.Smtp.Host, c.Smtp.Port)
	fmt.Printf("  发件邮箱: %s\n", maskPassword(c.Smtp.Username))
	fmt.Println()
}

func maskPassword(password string) string {
	if len(password) <= 4 {
		return "****"
	}
	return password[:2] + strings.Repeat("*", len(password)-4) + password[len(password)-2:]
}

func checkPort(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("地址格式错误: %v", err)
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("端口被占用: %s", port)
	}
	listener.Close()

	fmt.Printf("端口可用: %s\n", port)
	return nil
}

func startServer(ctx context.Context, cfg *config.Config, svcCtx *svc.ServiceContext, consulRegistered bool) error {
	s := zrpc.MustNewServer(cfg.RpcServerConf, func(grpcServer *grpc.Server) {
		mail.RegisterMailServiceServer(grpcServer, server.NewMailServiceServer(svcCtx))
		if cfg.Mode == service.DevMode || cfg.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	defer s.Stop()

	fmt.Printf("\n邮件服务启动\n")
	fmt.Printf("监听地址: %s\n", cfg.ListenOn)
	if consulRegistered {
		fmt.Printf("服务发现: Consul (KV路径: echo_wing/%s)\n", cfg.Name)
	}
	fmt.Printf("启动时间: %s\n", time.Now().Format("15:04:05"))
	fmt.Println()

	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				serverErr <- fmt.Errorf("服务器异常: %v", r)
			}
		}()
		s.Start()
	}()

	select {
	case <-ctx.Done():
		fmt.Println("停止服务...")
	case sig := <-gracefulStop:
		fmt.Printf("收到信号 %v，停止服务...\n", sig)
	case err := <-serverErr:
		fmt.Printf("服务器错误: %v\n", err)
		return err
	}

	time.Sleep(500 * time.Millisecond)
	fmt.Println("服务已停止")
	return nil
}
