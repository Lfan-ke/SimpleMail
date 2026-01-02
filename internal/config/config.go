package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	Consul ConsulConfig `json:",optional" yaml:",omitempty"`
	Smtp   SmtpConfig   `json:"Smtp" yaml:"Smtp"`
}

type ConsulConfig struct {
	Host                string `json:"Host,default=127.0.0.1:8500" yaml:"Host,omitempty"`
	Token               string `json:",optional" yaml:"Token,omitempty"`
	Scheme              string `json:"Scheme,default=http" yaml:"Scheme,omitempty"`
	HealthCheckInterval string `json:"HealthCheckInterval,default=10s" yaml:"HealthCheckInterval,omitempty"`
	HealthCheckTimeout  string `json:"HealthCheckTimeout,default=5s" yaml:"HealthCheckTimeout,omitempty"`
}

type SmtpConfig struct {
	Host        string `json:"Host,default=smtp.qq.com" yaml:"Host"`
	Port        int    `json:"Port,default=465" yaml:"Port"`
	FromName    string `json:"FromName,default=SimpleMail" yaml:"FromName,omitempty"`
	Username    string `json:"Username" yaml:"Username"`
	Password    string `json:"Password" yaml:"Password"`
	SSL         bool   `json:"SSL,default=true" yaml:"SSL,omitempty"`
	DefaultFrom string `json:"DefaultFrom,optional" yaml:"DefaultFrom,omitempty"`
}
