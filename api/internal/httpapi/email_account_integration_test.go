package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/AlexQFMM2/mhed-platform/api/internal/config"
	"github.com/AlexQFMM2/mhed-platform/api/internal/database"
	"github.com/AlexQFMM2/mhed-platform/api/internal/mailservice"
)

type capturedEmail struct {
	To   string
	Data mailservice.TemplateData
}

func performJSON(handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeObject(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %d: %v: %s", response.Code, err, response.Body.String())
	}
	return value
}

func waitEmail(t *testing.T, values <-chan capturedEmail) capturedEmail {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("email outbox was not delivered")
		return capturedEmail{}
	}
}

func TestDesktopEmailRegistrationAndReset(t *testing.T) {
	databaseURL := os.Getenv("MHED_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MHED_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE email_outbox,email_verification_challenges,loadout_likes,loadout_skill_index,loadout_equipment_index,loadout_reports,loadouts,sessions,user_roles,users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `WITH administrator AS (
		INSERT INTO users(username,password_hash,must_change_password) VALUES('integration_admin','test-only',false) RETURNING id
	) INSERT INTO user_roles(user_id,role_id) SELECT administrator.id,roles.id FROM administrator,roles WHERE roles.key='super_admin'`); err != nil {
		t.Fatal(err)
	}

	emails := make(chan capturedEmail, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.URL.Path == "/balance" {
			_, _ = writer.Write([]byte(`{"code":"200","message":"ok","account":"99"}`))
			return
		}
		var data mailservice.TemplateData
		if err := json.Unmarshal([]byte(request.Form.Get("data")), &data); err != nil {
			t.Error(err)
		}
		emails <- capturedEmail{To: request.Form.Get("to"), Data: data}
		_, _ = writer.Write([]byte(`{"code":200,"message":"ok","msg_id":"test-message"}`))
	}))
	defer provider.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mail, err := mailservice.New(pool, logger, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	mail.WithProviderForTest(provider.Client(), provider.URL+"/send", provider.URL+"/balance")
	ciphertext, nonce, err := mail.Seal([]byte("test-api-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_provider_settings(id,enabled,api_key_ciphertext,api_key_nonce,template_id)
		VALUES(1,false,$1,$2,'template-1')
		ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,api_key_ciphertext=excluded.api_key_ciphertext,
		api_key_nonce=excluded.api_key_nonce,template_id=excluded.template_id`, ciphertext, nonce); err != nil {
		t.Fatal(err)
	}
	balance, err := mail.Balance(ctx)
	if err != nil || balance != "99" {
		t.Fatalf("disabled email settings could not query balance: balance=%q error=%v", balance, err)
	}
	if _, err := mail.SendTest(ctx, "admin@example.test"); err != nil {
		t.Fatalf("disabled email settings could not send test: %v", err)
	}
	testEmail := waitEmail(t, emails)
	if testEmail.To != "admin@example.test" || testEmail.Data.Code != "123456" {
		t.Fatalf("unexpected test email: %#v", testEmail)
	}
	var testStatus, testMessageID string
	if err := pool.QueryRow(ctx, `SELECT status,provider_message_id FROM email_outbox WHERE challenge_id IS NULL ORDER BY id DESC LIMIT 1`).Scan(&testStatus, &testMessageID); err != nil {
		t.Fatal(err)
	}
	if testStatus != "sent" || testMessageID != "test-message" {
		t.Fatalf("test email delivery was not recorded: status=%q message_id=%q", testStatus, testMessageID)
	}
	if _, err := pool.Exec(ctx, `UPDATE email_provider_settings SET enabled=true WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	go mail.Run(ctx)
	router := NewRouter(logger, pool, WithConfig(config.Config{Environment: "development", ReportHMACKey: "test", SecretEncryptionKey: []byte("0123456789abcdef0123456789abcdef")}), WithMailService(mail))

	registrationCode := performJSON(router, http.MethodPost, "/v1/desktop/auth/register/code", "", map[string]string{"username": "mail_hunter", "email": "hunter@example.test"})
	if registrationCode.Code != http.StatusAccepted {
		t.Fatalf("registration code returned %d: %s", registrationCode.Code, registrationCode.Body.String())
	}
	challengeID := decodeObject(t, registrationCode)["challenge_id"].(string)
	registrationEmail := waitEmail(t, emails)
	if registrationEmail.To != "hunter@example.test" || registrationEmail.Data.Username != "mail_hunter" || registrationEmail.Data.Code == "" {
		t.Fatalf("unexpected registration email: %#v", registrationEmail)
	}

	registration := performJSON(router, http.MethodPost, "/v1/desktop/auth/register", "", map[string]string{"challenge_id": challengeID, "code": registrationEmail.Data.Code, "password": "Hunter#2026"})
	if registration.Code != http.StatusCreated {
		t.Fatalf("registration returned %d: %s", registration.Code, registration.Body.String())
	}
	registered := decodeObject(t, registration)
	token := registered["access_token"].(string)
	profile := registered["user"].(map[string]any)
	if profile["email"] != "hunter@example.test" || profile["email_verified"] != true || profile["received_like_count"].(float64) != 0 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	var roleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE u.username='mail_hunter'`).Scan(&roleCount); err != nil || roleCount != 0 {
		t.Fatalf("registered user roles = %d, error=%v", roleCount, err)
	}

	emailLogin := performJSON(router, http.MethodPost, "/v1/desktop/auth/login", "", map[string]string{"account": "HUNTER@example.test", "password": "Hunter#2026"})
	if emailLogin.Code != http.StatusOK {
		t.Fatalf("email login returned %d: %s", emailLogin.Code, emailLogin.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE email_verification_challenges SET created_at=created_at-interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	resetCode := performJSON(router, http.MethodPost, "/v1/desktop/auth/password-reset/code", "", map[string]string{"account": "mail_hunter"})
	if resetCode.Code != http.StatusAccepted {
		t.Fatalf("reset code returned %d: %s", resetCode.Code, resetCode.Body.String())
	}
	resetChallenge := decodeObject(t, resetCode)["challenge_id"].(string)
	repeatedResetCode := performJSON(router, http.MethodPost, "/v1/desktop/auth/password-reset/code", "", map[string]string{"account": "mail_hunter"})
	unknownResetCode := performJSON(router, http.MethodPost, "/v1/desktop/auth/password-reset/code", "", map[string]string{"account": "missing_hunter"})
	if repeatedResetCode.Code != http.StatusAccepted || unknownResetCode.Code != http.StatusAccepted {
		t.Fatalf("password reset account enumeration protection failed: existing=%d unknown=%d", repeatedResetCode.Code, unknownResetCode.Code)
	}
	resetEmail := waitEmail(t, emails)
	reset := performJSON(router, http.MethodPost, "/v1/desktop/auth/password-reset", "", map[string]string{"challenge_id": resetChallenge, "code": resetEmail.Data.Code, "new_password": "NewPass#2026"})
	if reset.Code != http.StatusOK {
		t.Fatalf("reset returned %d: %s", reset.Code, reset.Body.String())
	}
	oldSession := performJSON(router, http.MethodGet, "/v1/desktop/me", token, nil)
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("old session survived reset: %d", oldSession.Code)
	}
	newLogin := performJSON(router, http.MethodPost, "/v1/desktop/auth/login", "", map[string]string{"account": "hunter@example.test", "password": "NewPass#2026"})
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login returned %d: %s", newLogin.Code, newLogin.Body.String())
	}
}
