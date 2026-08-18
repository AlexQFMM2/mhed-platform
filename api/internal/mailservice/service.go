package mailservice

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	SendAPIURL    = "https://apiv2.aoksend.com/index/api/send_email"
	BalanceAPIURL = "https://apiv2.aoksend.com/index/api/check_account"
)

var (
	ErrNotConfigured = errors.New("email service is not configured")
	ErrRateLimited   = errors.New("email verification rate limited")
	ErrInvalidCode   = errors.New("verification code is invalid")
	ErrExpiredCode   = errors.New("verification code is expired")
)

type Service struct {
	pool       *pgxpool.Pool
	logger     *slog.Logger
	client     *http.Client
	masterKey  []byte
	sendURL    string
	balanceURL string
}

type Settings struct {
	Enabled       bool
	HasAPIKey     bool
	TemplateID    string
	SenderAlias   string
	ReplyTo       string
	UpdatedAt     string
	APICiphertext []byte
	APINonce      []byte
}

type TemplateData struct {
	Code     string `json:"code"`
	Username string `json:"username"`
	UserInfo string `json:"userinfo"`
}

type Challenge struct {
	ID       string
	Purpose  string
	UserID   *string
	Username string
	Email    string
}

type providerResponse struct {
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
	MsgID   string          `json:"msg_id"`
	Account json.RawMessage `json:"account"`
}

type ProviderError struct {
	Code      int
	Permanent bool
}

func (value *ProviderError) Error() string {
	if value.Code != 0 {
		return fmt.Sprintf("aoksend rejected request with code %d", value.Code)
	}
	return "aoksend rejected request"
}

func New(pool *pgxpool.Pool, logger *slog.Logger, masterKey []byte) (*Service, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("mail encryption master key must contain exactly 32 bytes")
	}
	return &Service{
		pool: pool, logger: logger, masterKey: append([]byte(nil), masterKey...),
		client: &http.Client{Timeout: 10 * time.Second}, sendURL: SendAPIURL, balanceURL: BalanceAPIURL,
	}, nil
}

func (service *Service) WithProviderForTest(client *http.Client, sendURL, balanceURL string) {
	if client != nil {
		service.client = client
	}
	if sendURL != "" {
		service.sendURL = sendURL
	}
	if balanceURL != "" {
		service.balanceURL = balanceURL
	}
}

func (service *Service) derivedKey(label string) []byte {
	mac := hmac.New(sha256.New, service.masterKey)
	mac.Write([]byte("mhed/" + label + "/v1"))
	return mac.Sum(nil)
}

func (service *Service) Seal(plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(service.derivedKey("email-encryption"))
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, []byte("mhed-email-v1")), nonce, nil
}

func (service *Service) Open(ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(service.derivedKey("email-encryption"))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, []byte("mhed-email-v1"))
}

func (service *Service) SourceDigest(source string) []byte {
	mac := hmac.New(sha256.New, service.derivedKey("verification-source"))
	mac.Write([]byte(source))
	return mac.Sum(nil)
}

func (service *Service) codeDigest(id, code string) []byte {
	mac := hmac.New(sha256.New, service.derivedKey("verification-code"))
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	mac.Write([]byte(code))
	return mac.Sum(nil)
}

func RandomCode() (string, error) {
	value := make([]byte, 4)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	number := (uint32(value[0])<<24 | uint32(value[1])<<16 | uint32(value[2])<<8 | uint32(value[3])) % 1000000
	return fmt.Sprintf("%06d", number), nil
}

func (service *Service) GetSettings(ctx context.Context) (Settings, error) {
	var value Settings
	var updated time.Time
	err := service.pool.QueryRow(ctx, `SELECT enabled,api_key_ciphertext IS NOT NULL,template_id,sender_alias,reply_to,api_key_ciphertext,api_key_nonce,updated_at FROM email_provider_settings WHERE id=1`).
		Scan(&value.Enabled, &value.HasAPIKey, &value.TemplateID, &value.SenderAlias, &value.ReplyTo, &value.APICiphertext, &value.APINonce, &updated)
	value.UpdatedAt = updated.UTC().Format(time.RFC3339)
	return value, err
}

func (service *Service) providerCredentials(ctx context.Context, requireEnabled, requireTemplate bool) (Settings, string, error) {
	settings, err := service.GetSettings(ctx)
	if err != nil {
		return Settings{}, "", err
	}
	if (requireEnabled && !settings.Enabled) || !settings.HasAPIKey ||
		(requireTemplate && strings.TrimSpace(settings.TemplateID) == "") {
		return Settings{}, "", ErrNotConfigured
	}
	plaintext, err := service.Open(settings.APICiphertext, settings.APINonce)
	if err != nil {
		return Settings{}, "", err
	}
	return settings, string(plaintext), nil
}

