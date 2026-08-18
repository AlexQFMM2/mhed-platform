package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AlexQFMM2/mhed-platform/api/internal/auth"
	"github.com/AlexQFMM2/mhed-platform/api/internal/mailservice"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func normalizedEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func (server *apiServer) mailReady(writer http.ResponseWriter, request *http.Request) bool {
	if server.mail == nil {
		writeMailError(writer, request, mailservice.ErrNotConfigured)
		return false
	}
	settings, err := server.mail.GetSettings(request.Context())
	if err != nil || !settings.Enabled || !settings.HasAPIKey || settings.TemplateID == "" {
		writeMailError(writer, request, mailservice.ErrNotConfigured)
		return false
	}
	return true
}

func challengeResponse(writer http.ResponseWriter, id string) {
	writeJSON(writer, http.StatusAccepted, map[string]any{"challenge_id": id, "resend_after_seconds": 60, "expires_in_seconds": 600})
}

func (server *apiServer) enqueueCode(writer http.ResponseWriter, request *http.Request, purpose string, userID *string, username, email, userInfo string) {
	code, err := mailservice.RandomCode()
	if err != nil {
		serverError(writer, request, err)
		return
	}
	id, err := server.mail.Enqueue(request.Context(), purpose, userID, username, email, code, userInfo, server.mail.SourceDigest(remoteAddress(request)))
	if err != nil {
		writeMailError(writer, request, err)
		return
	}
	challengeResponse(writer, id)
}

func (server *apiServer) registerCode(writer http.ResponseWriter, request *http.Request) {
	if !server.mailReady(writer, request) {
		return
	}
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	input.Email = normalizedEmail(input.Email)
	if !validUsername(input.Username) || !validEmail(input.Email) {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "用户名或邮箱格式不正确。")
		return
	}
	var usernameTaken, emailTaken bool
	err := server.pool.QueryRow(request.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username)=lower($1)),EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($2) AND status<>'deleted')`, input.Username, input.Email).Scan(&usernameTaken, &emailTaken)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	if usernameTaken {
		writeError(writer, request, http.StatusConflict, "USERNAME_TAKEN", "该用户名已被使用。")
		return
	}
	if emailTaken {
		writeError(writer, request, http.StatusConflict, "EMAIL_TAKEN", "该邮箱已被使用。")
		return
	}
	server.enqueueCode(writer, request, "register", nil, input.Username, input.Email, "MHED 注册验证，10分钟内有效")
}

func (server *apiServer) issueDesktopSession(request *http.Request, user sessionUser) (string, time.Time, error) {
	token, err := auth.RandomSecret(32)
	if err != nil {
		return "", time.Time{}, err
	}
	csrf, err := auth.RandomSecret(24)
	if err != nil {
		return "", time.Time{}, err
	}
	absolute := time.Now().Add(24 * time.Hour)
	_, err = server.pool.Exec(request.Context(), `INSERT INTO sessions(user_id,token_hash,csrf_hash,idle_expires_at,absolute_expires_at,user_agent,client_type) VALUES($1,$2,$3,now()+interval '8 hours',$4,$5,'desktop')`, user.ID, hashSecret(token), hashSecret(csrf), absolute, truncate(request.UserAgent(), 300))
	return token, absolute, err
}

func (server *apiServer) registerDesktop(writer http.ResponseWriter, request *http.Request) {
	if !server.mailReady(writer, request) {
		return
	}
	var input struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
		Password    string `json:"password"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", passwordPolicyMessage)
		return
	}
	challenge, err := server.mail.Verify(request.Context(), input.ChallengeID, "register", strings.TrimSpace(input.Code), nil)
	if err != nil {
		writeMailError(writer, request, err)
		return
	}
	var user sessionUser
	err = server.pool.QueryRow(request.Context(), `INSERT INTO users(username,email,email_verified_at,password_hash,must_change_password)
		VALUES($1,$2,now(),$3,false) RETURNING id,public_id,nickname,username,status,must_change_password`, challenge.Username, normalizedEmail(challenge.Email), hash).
		Scan(&user.ID, &user.PublicID, &user.Nickname, &user.Username, &user.Status, &user.MustChangePassword)
	if err != nil {
		if strings.Contains(err.Error(), "users_username_unique_ci") {
			writeError(writer, request, http.StatusConflict, "USERNAME_TAKEN", "该用户名已被使用。")
			return
		}
		if strings.Contains(err.Error(), "users_email_unique_ci") {
			writeError(writer, request, http.StatusConflict, "EMAIL_TAKEN", "该邮箱已被使用。")
			return
		}
		databaseError(writer, request, err)
		return
	}
	token, expires, err := server.issueDesktopSession(request, user)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	_, _ = server.pool.Exec(request.Context(), `UPDATE users SET last_login_at=now(),updated_at=now() WHERE id=$1`, user.ID)
	profile, err := server.profileFor(request.Context(), user)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"access_token": token, "expires_at": expires.UTC().Format(time.RFC3339), "user": profile})
}

