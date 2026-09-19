package server

import (
	"context"

	emailinfra "servify/apps/server/internal/modules/email/infra"
	satisfapp "servify/apps/server/internal/modules/satisfaction/application"
)

// surveyEmailMailer 将 email 渠道的 SMTP sender 适配为满意度模块的
// SurveyMailer 窄接口（纯文本 RFC 5322 报文，复用 ComposeTextMessage 编码）。
type surveyEmailMailer struct {
	sender emailinfra.SMTPSender
	from   string
}

var _ satisfapp.SurveyMailer = (*surveyEmailMailer)(nil)

func (m *surveyEmailMailer) SendSurveyEmail(ctx context.Context, to, subject, textBody string) error {
	msg, err := emailinfra.ComposeTextMessage(m.from, to, subject, textBody, "")
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, m.from, []string{to}, msg)
}
