package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AlexQFMM2/mhed-platform/api/internal/auth"
)

type desktopProfile struct {
	PublicID           int64  `json:"public_id"`
	Nickname           string `json:"nickname"`
	MustChangePassword bool   `json:"must_change_password"`
}

func profileFor(user sessionUser) desktopProfile {
	return desktopProfile{PublicID: user.PublicID, Nickname: user.Nickname, MustChangePassword: user.MustChangePassword}
}

func (server *apiServer) desktopLogin(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	var user sessionUser
	var passwordHash string
	err := server.pool.QueryRow(request.Context(), `SELECT id,public_id,nickname,username,password_hash,status,must_change_password FROM users WHERE lower(username)=lower($1)`, strings.TrimSpace(input.Username)).Scan(&user.ID, &user.PublicID, &user.Nickname, &user.Username, &passwordHash, &user.Status, &user.MustChangePassword)
	if err != nil || user.Status != "active" || !auth.VerifyPassword(passwordHash, input.Password) {
		writeError(writer, request, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误。")
		return
	}
	token, err := auth.RandomSecret(32)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	csrf, err := auth.RandomSecret(24)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	absolute := time.Now().Add(24 * time.Hour)
	_, err = server.pool.Exec(request.Context(), `INSERT INTO sessions(user_id,token_hash,csrf_hash,idle_expires_at,absolute_expires_at,user_agent,client_type) VALUES($1,$2,$3,now()+interval '8 hours',$4,$5,'desktop')`, user.ID, hashSecret(token), hashSecret(csrf), absolute, truncate(request.UserAgent(), 300))
	if err != nil {
		serverError(writer, request, err)
		return
	}
	_, _ = server.pool.Exec(request.Context(), `UPDATE users SET last_login_at=now(),updated_at=now() WHERE id=$1`, user.ID)
	writeJSON(writer, http.StatusOK, map[string]any{"access_token": token, "expires_at": absolute.UTC().Format(time.RFC3339), "user": profileFor(user)})
}

func bearerToken(request *http.Request) string {
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func (server *apiServer) desktopSession(ctx context.Context, token string) (sessionUser, error) {
	var user sessionUser
	err := server.pool.QueryRow(ctx, `SELECT u.id,u.public_id,u.nickname,u.username,u.status,u.must_change_password,s.id,s.csrf_hash
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.client_type='desktop' AND s.revoked_at IS NULL AND s.idle_expires_at>now() AND s.absolute_expires_at>now() AND u.status='active'`, hashSecret(token)).Scan(&user.ID, &user.PublicID, &user.Nickname, &user.Username, &user.Status, &user.MustChangePassword, &user.SessionID, &user.CSRFHash)
	if err == nil {
		_, _ = server.pool.Exec(ctx, `UPDATE sessions SET last_used_at=now(),idle_expires_at=least(now()+interval '8 hours',absolute_expires_at) WHERE id=$1`, user.SessionID)
	}
	return user, err
}

func (server *apiServer) requireDesktopAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		token := bearerToken(request)
		if token == "" {
			writeError(writer, request, http.StatusUnauthorized, "AUTH_REQUIRED", "请先登录。")
			return
		}
		user, err := server.desktopSession(request.Context(), token)
		if err != nil {
			writeError(writer, request, http.StatusUnauthorized, "AUTH_REQUIRED", "会话已失效，请重新登录。")
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), userContextKey, user)))
	})
}

func (server *apiServer) optionalDesktopAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		token := bearerToken(request)
		if token == "" {
			next.ServeHTTP(writer, request)
			return
		}
		user, err := server.desktopSession(request.Context(), token)
		if err != nil {
			writeError(writer, request, http.StatusUnauthorized, "AUTH_REQUIRED", "会话已失效，请重新登录。")
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), userContextKey, user)))
	})
}

func requireDesktopReady(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if currentUser(request).MustChangePassword {
			writeError(writer, request, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "首次登录必须先修改临时密码。")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *apiServer) desktopMe(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"user": profileFor(currentUser(request))})
}

func validNickname(value string) bool {
	count := utf8.RuneCountInString(value)
	if count < 2 || count > 32 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (server *apiServer) updateDesktopProfile(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Nickname string `json:"nickname"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	input.Nickname = strings.TrimSpace(input.Nickname)
	if !validNickname(input.Nickname) {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "昵称必须为 2～32 个字符且不能包含控制字符。")
		return
	}
	user := currentUser(request)
	if _, err := server.pool.Exec(request.Context(), `UPDATE users SET nickname=$1,updated_at=now() WHERE id=$2`, input.Nickname, user.ID); err != nil {
		databaseError(writer, request, err)
		return
	}
	user.Nickname = input.Nickname
	writeJSON(writer, http.StatusOK, map[string]any{"user": profileFor(user)})
}

func (server *apiServer) desktopLogout(writer http.ResponseWriter, request *http.Request) {
	_, _ = server.pool.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE id=$1`, currentUser(request).SessionID)
	writer.WriteHeader(http.StatusNoContent)
}

func (server *apiServer) desktopChangePassword(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	user := currentUser(request)
	var currentHash string
	if err := server.pool.QueryRow(request.Context(), `SELECT password_hash FROM users WHERE id=$1`, user.ID).Scan(&currentHash); err != nil {
		serverError(writer, request, err)
		return
	}
	if !auth.VerifyPassword(currentHash, input.CurrentPassword) {
		writeError(writer, request, http.StatusBadRequest, "INVALID_CURRENT_PASSWORD", "当前密码错误。")
		return
	}
	hash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "新密码必须为 12～128 字节。")
		return
	}
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE users SET password_hash=$1,must_change_password=false,updated_at=now() WHERE id=$2`, hash, user.ID)
	}
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1`, user.ID)
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
	writeJSON(writer, http.StatusOK, map[string]string{"status": "password_changed", "message": "密码已修改，请重新登录。"})
}