func (server *apiServer) resetPasswordCode(writer http.ResponseWriter, request *http.Request) {
	if !server.mailReady(writer, request) {
		return
	}
	var input struct {
		Account string `json:"account"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	account := strings.TrimSpace(input.Account)
	var id, username, email string
	err := server.pool.QueryRow(request.Context(), `SELECT id,username,email FROM users WHERE status='active' AND email_verified_at IS NOT NULL AND (lower(username)=lower($1) OR lower(email)=lower($1))`, account).Scan(&id, &username, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		challengeResponse(writer, uuid.NewString())
		return
	}
	if err != nil {
		serverError(writer, request, err)
		return
	}
	code, err := mailservice.RandomCode()
	if err == nil {
		var challengeID string
		challengeID, err = server.mail.Enqueue(request.Context(), "reset_password", &id, username, normalizedEmail(email), code,
			"MHED 重置密码验证，10分钟内有效", server.mail.SourceDigest(remoteAddress(request)))
		if err == nil {
			challengeResponse(writer, challengeID)
			return
		}
	}
	server.logger.Warn("password reset code was not queued", "request_id", requestID(request), "error", err)
	challengeResponse(writer, uuid.NewString())
}

func (server *apiServer) resetPassword(writer http.ResponseWriter, request *http.Request) {
	if !server.mailReady(writer, request) {
		return
	}
	var input struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	hash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", passwordPolicyMessage)
		return
	}
	challenge, err := server.mail.Verify(request.Context(), input.ChallengeID, "reset_password", strings.TrimSpace(input.Code), nil)
	if err != nil || challenge.UserID == nil {
		if err == nil {
			err = mailservice.ErrInvalidCode
		}
		writeMailError(writer, request, err)
		return
	}
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE users SET password_hash=$1,must_change_password=false,updated_at=now() WHERE id=$2 AND status='active'`, hash, *challenge.UserID)
	}
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, *challenge.UserID)
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
	writeJSON(writer, http.StatusOK, map[string]string{"status": "password_reset", "message": "密码已重置，请重新登录。"})
}

func (server *apiServer) bindEmailCode(writer http.ResponseWriter, request *http.Request) {
	if !server.mailReady(writer, request) {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	input.Email = normalizedEmail(input.Email)
	if !validEmail(input.Email) {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "邮箱格式不正确。")
		return
	}
	user := currentUser(request)
	var taken bool
	err := server.pool.QueryRow(request.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1) AND status<>'deleted' AND id<>$2)`, input.Email, user.ID).Scan(&taken)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	if taken {
		writeError(writer, request, http.StatusConflict, "EMAIL_TAKEN", "该邮箱已被使用。")
		return
	}
	server.enqueueCode(writer, request, "bind_email", &user.ID, user.Username, input.Email, "MHED 邮箱验证，10分钟内有效")
}

func (server *apiServer) bindEmail(writer http.ResponseWriter, request *http.Request) {
	if !server.mailReady(writer, request) {
		return
	}
	var input struct {
		ChallengeID     string `json:"challenge_id"`
		Code            string `json:"code"`
		CurrentPassword string `json:"current_password"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	user := currentUser(request)
	var passwordHash string
	if err := server.pool.QueryRow(request.Context(), `SELECT password_hash FROM users WHERE id=$1`, user.ID).Scan(&passwordHash); err != nil {
		serverError(writer, request, err)
		return
	}
	if !auth.VerifyPassword(passwordHash, input.CurrentPassword) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURRENT_PASSWORD", "当前密码错误。")
		return
	}
	challenge, err := server.mail.Verify(request.Context(), input.ChallengeID, "bind_email", strings.TrimSpace(input.Code), &user.ID)
	if err != nil {
		writeMailError(writer, request, err)
		return
	}
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE users SET email=$1,email_verified_at=now(),updated_at=now() WHERE id=$2`, normalizedEmail(challenge.Email), user.ID)
	}
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, user.ID)
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	if err != nil {
		databaseError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "email_verified", "message": "邮箱已验证，请重新登录。"})
}
