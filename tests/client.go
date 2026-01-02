package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"time"

	"simplemail/mail"

	consul "github.com/hashicorp/consul/api"
	"github.com/zeromicro/go-zero/core/conf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	KVBasePath = "echo_wing/"
)

type KVServiceMeta struct {
	ServerName string                 `json:"ServerName"`
	ServerDesc string                 `json:"ServerDesc,omitempty"`
	ServerData map[string]interface{} `json:"ServerData,omitempty"`
	CreatedAt  int64                  `json:"created_at"`
	UpdatedAt  int64                  `json:"updated_at"`
}

var (
	configFile = flag.String("f", "etc/mail.yaml", "配置文件路径")
)

type Config struct {
	Name     string `json:"Name" yaml:"Name"`
	ListenOn string `json:"ListenOn" yaml:"ListenOn"`
	Consul   struct {
		Host string `json:"Host" yaml:"Host"`
	} `json:"Consul" yaml:"Consul"`
	Smtp struct {
		DefaultFrom string `json:"DefaultFrom" yaml:"DefaultFrom"`
	} `json:"Smtp" yaml:"Smtp"`
}

func testTCPConnection(addr string, timeout time.Duration) error {
	fmt.Printf("测试TCP连接: %s...\n", addr)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("TCP连接失败: %v", err)
	}
	defer conn.Close()
	fmt.Println("✅ TCP连接成功")
	return nil
}

func waitForConnectionReady(conn *grpc.ClientConn, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			currentState := conn.GetState()
			return fmt.Errorf("连接超时 (等待: %v, 状态: %v)", time.Since(start), currentState)
		default:
			state := conn.GetState()

			if time.Since(start).Seconds() >= 1 && int(time.Since(start).Seconds())%2 == 0 {
				fmt.Printf("等待连接建立... (状态: %v, 已等待: %v)\n", state, time.Since(start))
			}

			switch state {
			case connectivity.Ready:
				fmt.Printf("✅ gRPC连接已建立 (耗时: %v)\n", time.Since(start))
				return nil
			case connectivity.TransientFailure:
				fmt.Printf("⚠️  连接暂时失败，等待重试... (状态: %v)\n", state)
			case connectivity.Shutdown:
				return fmt.Errorf("连接已关闭")
			case connectivity.Idle:
				conn.Connect()
			}

			if !conn.WaitForStateChange(ctx, state) {
				currentState := conn.GetState()
				return fmt.Errorf("等待状态变化超时 (最终状态: %v)", currentState)
			}
		}
	}
}

