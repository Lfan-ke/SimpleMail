package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
)

const (
	KVBasePath             = "echo_wing/"
	DefaultCheckInterval   = "10s"
	DefaultCheckTimeout    = "5s"
	DefaultDeregisterAfter = "30s"
)

type ServiceInfo struct {
	ServiceID   string            `json:"service_id"`
	ServiceName string            `json:"service_name"`
	Address     string            `json:"address"`
	Port        int               `json:"port"`
	Tags        []string          `json:"tags"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type KVServiceMeta struct {
	ServerName string                 `json:"ServerName"`
	ServerDesc string                 `json:"ServerDesc,omitempty"`
	ServerData map[string]interface{} `json:"ServerData,omitempty"`
	CreatedAt  int64                  `json:"created_at"`
	UpdatedAt  int64                  `json:"updated_at"`
}

type Registrar struct {
	client       *api.Client
	serviceID    string
	serviceName  string
	kvPath       string
	registered   bool
	kvRegistered bool
	mu           sync.RWMutex
}

func NewRegistrar(consulHost, token string) (*Registrar, error) {
	config := api.DefaultConfig()
	config.Address = consulHost
	config.Token = token

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
		client:       client,
		registered:   false,
		kvRegistered: false,
	}, nil
}

func (r *Registrar) RegisterService(serviceName, listenOn string, maxRetries int,
	serviceDesc string, serverData map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.registered {
		return nil
	}

	r.serviceName = serviceName
	r.kvPath = path.Join(KVBasePath, serviceName)

	kvExists, err := r.checkKVExists()
	if err != nil {
		log.Printf("Warning: Check KV exists failed: %v", err)
	}

	host, port, err := parseListenOn(listenOn)
	if err != nil {
		return fmt.Errorf("parse listen address failed: %v", err)
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
		Meta: map[string]string{
			"kv_path": r.kvPath,
			"version": "1.0.0",
			"host":    hostname,
			"pid":     fmt.Sprintf("%d", pid),
			"started": fmt.Sprintf("%d", timestamp),
		},
		Check: &api.AgentServiceCheck{
			TCP:                            fmt.Sprintf("%s:%d", host, port),
			Interval:                       DefaultCheckInterval,
			Timeout:                        DefaultCheckTimeout,
			DeregisterCriticalServiceAfter: DefaultDeregisterAfter,
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
			log.Printf("Service %s registered to Consul (ID: %s, KV Path: %s)",
				serviceName, r.serviceID, r.kvPath)
			break
		}
		lastErr = err
	}

	if lastErr != nil {
		return fmt.Errorf("after %d retries: %v", maxRetries, lastErr)
	}

	if !kvExists {
		if err := r.registerKV(serviceDesc, serverData); err != nil {
			log.Printf("Warning: Register KV failed: %v", err)
		} else {
			r.kvRegistered = true
			log.Printf("KV %s registered to Consul", r.kvPath)
		}
	} else {
		r.kvRegistered = true
		log.Printf("KV %s already exists, skip registration", r.kvPath)
	}

	return nil
}

func (r *Registrar) DeregisterService() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.registered || r.serviceID == "" {
		return nil
	}

	var lastErr error
	serviceDeregistered := false
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}

		err := r.client.Agent().ServiceDeregister(r.serviceID)
		if err == nil {
			serviceDeregistered = true
			log.Printf("Service %s (ID: %s) deregistered from Consul",
				r.serviceName, r.serviceID)
			break
		}
		lastErr = err
	}

	if !serviceDeregistered {
		return fmt.Errorf("failed to deregister service after 3 retries: %v", lastErr)
	}

	hasActiveServices, err := r.hasActiveServices()
	if err != nil {
		log.Printf("Warning: Check active services failed: %v", err)
		r.registered = false
		r.serviceID = ""
		return nil
	}

	if !hasActiveServices && r.kvRegistered {
		if err := r.deleteKV(); err != nil {
			log.Printf("Warning: Delete KV failed: %v", err)
		} else {
			log.Printf("KV %s deleted from Consul (no active instances)", r.kvPath)
			r.kvRegistered = false
		}
	}

	r.registered = false
	r.serviceID = ""
	return nil
}

func (r *Registrar) DiscoverServices(serviceName string) ([]*ServiceInfo, error) {
	services, _, err := r.client.Health().Service(serviceName, "", true, nil)
	if err != nil {
		return nil, fmt.Errorf("discover services failed: %v", err)
	}

	serviceInfos := make([]*ServiceInfo, 0, len(services))
	for _, service := range services {
		serviceInfos = append(serviceInfos, &ServiceInfo{
			ServiceID:   service.Service.ID,
			ServiceName: service.Service.Service,
			Address:     service.Service.Address,
			Port:        service.Service.Port,
			Tags:        service.Service.Tags,
			Meta:        service.Service.Meta,
		})
	}

	return serviceInfos, nil
}

func (r *Registrar) GetKVServiceMeta(serviceName string) (*KVServiceMeta, error) {
	kvPath := path.Join(KVBasePath, serviceName)
	kvPair, _, err := r.client.KV().Get(kvPath, nil)
	if err != nil {
		return nil, fmt.Errorf("get KV failed: %v", err)
	}

	if kvPair == nil {
		return nil, fmt.Errorf("KV not found for service %s", serviceName)
	}

	var kvMeta KVServiceMeta
	if err := json.Unmarshal(kvPair.Value, &kvMeta); err != nil {
		return nil, fmt.Errorf("unmarshal KV meta failed: %v", err)
	}

	return &kvMeta, nil
}

func (r *Registrar) UpdateKVServiceMeta(serviceDesc string, serverData map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.kvRegistered {
		return fmt.Errorf("KV not registered for service %s", r.serviceName)
	}

	kvMeta, err := r.GetKVServiceMeta(r.serviceName)
	if err != nil {
		return err
	}

	if serviceDesc != "" {
		kvMeta.ServerDesc = serviceDesc
	}

	if serverData != nil {
		kvMeta.ServerData = serverData
	}

	kvMeta.UpdatedAt = time.Now().Unix()

	data, err := json.Marshal(kvMeta)
	if err != nil {
		return fmt.Errorf("marshal KV meta failed: %v", err)
	}

	kvPair := &api.KVPair{
		Key:   r.kvPath,
		Value: data,
	}

	_, err = r.client.KV().Put(kvPair, nil)
	if err != nil {
		return fmt.Errorf("update KV failed: %v", err)
	}

	log.Printf("KV %s updated in Consul", r.kvPath)
	return nil
}

func (r *Registrar) WatchServices(ctx context.Context, serviceName string,
	handler func([]*ServiceInfo)) error {
	lastIndex := uint64(0)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			services, meta, err := r.client.Health().Service(serviceName, "", true,
				&api.QueryOptions{
					WaitIndex: lastIndex,
					WaitTime:  30 * time.Second,
				})

			if err != nil {
				log.Printf("Watch services error: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			if meta.LastIndex != lastIndex {
				lastIndex = meta.LastIndex

				serviceInfos := make([]*ServiceInfo, 0, len(services))
				for _, service := range services {
					serviceInfos = append(serviceInfos, &ServiceInfo{
						ServiceID:   service.Service.ID,
						ServiceName: service.Service.Service,
						Address:     service.Service.Address,
						Port:        service.Service.Port,
						Tags:        service.Service.Tags,
						Meta:        service.Service.Meta,
					})
				}

				if handler != nil {
					handler(serviceInfos)
				}
			}
		}
	}
}

func (r *Registrar) KeepAlive(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.IsRegistered() {
				r.client.Agent().UpdateTTL("service:"+r.serviceID, "healthy", api.HealthPassing)
			}
		}
	}
}

func (r *Registrar) IsRegistered() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registered
}

func (r *Registrar) GetServiceID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.serviceID
}

func (r *Registrar) GetServiceName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.serviceName
}

func (r *Registrar) checkKVExists() (bool, error) {
	kvPair, _, err := r.client.KV().Get(r.kvPath, nil)
	if err != nil {
		return false, err
	}
	return kvPair != nil, nil
}

func (r *Registrar) registerKV(serviceDesc string, serverData map[string]interface{}) error {
	if serverData == nil {
		serverData = make(map[string]interface{})
	}

	now := time.Now().Unix()
	kvMeta := KVServiceMeta{
		ServerName: r.serviceName,
		ServerDesc: serviceDesc,
		ServerData: serverData,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	data, err := json.Marshal(kvMeta)
	if err != nil {
		return fmt.Errorf("marshal KV meta failed: %v", err)
	}

	kvPair := &api.KVPair{
		Key:   r.kvPath,
		Value: data,
	}

	_, err = r.client.KV().Put(kvPair, nil)
	if err != nil {
		return fmt.Errorf("put KV failed: %v", err)
	}

	return nil
}

func (r *Registrar) hasActiveServices() (bool, error) {
	services, _, err := r.client.Health().Service(r.serviceName, "", true, nil)
	if err != nil {
		return false, err
	}

	activeCount := 0
	for _, service := range services {
		if service.Service.ID != r.serviceID {
			activeCount++
		}
	}

	return activeCount > 0, nil
}

func (r *Registrar) deleteKV() error {
	_, err := r.client.KV().Delete(r.kvPath, nil)
	return err
}

func parseListenOn(listenOn string) (string, int, error) {
	if listenOn == "" {
		return "0.0.0.0", 50051, nil
	}

	host, portStr, err := net.SplitHostPort(listenOn)
	if err != nil {
		if strings.Contains(err.Error(), "missing port") {
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

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "0.0.0.0", 50051, fmt.Errorf("invalid port %s: %v", portStr, err)
	}

	if host == "" || host == "0.0.0.0" || host == "[::]" || host == "[::1]" {
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
