package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"simplemail/mail"

	consul "github.com/hashicorp/consul/api"
	"github.com/zeromicro/go-zero/core/conf"
	"google.golang.org/grpc"
)

var (
	configFile = flag.String("f", "etc/mail.yaml", "配置文件路径")
)

type Config struct {
	Consul struct {
		Host string `json:"Host" yaml:"Host"`
	} `json:"Consul" yaml:"Consul"`
	Smtp struct {
		DefaultFrom string `json:"DefaultFrom" yaml:"DefaultFrom"`
	} `json:"Smtp" yaml:"Smtp"`
}

func main() {
	flag.Parse()
	log.SetFlags(0)

	fmt.Printf("读取配置文件: %s\n", *configFile)
	var cfg Config
	if err := conf.Load(*configFile, &cfg); err != nil {
		log.Fatal("配置文件读取失败:", err)
	}

	if cfg.Consul.Host == "" {
		log.Fatal("配置文件中未找到Consul地址")
	}

	fmt.Printf("使用Consul地址: %s\n", cfg.Consul.Host)

	config := consul.DefaultConfig()
	config.Address = cfg.Consul.Host

	client, err := consul.NewClient(config)
	if err != nil {
		log.Fatal("Consul连接失败:", err)
	}

	serviceName := "mail.rpc"
	fmt.Printf("查找服务: %s\n", serviceName)

	var services []*consul.ServiceEntry
	for i := 0; i < 3; i++ {
		services, _, err = client.Health().Service(serviceName, "", true, nil)
		if err == nil && len(services) > 0 {
			break
		}

		if i < 2 {
			fmt.Printf("服务查询失败，重试 %d/3...\n", i+1)
			time.Sleep(1 * time.Second)
		}
	}

	if err != nil {
		log.Fatal("服务查询失败:", err)
	}

	if len(services) == 0 {
		log.Fatal("服务未找到")
	}

	service := services[0].Service
	target := fmt.Sprintf("%s:%d", service.Address, service.Port)
	fmt.Printf("发现服务地址: %s\n", target)

	conn, err := grpc.Dial(target, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(3*time.Second))
	if err != nil {
		log.Fatal("gRPC连接失败:", err)
	}
	defer conn.Close()

	fmt.Println("使用默认发件人:", cfg.Smtp.DefaultFrom)

	mailClient := mail.NewMailServiceClient(conn)
	req := &mail.SendMailRequest{
		From:    cfg.Smtp.DefaultFrom,
		To:      []string{"leo-cheng@vip.qq.com"},
		Subject: fmt.Sprintf("测试邮件 %s", time.Now().Format("2006-01-02 15:04:05")),
		Content: "<h1>邮件服务测试</h1><p>这是一封自动发送的测试邮件</p>",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := mailClient.SendMail(ctx, req)
	if err != nil {
		log.Fatal("邮件发送失败:", err)
	}

	if resp.Status == 200 {
		fmt.Println("✅ 测试成功")
		fmt.Printf("消息ID: %s\n", resp.Data)
		os.Exit(0)
	} else {
		fmt.Printf("❌ 测试失败 (状态码: %d)\n", resp.Status)
		fmt.Printf("错误信息: %s\n", resp.Message)
		os.Exit(1)
	}
}
