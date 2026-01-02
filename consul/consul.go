package consul

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

type Registrar struct {
	client      *api.Client
	serviceID   string
	serviceName string
	registered  bool
	mu          sync.RWMutex
}

func NewRegistrar(consulHost string) (*Registrar, error) {
	config := api.DefaultConfig()
	config.Address = consulHost

	if config.HttpClient == nil {
		config.HttpClient = &http.Client{
			Timeout: 5 * time.Second,
		}
	} else {
		config.HttpClient.Timeout = 5 * time.Second
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create consul client failed: %v", err)
	}

	return &Registrar{
		client:     client,
		registered: false,
	}, nil
}

// Register 注册服务到Consul，支持重试
func (r *Registrar) Register(serviceName, listenOn string, maxRetries int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.registered {
		return nil
	}

	r.serviceName = serviceName

	host, port, err := parseListenOn(listenOn)
	if err != nil {
		return err
	}

	hostname, _ := os.Hostname()
	pid := os.Getpid()
	timestamp := time.Now().Unix()
	r.serviceID = fmt.Sprintf("%s-%s-%d-%d-%d", serviceName, hostname, pid, port, timestamp)

	registration := &api.AgentServiceRegistration{
		ID:      r.serviceID,
		Name:    serviceName,
		Address: host,
		Port:    port,
		Tags:    []string{"mail", "notification", "grpc"},
		Check: &api.AgentServiceCheck{
			TCP:                            fmt.Sprintf("%s:%d", host, port),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}

		err := r.client.Agent().ServiceRegister(registration)
		if err == nil {
			r.registered = true
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("after %d retries: %v", maxRetries, lastErr)
}

// Deregister 从Consul注销服务，支持重试
func (r *Registrar) Deregister() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.registered || r.serviceID == "" {
		return nil
	}

	var lastErr error
	for i := 0; i < 3; i++ { // 最多重试3次
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}

		err := r.client.Agent().ServiceDeregister(r.serviceID)
		if err == nil {
			log.Printf("Service %s (ID: %s) deregistered from Consul",
				r.serviceName, r.serviceID)
			r.registered = false
			r.serviceID = ""
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("failed to deregister service after 3 retries: %v", lastErr)
}

// IsRegistered 检查服务是否已注册
func (r *Registrar) IsRegistered() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registered
}

// KeepAlive 保持服务存活状态
func (r *Registrar) KeepAlive(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.IsRegistered() {
				// 发送心跳/健康检查
				r.client.Agent().UpdateTTL("service:"+r.serviceID, "healthy", api.HealthPassing)
			}
		}
	}
}

// 解析监听地址，返回主机和端口
func parseListenOn(listenOn string) (string, int, error) {
	if listenOn == "" {
		return "0.0.0.0", 50051, nil
	}

	// 使用net包的标准解析
	host, portStr, err := net.SplitHostPort(listenOn)
	if err != nil {
		// 尝试添加默认端口
		if strings.Contains(err.Error(), "missing port") {
			// 如果是IPv6地址
			if strings.Contains(listenOn, ":") && !strings.Contains(listenOn, "[") {
				listenOn = "[" + listenOn + "]:50051"
			} else {
				listenOn = listenOn + ":50051"
			}
			host, portStr, err = net.SplitHostPort(listenOn)
		}
		if err != nil {
			return "0.0.0.0", 50051, fmt.Errorf("invalid listen address %s: %v", listenOn, err)
		}
	}

	// 解析端口
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "0.0.0.0", 50051, fmt.Errorf("invalid port %s: %v", portStr, err)
	}

	// 处理通配符地址
	if host == "" || host == "0.0.0.0" || host == "[::]" || host == "[::1]" {
		// 获取本地非回环IP
		localIP, err := getLocalIP()
		if err != nil {
			log.Printf("Warning: Failed to get local IP: %v, using 127.0.0.1", err)
			host = "127.0.0.1"
		} else {
			host = localIP
		}
	}

	return host, port, nil
}

// getLocalIP 获取本地非回环IP地址
func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no non-loopback IP address found")
}

// GetServiceInfo 获取服务信息
func (r *Registrar) GetServiceInfo() (string, string, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.registered {
		return "", "", 0, fmt.Errorf("service not registered")
	}

	services, err := r.client.Agent().Services()
	if err != nil {
		return "", "", 0, err
	}

	service, ok := services[r.serviceID]
	if !ok {
		return "", "", 0, fmt.Errorf("service not found in Consul")
	}

	return service.ID, service.Address, service.Port, nil
}