func createGRPCConnection(target string) (*grpc.ClientConn, error) {
	fmt.Printf("创建gRPC连接到: %s...\n", target)

	if err := testTCPConnection(target, 5*time.Second); err != nil {
		return nil, fmt.Errorf("TCP连接测试失败: %v", err)
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	if err != nil {
		return nil, fmt.Errorf("创建gRPC客户端失败: %v", err)
	}

	return conn, nil
}

func getKVServiceMeta(client *consul.Client, serviceName string) (*KVServiceMeta, error) {
	kvPath := path.Join(KVBasePath, serviceName)
	kvPair, _, err := client.KV().Get(kvPath, nil)
	if err != nil {
		return nil, fmt.Errorf("get KV failed: %v", err)
	}

	if kvPair == nil {
		return nil, fmt.Errorf("KV not found for service %s (path: %s)", serviceName, kvPath)
	}

	var kvMeta KVServiceMeta
	if err := json.Unmarshal(kvPair.Value, &kvMeta); err != nil {
		return nil, fmt.Errorf("unmarshal KV meta failed: %v", err)
	}

	return &kvMeta, nil
}

func printServiceInstances(services []*consul.ServiceEntry) {
	fmt.Println("\n发现的服务实例:")
	fmt.Println("┌───────────────────────────────────────────────────────────────┐")
	for i, serviceEntry := range services {
		service := serviceEntry.Service
		status := "健康"
		if serviceEntry.Checks != nil {
			for _, check := range serviceEntry.Checks {
				if check.Status != "passing" {
					status = "不健康"
					break
				}
			}
		}

		fmt.Printf("│ 实例 %d:\n", i+1)
		fmt.Printf("│   ID:      %s\n", service.ID)
		fmt.Printf("│   名称:    %s\n", service.Service)
		fmt.Printf("│   地址:    %s:%d\n", service.Address, service.Port)
		fmt.Printf("│   状态:    %s\n", status)
		if len(service.Tags) > 0 {
			fmt.Printf("│   标签:    %v\n", service.Tags)
		}
		if len(service.Meta) > 0 {
			fmt.Println("│   元数据:")
			for key, value := range service.Meta {
				fmt.Printf("│     - %s: %s\n", key, value)
			}
		}
		if i < len(services)-1 {
			fmt.Println("│  ───────────────────────────────────────────────────────────")
		}
	}
	fmt.Println("└───────────────────────────────────────────────────────────────┘")
}

func printKVMeta(kvMeta *KVServiceMeta, kvPath string) {
	fmt.Println("\n服务KV元信息:")
	fmt.Println("┌───────────────────────────────────────────────────────────────┐")
	fmt.Printf("│ KV路径:     %s\n", kvPath)
	fmt.Printf("│ 服务名称:   %s\n", kvMeta.ServerName)
	if kvMeta.ServerDesc != "" {
		fmt.Printf("│ 服务描述:   %s\n", kvMeta.ServerDesc)
	}
	fmt.Printf("│ 创建时间:   %s\n", time.Unix(kvMeta.CreatedAt, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("│ 更新时间:   %s\n", time.Unix(kvMeta.UpdatedAt, 0).Format("2006-01-02 15:04:05"))

	if kvMeta.ServerData != nil {
		fmt.Println("│")
		fmt.Println("│ 接口定义:")
		if fields, ok := kvMeta.ServerData["fields"].(map[string]interface{}); ok && len(fields) > 0 {
			for fieldName, fieldDef := range fields {
				if fieldMap, ok := fieldDef.(map[string]interface{}); ok {
					required := "否"
					if req, ok := fieldMap["required"].(bool); ok && req {
						required = "是"
					}
					fieldType := "未知"
					if typ, ok := fieldMap["type"].(string); ok {
						fieldType = typ
					}
					description := ""
					if desc, ok := fieldMap["description"].(string); ok {
						description = desc
					}

					fmt.Printf("│   %s:\n", fieldName)
					fmt.Printf("│     - 类型: %s, 必需: %s\n", fieldType, required)
					if description != "" {
						fmt.Printf("│     - 描述: %s\n", description)
					}
				}
			}
		}
	}
	fmt.Println("└───────────────────────────────────────────────────────────────┘")
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

	serviceName := cfg.Name
	fmt.Printf("\n========================================\n")
	fmt.Printf("查找服务: %s\n", serviceName)
	fmt.Printf("KV路径: echo_wing/%s\n", serviceName)
	fmt.Printf("========================================\n")

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

	printServiceInstances(services)

	fmt.Printf("\n========================================\n")
	fmt.Println("检查KV存储...")
	fmt.Printf("========================================\n")

	kvMeta, err := getKVServiceMeta(client, serviceName)
	if err != nil {
		log.Printf("警告: 获取KV元信息失败: %v", err)
	} else {
		printKVMeta(kvMeta, path.Join(KVBasePath, serviceName))

		if kvMeta.ServerName != serviceName {
			fmt.Printf("⚠️  警告: KV中的服务名称(%s)与配置的服务名称(%s)不匹配\n",
				kvMeta.ServerName, serviceName)
		} else {
			fmt.Println("✅ KV验证通过: 服务名称匹配")
		}
	}

	service := services[0].Service
	target := fmt.Sprintf("%s:%d", service.Address, service.Port)
	fmt.Printf("\n========================================\n")
	fmt.Printf("选择实例: %s\n", service.ID)
	fmt.Printf("连接地址: %s\n", target)
	fmt.Printf("========================================\n")

	conn, err := createGRPCConnection(target)
	if err != nil {
		log.Fatal("gRPC连接创建失败:", err)
	}
	defer conn.Close()

	fmt.Println("等待连接就绪...")
	if err := waitForConnectionReady(conn, 10*time.Second); err != nil {
		log.Fatal("等待连接就绪失败:", err)
	}

	fmt.Println("使用默认发件人:", cfg.Smtp.DefaultFrom)

	mailClient := mail.NewMailServiceClient(conn)
	req := &mail.SendMailRequest{
		From:    cfg.Smtp.DefaultFrom,
		To:      []string{"leo-cheng@vip.qq.com"},
		Subject: fmt.Sprintf("测试邮件 %s", time.Now().Format("2006-01-02 15:04:05")),
		Content: "<h1>邮件服务测试</h1><p>这是一封自动发送的测试邮件</p>",
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sendCancel()

	fmt.Println("正在发送测试邮件...")
	resp, err := mailClient.SendMail(sendCtx, req)
	if err != nil {
		log.Fatal("邮件发送失败:", err)
	}

	if resp.Status == 200 {
		fmt.Println("✅ 测试成功")
		fmt.Printf("消息ID: %s\n", resp.Data)
		fmt.Printf("状态码: %d\n", resp.Status)
		fmt.Printf("消息: %s\n", resp.Message)
		os.Exit(0)
	} else {
		fmt.Printf("❌ 测试失败 (状态码: %d)\n", resp.Status)
		fmt.Printf("错误信息: %s\n", resp.Message)
		os.Exit(1)
	}
}
