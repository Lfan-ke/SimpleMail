package svc

import (
	"log"

	"simplemail/consul"
	"simplemail/internal/config"
	"simplemail/pkg/mailer"
)

type ServiceContext struct {
	Config *config.Config
	Mailer *mailer.Mailer
	Consul *consul.Registrar
}

func NewServiceContext(c *config.Config) *ServiceContext {
	// 创建邮件发送器
	mailerInstance := mailer.NewMailer(
		c.Smtp.Host,
		c.Smtp.Port,
		c.Smtp.Username,
		c.Smtp.Password,
		c.Smtp.FromName,
		c.Smtp.SSL,
	)

	// 创建Consul注册器
	var consulReg *consul.Registrar
	if c.Consul.Host != "" && c.Consul.Host != "127.0.0.1:8500" { // 避免默认值
		var err error
		consulReg, err = consul.NewRegistrar(c.Consul.Host)
		if err != nil {
			log.Printf("Warning: Failed to create consul client: %v", err)
		}
	}

	return &ServiceContext{
		Config: c,
		Mailer: mailerInstance,
		Consul: consulReg,
	}
}
