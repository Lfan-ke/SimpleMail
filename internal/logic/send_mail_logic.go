package logic

import (
	"context"
	"fmt"
	"strings"

	"simplemail/internal/svc"
	"simplemail/mail"
	"simplemail/pkg/mailer"

	"github.com/zeromicro/go-zero/core/logx"
)

type SendMailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSendMailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SendMailLogic {
	return &SendMailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SendMailLogic) SendMail(in *mail.SendMailRequest) (*mail.SendMailResponse, error) {
	if err := l.validateRequest(in); err != nil {
		return &mail.SendMailResponse{
			Status:  400,
			Message: err.Error(),
			Data:    "",
		}, nil
	}

	from := in.From
	if from == "" && l.svcCtx.Config.Smtp.DefaultFrom != "" {
		from = l.svcCtx.Config.Smtp.DefaultFrom
	}
	if from == "" {
		from = l.svcCtx.Config.Smtp.Username
	}

	messageID, err := l.svcCtx.Mailer.Send(from, in.To, in.Cc, in.Bcc, in.Subject, in.Content)
	if err != nil {
		l.Logger.Errorf("Send mail failed: %v", err)
		return &mail.SendMailResponse{
			Status:  500,
			Message: fmt.Sprintf("Failed to send mail: %v", err),
			Data:    "",
		}, nil
	}

	l.Logger.Infof("Mail sent successfully. MessageID: %s, To: %v", messageID, in.To)

	return &mail.SendMailResponse{
		Status:  200,
		Message: "Mail sent successfully",
		Data:    messageID,
	}, nil
}

func (l *SendMailLogic) validateRequest(in *mail.SendMailRequest) error {
	if in.From != "" && !mailer.ValidateEmail(in.From) {
		return fmt.Errorf("invalid from email address: %s", in.From)
	}

	if len(in.To) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	for _, email := range in.To {
		if !mailer.ValidateEmail(email) {
			return fmt.Errorf("invalid to email address: %s", email)
		}
	}

	for _, email := range in.Cc {
		if !mailer.ValidateEmail(email) {
			return fmt.Errorf("invalid cc email address: %s", email)
		}
	}

	for _, email := range in.Bcc {
		if !mailer.ValidateEmail(email) {
			return fmt.Errorf("invalid bcc email address: %s", email)
		}
	}

	if strings.TrimSpace(in.Subject) == "" {
		return fmt.Errorf("subject is required")
	}

	if strings.TrimSpace(in.Content) == "" {
		return fmt.Errorf("content is required")
	}

	return nil
}