func responseCode(raw json.RawMessage) int {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	value, _ := strconv.Atoi(text)
	return value
}

func (service *Service) postForm(ctx context.Context, endpoint string, form url.Values) (providerResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return providerResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "MHED-Platform/1")
	response, err := service.client.Do(request)
	if err != nil {
		return providerResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerResponse{}, &ProviderError{Code: response.StatusCode, Permanent: response.StatusCode >= 400 && response.StatusCode < 500}
	}
	var value providerResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&value); err != nil {
		return providerResponse{}, err
	}
	if responseCode(value.Code) != 200 {
		return value, &ProviderError{Code: responseCode(value.Code), Permanent: responseCode(value.Code) >= 40000}
	}
	return value, nil
}

func (service *Service) send(ctx context.Context, to string, data TemplateData, requireEnabled bool) (string, error) {
	settings, apiKey, err := service.providerCredentials(ctx, requireEnabled, true)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	form := url.Values{"app_key": {apiKey}, "template_id": {settings.TemplateID}, "to": {to}, "data": {string(encoded)}}
	if settings.SenderAlias != "" {
		form.Set("alias", settings.SenderAlias)
	}
	if settings.ReplyTo != "" {
		form.Set("reply_to", settings.ReplyTo)
	}
	response, err := service.postForm(ctx, service.sendURL, form)
	return response.MsgID, err
}

func (service *Service) Balance(ctx context.Context) (string, error) {
	_, apiKey, err := service.providerCredentials(ctx, false, false)
	if err != nil {
		return "", err
	}
	response, err := service.postForm(ctx, service.balanceURL, url.Values{"app_key": {apiKey}})
	if err != nil {
		return "", err
	}
	return strings.Trim(strings.TrimSpace(string(response.Account)), `"`), nil
}

func (service *Service) SendTest(ctx context.Context, to string) (string, error) {
	messageID, err := service.send(ctx, to, TemplateData{Code: "123456", Username: "MHED测试", UserInfo: "MHED 邮件配置测试，本验证码无需使用"}, false)
	status := "sent"
	errorCode := ""
	sentAt := any(time.Now())
	if err != nil {
		status = "failed"
		sentAt = nil
		errorCode = "DELIVERY_FAILED"
		var providerErr *ProviderError
		if errors.As(err, &providerErr) && providerErr.Code != 0 {
			errorCode = fmt.Sprintf("AOKSEND_%d", providerErr.Code)
		} else if errors.Is(err, ErrNotConfigured) {
			errorCode = "EMAIL_NOT_CONFIGURED"
		}
	}
	if _, recordErr := service.pool.Exec(ctx, `INSERT INTO email_outbox(recipient,status,attempt_count,provider_message_id,last_error_code,sent_at,payload_ciphertext,payload_nonce)
		VALUES($1,$2,1,$3,$4,$5,NULL,NULL)`, to, status, messageID, errorCode, sentAt); recordErr != nil {
		service.logger.Warn("test email delivery record was not saved", "error", recordErr)
	}
	return messageID, err
}

