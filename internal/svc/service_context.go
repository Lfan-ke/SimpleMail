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
	mailerInstance := mailer.NewMailer(
		c.Smtp.Host,
		c.Smtp.Port,
		c.Smtp.Username,
		c.Smtp.Password,
		c.Smtp.FromName,
		c.Smtp.SSL,
	)

	var consulReg *consul.Registrar
	if c.Consul.Host != "" && c.Consul.Host != "127.0.0.1:8500" {
		var err error
		consulReg, err = consul.NewRegistrar(c.Consul.Host, c.Consul.Token)
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
