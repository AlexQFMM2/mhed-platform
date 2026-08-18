package httpapi

import (
	"errors"
	"net/http"
	mailaddr "net/mail"
	"strings"
	"time"

	"github.com/AlexQFMM2/mhed-platform/api/internal/mailservice"
	"github.com/jackc/pgx/v5"
)

type emailDeliveryView struct {
	ID                int64   `json:"id"`
	Purpose           string  `json:"purpose"`
	Recipient         string  `json:"recipient"`
	Status            string  `json:"status"`
	AttemptCount      int     `json:"attempt_count"`
	ProviderMessageID string  `json:"provider_message_id"`
	LastErrorCode     string  `json:"last_error_code"`
	CreatedAt         string  `json:"created_at"`
	SentAt            *string `json:"sent_at"`
}

func (server *apiServer) emailDeliveries(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.pool.Query(request.Context(), `SELECT o.id,coalesce(c.purpose,'test'),o.recipient,o.status,o.attempt_count,
		o.provider_message_id,o.last_error_code,o.created_at,o.sent_at
		FROM email_outbox o LEFT JOIN email_verification_challenges c ON c.id=o.challenge_id
		ORDER BY o.id DESC LIMIT 100`)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	items := make([]emailDeliveryView, 0)
	for rows.Next() {
		var item emailDeliveryView
		var createdAt time.Time
		var sentAt *time.Time
		if err := rows.Scan(&item.ID, &item.Purpose, &item.Recipient, &item.Status, &item.AttemptCount,
			&item.ProviderMessageID, &item.LastErrorCode, &createdAt, &sentAt); err != nil {
			serverError(writer, request, err)
			return
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if sentAt != nil {
			value := sentAt.UTC().Format(time.RFC3339)
			item.SentAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

func validEmail(value string) bool {
	if len(value) < 3 || len(value) > 320 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, err := mailaddr.ParseAddress(value)
	return err == nil && strings.EqualFold(parsed.Address, value)
}

func (server *apiServer) emailSettings(writer http.ResponseWriter, request *http.Request) {
	if server.mail == nil {
		writeError(writer, request, http.StatusServiceUnavailable, "EMAIL_NOT_CONFIGURED", "邮件服务未初始化。")
		return
	}
	settings, err := server.mail.GetSettings(request.Context())
	if err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"provider": "aoksend", "enabled": settings.Enabled, "has_api_key": settings.HasAPIKey,
		"template_id": settings.TemplateID, "sender_alias": settings.SenderAlias, "reply_to": settings.ReplyTo,
		"updated_at": settings.UpdatedAt,
		"send_api":   mailservice.SendAPIURL, "balance_api": mailservice.BalanceAPIURL,
		"send_documentation":    "https://www.aoksend.com/api.html",
		"balance_documentation": "https://www.aoksend.com/check_balance_api.html",
		"template_variables":    []string{"code", "username", "userinfo"},
	})
}

func (server *apiServer) updateEmailSettings(writer http.ResponseWriter, request *http.Request) {
	if server.mail == nil {
		writeError(writer, request, http.StatusServiceUnavailable, "EMAIL_NOT_CONFIGURED", "邮件服务未初始化。")
		return
	}
	var input struct {
		Enabled     bool   `json:"enabled"`
		APIKey      string `json:"api_key"`
		TemplateID  string `json:"template_id"`
		SenderAlias string `json:"sender_alias"`
		ReplyTo     string `json:"reply_to"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.SenderAlias = strings.TrimSpace(input.SenderAlias)
	input.ReplyTo = strings.TrimSpace(input.ReplyTo)
	current, err := server.mail.GetSettings(request.Context())
	if err != nil {
		serverError(writer, request, err)
		return
	}
	if len(input.TemplateID) > 80 || len([]rune(input.SenderAlias)) > 80 || (input.ReplyTo != "" && !validEmail(input.ReplyTo)) {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "模板 ID、发件人名称或回复邮箱格式不正确。")
		return
	}
	if input.SenderAlias == "" {
		input.SenderAlias = "MHED"
	}
	if input.Enabled && (input.TemplateID == "" || (!current.HasAPIKey && input.APIKey == "")) {
		writeError(writer, request, http.StatusBadRequest, "EMAIL_NOT_CONFIGURED", "启用邮件前必须配置 API 密钥和模板 ID。")
		return
	}
	ciphertext, nonce := current.APICiphertext, current.APINonce
	if input.APIKey != "" {
		ciphertext, nonce, err = server.mail.Seal([]byte(input.APIKey))
		if err != nil {
			serverError(writer, request, err)
			return
		}
	}
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE email_provider_settings SET enabled=$1,api_key_ciphertext=$2,api_key_nonce=$3,template_id=$4,sender_alias=$5,reply_to=$6,updated_by=$7,updated_at=now() WHERE id=1`, input.Enabled, ciphertext, nonce, input.TemplateID, input.SenderAlias, input.ReplyTo, actor.ID)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "email.settings_update", "email_provider", "aoksend", map[string]any{"enabled": input.Enabled, "api_key_replaced": input.APIKey != "", "template_id": input.TemplateID}, requestID(request))
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	if err != nil {
		serverError(writer, request, err)
		return
	}
	server.emailSettings(writer, request)
}

func (server *apiServer) auditEmailAction(request *http.Request, action string) error {
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		err = server.audit(request.Context(), tx, actor, action, "email_provider", "aoksend", map[string]any{}, requestID(request))
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	return err
}

func writeMailError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, mailservice.ErrNotConfigured):
		writeError(writer, request, http.StatusServiceUnavailable, "EMAIL_NOT_CONFIGURED", "邮件服务尚未启用或配置不完整。")
	case errors.Is(err, mailservice.ErrRateLimited):
		writeError(writer, request, http.StatusTooManyRequests, "EMAIL_RATE_LIMITED", "验证码发送过于频繁，请稍后再试。")
	case errors.Is(err, mailservice.ErrExpiredCode):
		writeError(writer, request, http.StatusBadRequest, "VERIFICATION_CODE_EXPIRED", "验证码尚未发送成功或已经失效。")
	case errors.Is(err, mailservice.ErrInvalidCode), errors.Is(err, pgx.ErrNoRows):
		writeError(writer, request, http.StatusBadRequest, "VERIFICATION_CODE_INVALID", "验证码错误或已使用。")
	default:
		writeError(writer, request, http.StatusBadGateway, "EMAIL_SEND_FAILED", "邮件服务暂时不可用，请稍后再试。")
	}
}

func (server *apiServer) checkEmailBalance(writer http.ResponseWriter, request *http.Request) {
	if server.mail == nil {
		writeMailError(writer, request, mailservice.ErrNotConfigured)
		return
	}
	balance, err := server.mail.Balance(request.Context())
	if err != nil {
		writeMailError(writer, request, err)
		return
	}
	if err := server.auditEmailAction(request, "email.balance_check"); err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"balance": balance})
}

func (server *apiServer) testEmail(writer http.ResponseWriter, request *http.Request) {
	if server.mail == nil {
		writeMailError(writer, request, mailservice.ErrNotConfigured)
		return
	}
	var input struct {
		To string `json:"to"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	input.To = strings.TrimSpace(input.To)
	if !validEmail(input.To) {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "测试收件邮箱格式不正确。")
		return
	}
	messageID, err := server.mail.SendTest(request.Context(), input.To)
	if err != nil {
		writeMailError(writer, request, err)
		return
	}
	if err := server.auditEmailAction(request, "email.test_send"); err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "sent", "message_id": messageID})
}