func (service *Service) Enqueue(ctx context.Context, purpose string, userID *string, username, email, code, userInfo string, sourceDigest []byte) (string, error) {
	if _, _, err := service.providerCredentials(ctx, true, true); err != nil {
		return "", err
	}
	id := uuid.NewString()
	payload, err := json.Marshal(TemplateData{Code: code, Username: username, UserInfo: userInfo})
	if err != nil {
		return "", err
	}
	ciphertext, nonce, err := service.Seal(payload)
	if err != nil {
		return "", err
	}
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(lower($1::text),0))`, email); err != nil {
		return "", err
	}
	var recent, hourlyEmail, hourlySource int
	err = tx.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE created_at>now()-interval '60 seconds'),
		count(*) FILTER (WHERE created_at>now()-interval '1 hour')
		FROM email_verification_challenges WHERE lower(email)=lower($1)`, email).Scan(&recent, &hourlyEmail)
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM email_verification_challenges WHERE source_digest=$1 AND created_at>now()-interval '1 hour'`, sourceDigest).Scan(&hourlySource)
	}
	if err != nil {
		return "", err
	}
	if recent > 0 || hourlyEmail >= 5 || hourlySource >= 10 {
		return "", ErrRateLimited
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_verification_challenges(id,purpose,user_id,username,email,code_digest,source_digest)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, id, purpose, userID, username, email, service.codeDigest(id, code), sourceDigest)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO email_outbox(challenge_id,recipient,payload_ciphertext,payload_nonce) VALUES($1,$2,$3,$4)`, id, email, ciphertext, nonce)
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (service *Service) Verify(ctx context.Context, id, purpose, code string, expectedUserID *string) (Challenge, error) {
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		return Challenge{}, err
	}
	defer tx.Rollback(ctx)
	var challenge Challenge
	var digest []byte
	var status string
	var attempts int
	var expiresAt *time.Time
	err = tx.QueryRow(ctx, `SELECT id,purpose,user_id,username,email,code_digest,status,attempt_count,expires_at
		FROM email_verification_challenges WHERE id=$1 FOR UPDATE`, id).
		Scan(&challenge.ID, &challenge.Purpose, &challenge.UserID, &challenge.Username, &challenge.Email, &digest, &status, &attempts, &expiresAt)
	if err != nil || challenge.Purpose != purpose || (expectedUserID != nil && (challenge.UserID == nil || *challenge.UserID != *expectedUserID)) {
		return Challenge{}, ErrInvalidCode
	}
	if status != "sent" || expiresAt == nil || time.Now().After(*expiresAt) {
		return Challenge{}, ErrExpiredCode
	}
	if attempts >= 5 || subtle.ConstantTimeCompare(digest, service.codeDigest(id, code)) != 1 {
		attempts++
		newStatus := "sent"
		if attempts >= 5 {
			newStatus = "failed"
		}
		_, _ = tx.Exec(ctx, `UPDATE email_verification_challenges SET attempt_count=$1,status=$2 WHERE id=$3`, attempts, newStatus, id)
		_ = tx.Commit(ctx)
		return Challenge{}, ErrInvalidCode
	}
	_, err = tx.Exec(ctx, `UPDATE email_verification_challenges SET status='used',consumed_at=now() WHERE id=$1`, id)
	if err != nil {
		return Challenge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Challenge{}, err
	}
	return challenge, nil
}

func (service *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := service.deliverOne(ctx); err != nil && !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, context.Canceled) {
			service.logger.Warn("email outbox delivery failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (service *Service) deliverOne(ctx context.Context) error {
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	var challengeID, recipient string
	var ciphertext, nonce []byte
	var attempts int
	err = tx.QueryRow(ctx, `SELECT id,challenge_id,recipient,payload_ciphertext,payload_nonce,attempt_count
		FROM email_outbox WHERE status IN ('queued','sending') AND next_attempt_at<=now()
		ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &challengeID, &recipient, &ciphertext, &nonce, &attempts)
	if err != nil {
		return err
	}
	attempts++
	if _, err = tx.Exec(ctx, `UPDATE email_outbox SET status='sending',attempt_count=$1,
		next_attempt_at=now()+interval '1 minute',updated_at=now() WHERE id=$2`, attempts, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	payload, err := service.Open(ciphertext, nonce)
	var data TemplateData
	if err == nil {
		err = json.Unmarshal(payload, &data)
	}
	var messageID string
	if err == nil {
		messageID, err = service.send(ctx, recipient, data, true)
	}
	if err == nil {
		batch := &pgx.Batch{}
		batch.Queue(`UPDATE email_outbox SET status='sent',provider_message_id=$1,payload_ciphertext=NULL,payload_nonce=NULL,sent_at=now(),updated_at=now() WHERE id=$2`, messageID, id)
		batch.Queue(`UPDATE email_verification_challenges SET status='sent',sent_at=now(),expires_at=now()+interval '10 minutes' WHERE id=$1 AND status='queued'`, challengeID)
		results := service.pool.SendBatch(ctx, batch)
		defer results.Close()
		if _, batchErr := results.Exec(); batchErr != nil {
			return batchErr
		}
		_, batchErr := results.Exec()
		return batchErr
	}
	var providerErr *ProviderError
	if attempts >= 4 || errors.Is(err, ErrNotConfigured) || (errors.As(err, &providerErr) && providerErr.Permanent) {
		_, updateErr := service.pool.Exec(ctx, `UPDATE email_outbox SET status='failed',last_error_code='DELIVERY_FAILED',payload_ciphertext=NULL,payload_nonce=NULL,updated_at=now() WHERE id=$1`, id)
		_, _ = service.pool.Exec(ctx, `UPDATE email_verification_challenges SET status='failed' WHERE id=$1 AND status='queued'`, challengeID)
		return updateErr
	}
	delay := time.Duration(1<<uint(attempts-1)) * 15 * time.Second
	_, updateErr := service.pool.Exec(ctx, `UPDATE email_outbox SET status='queued',last_error_code='DELIVERY_RETRY',next_attempt_at=$1,updated_at=now() WHERE id=$2`, time.Now().Add(delay), id)
	return updateErr
}
