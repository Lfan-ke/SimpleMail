package mailer

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

type Mailer struct {
	host     string
	port     int
	username string
	password string
	fromName string
	ssl      bool
}

func NewMailer(host string, port int, username, password, fromName string, ssl bool) *Mailer {
	return &Mailer{
		host:     host,
		port:     port,
		username: username,
		password: password,
		fromName: fromName,
		ssl:      ssl,
	}
}

// Send 发送邮件
func (m *Mailer) Send(from string, to, cc, bcc []string, subject, content string) (string, error) {
	// 如果没有指定发件人，使用默认用户名
	if from == "" {
		from = m.username
	}

	// 准备邮件内容
	message := m.buildMessage(from, to, cc, bcc, subject, content)
	allRecipients := m.getAllRecipients(to, cc, bcc)

	// 发送邮件
	if m.ssl {
		return m.sendWithSSL(message, from, allRecipients)
	}
	return m.sendWithoutSSL(message, from, allRecipients)
}

func (m *Mailer) buildMessage(from string, to, cc, bcc []string, subject, content string) []byte {
	var builder strings.Builder

	// 邮件头
	builder.WriteString(fmt.Sprintf("From: %s <%s>\r\n", m.fromName, from))
	builder.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))

	if len(cc) > 0 {
		builder.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(cc, ", ")))
	}

	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	builder.WriteString("\r\n")

	// 邮件内容
	builder.WriteString(content)

	return []byte(builder.String())
}

func (m *Mailer) getAllRecipients(to, cc, bcc []string) []string {
	recipients := make([]string, 0, len(to)+len(cc)+len(bcc))
	recipients = append(recipients, to...)
	recipients = append(recipients, cc...)
	recipients = append(recipients, bcc...)
	return recipients
}

func (m *Mailer) sendWithSSL(message []byte, from string, recipients []string) (string, error) {
	// TLS配置
	tlsConfig := &tls.Config{
		ServerName:         m.host,
		InsecureSkipVerify: false,
	}

	// 建立TLS连接
	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", m.host, m.port), tlsConfig)
	if err != nil {
		return "", fmt.Errorf("dial tls failed: %v", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return "", fmt.Errorf("create smtp client failed: %v", err)
	}
	defer client.Close()

	// 认证
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	if err = client.Auth(auth); err != nil {
		return "", fmt.Errorf("auth failed: %v", err)
	}

	// 设置发件人
	if err = client.Mail(from); err != nil {
		return "", fmt.Errorf("set mail from failed: %v", err)
	}

	// 添加收件人
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			return "", fmt.Errorf("add recipient %s failed: %v", recipient, err)
		}
	}

	// 发送邮件内容
	w, err := client.Data()
	if err != nil {
		return "", fmt.Errorf("get data writer failed: %v", err)
	}
	defer w.Close()

	if _, err = w.Write(message); err != nil {
		return "", fmt.Errorf("write message failed: %v", err)
	}

	// 生成消息ID
	msgID := fmt.Sprintf("<%d@simplemail>", time.Now().UnixNano())
	return msgID, nil
}

func (m *Mailer) sendWithoutSSL(message []byte, from string, recipients []string) (string, error) {
	auth := smtp.PlainAuth("", m.username, m.password, m.host)

	err := smtp.SendMail(
		fmt.Sprintf("%s:%d", m.host, m.port),
		auth,
		from,
		recipients,
		message,
	)

	if err != nil {
		return "", fmt.Errorf("send mail failed: %v", err)
	}

	msgID := fmt.Sprintf("<%d@simplemail>", time.Now().UnixNano())
	return msgID, nil
}

// ValidateEmail 验证邮箱格式
func ValidateEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".")
}
