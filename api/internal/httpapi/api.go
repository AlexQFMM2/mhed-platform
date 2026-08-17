package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AlexQFMM2/mhed-platform/api/internal/auth"
	"github.com/AlexQFMM2/mhed-platform/api/internal/config"
	"github.com/AlexQFMM2/mhed-platform/api/internal/game/mh3g"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionCookie = "mhed_admin_session"

type apiServer struct {
	logger      *slog.Logger
	pool        *pgxpool.Pool
	game        *mh3g.Adapter
	gameError   error
	requireGame bool
	config      config.Config
}
type sessionUser struct {
	ID                 string   `json:"id"`
	PublicID           int64    `json:"public_id"`
	Nickname           string   `json:"nickname"`
	Username           string   `json:"username"`
	Status             string   `json:"status"`
	MustChangePassword bool     `json:"must_change_password"`
	Roles              []string `json:"roles"`
	SessionID          string   `json:"-"`
	CSRFHash           []byte   `json:"-"`
}
type contextKey int

const userContextKey contextKey = 1

func newAPIServer(logger *slog.Logger) *apiServer {
	return &apiServer{logger: logger, config: config.Config{Environment: "development", AdminOrigin: "http://127.0.0.1:18102", ReportHMACKey: "development-only", CookieSecure: false}}
}

func (server *apiServer) routes(router chi.Router) {
	if server.pool == nil {
		return
	}
	router.Route("/v1/auth", func(r chi.Router) {
		r.Post("/login", server.login)
		r.Group(func(p chi.Router) {
			p.Use(server.requireAuthentication)
			p.Get("/me", server.me)
			p.With(server.requireCSRF).Post("/change-password", server.changePassword)
			p.With(server.requireCSRF).Post("/logout", server.logout)
		})
	})
	router.Route("/v1/desktop", func(r chi.Router) {
		r.Post("/auth/login", server.desktopLogin)
		r.With(server.optionalDesktopAuthentication).Get("/loadouts", server.publicLoadouts)
		r.With(server.optionalDesktopAuthentication).Get("/loadouts/{id}", server.publicLoadout)
		r.Group(func(account chi.Router) {
			account.Use(server.requireDesktopAuthentication)
			account.Get("/me", server.desktopMe)
			account.Post("/auth/change-password", server.desktopChangePassword)
			account.Post("/auth/logout", server.desktopLogout)
			account.Group(func(ready chi.Router) {
				ready.Use(requireDesktopReady)
				ready.Patch("/me", server.updateDesktopProfile)
				ready.Post("/loadouts", server.createDesktopLoadout)
				ready.Get("/me/loadouts", server.desktopOwnLoadouts)
				ready.Delete("/loadouts/{id}", server.deleteDesktopLoadout)
				ready.Post("/loadouts/{id}/likes", server.likeLoadout)
				ready.Delete("/loadouts/{id}/likes", server.unlikeLoadout)
				ready.Post("/loadouts/{id}/reports", server.createReport)
			})
		})
	})
	router.Route("/v1/admin", func(r chi.Router) {
		r.Use(server.requireAuthentication, server.requireAdmin)
		r.Get("/dashboard", server.dashboard)
		r.Get("/users", server.users)
		r.With(server.requireCSRF).Post("/users", server.createUser)
		r.With(server.requireCSRF).Patch("/users/{id}/status", server.updateUserStatus)
		r.With(server.requireCSRF).Post("/users/{id}/reset-password", server.resetUserPassword)
		r.With(server.requireCSRF).Put("/users/{id}/roles", server.replaceUserRoles)
		r.Get("/roles", server.roles)
		r.With(server.requireCSRF).Post("/roles", server.createRole)
		r.With(server.requireCSRF).Put("/roles/{id}", server.updateRole)
		r.With(server.requireCSRF).Delete("/roles/{id}", server.deleteRole)
		r.Get("/permissions", server.permissions)
		r.Get("/loadouts", server.adminLoadouts)
		r.With(server.requireCSRF).Post("/loadouts/preview", server.previewLoadout)
		r.With(server.requireCSRF).Post("/loadouts", server.createLoadout)
		r.Get("/loadouts/{id}", server.adminLoadout)
		r.With(server.requireCSRF).Put("/loadouts/{id}", server.updateLoadout)
		r.With(server.requireCSRF).Patch("/loadouts/{id}/status", server.updateLoadoutStatus)
		r.Get("/reports", server.reports)
		r.With(server.requireCSRF).Post("/reports/{id}/resolve", server.resolveReport)
		r.Get("/audit-logs", server.auditLogs)
		r.Get("/game-data/mh3g/equipment", server.gameEquipment)
		r.Get("/game-data/mh3g/meta", server.gameMeta)
		r.Get("/game-data/mh3g/decorations", server.gameDecorations)
		r.Get("/game-data/mh3g/skills", server.gameSkills)
		r.Get("/game-data/mh3g/charm-classes", server.gameCharmClasses)
		r.Get("/game-data/mh3g/charm-rules", server.gameCharmRules)
	})
}

func readJSON(writer http.ResponseWriter, request *http.Request, value any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "request body contains trailing data")
		return false
	}
	return true
}
func writeError(writer http.ResponseWriter, request *http.Request, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": middleware.GetReqID(request.Context())}})
}
func databaseError(writer http.ResponseWriter, request *http.Request, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "loadouts_unique_build_idx" {
			writeError(writer, request, http.StatusConflict, "DUPLICATE_LOADOUT", "已经有人上传过一模一样的配装。")
			return
		}
		writeError(writer, request, http.StatusConflict, "CONFLICT", "记录已存在或与现有数据冲突。")
		return
	}
	serverError(writer, request, err)
}
func serverError(writer http.ResponseWriter, request *http.Request, err error) {
	slog.Error("request failed", "request_id", middleware.GetReqID(request.Context()), "error", err)
	writeError(writer, request, http.StatusInternalServerError, "INTERNAL_ERROR", "服务器处理失败。")
}
func currentUser(request *http.Request) sessionUser {
	return request.Context().Value(userContextKey).(sessionUser)
}
func optionalCurrentUser(request *http.Request) (sessionUser, bool) {
	user, ok := request.Context().Value(userContextKey).(sessionUser)
	return user, ok
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func hashSecret(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }

func (server *apiServer) setSessionCookie(writer http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(writer, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: server.config.CookieSecure, SameSite: http.SameSiteStrictMode})
}
func (server *apiServer) clearSessionCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: server.config.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (server *apiServer) requireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(writer, request, http.StatusUnauthorized, "AUTH_REQUIRED", "请先登录。")
			return
		}
		tokenHash := hashSecret(cookie.Value)
		var user sessionUser
		var roles []string
		err = server.pool.QueryRow(request.Context(), `SELECT u.id,u.public_id,u.nickname,u.username,u.status,u.must_change_password,s.id,s.csrf_hash,coalesce(array_agg(r.key) FILTER (WHERE r.key IS NOT NULL),'{}')
		FROM sessions s JOIN users u ON u.id=s.user_id LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id
		WHERE s.token_hash=$1 AND s.client_type='browser' AND s.revoked_at IS NULL AND s.idle_expires_at>now() AND s.absolute_expires_at>now() AND u.status='active'
		GROUP BY u.id,s.id`, tokenHash).Scan(&user.ID, &user.PublicID, &user.Nickname, &user.Username, &user.Status, &user.MustChangePassword, &user.SessionID, &user.CSRFHash, &roles)
		if err != nil {
			server.clearSessionCookie(writer)
			writeError(writer, request, http.StatusUnauthorized, "AUTH_REQUIRED", "会话已失效，请重新登录。")
			return
		}
		user.Roles = roles
		_, _ = server.pool.Exec(request.Context(), `UPDATE sessions SET last_used_at=now(),idle_expires_at=least(now()+interval '8 hours',absolute_expires_at) WHERE id=$1`, user.SessionID)
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), userContextKey, user)))
	})
}
func (server *apiServer) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user := currentUser(request)
		if user.MustChangePassword {
			writeError(writer, request, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "首次登录必须先修改临时密码。")
			return
		}
		if !contains(user.Roles, "super_admin") {
			writeError(writer, request, http.StatusForbidden, "ADMIN_FORBIDDEN", "当前账号无权访问管理后台。")
			return
		}
		next.ServeHTTP(writer, request)
	})
}
func (server *apiServer) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := strings.TrimRight(request.Header.Get("Origin"), "/")
		if server.config.AdminOrigin != "" && origin != server.config.AdminOrigin {
			writeError(writer, request, http.StatusForbidden, "CSRF_REJECTED", "请求来源无效。")
			return
		}
		user := currentUser(request)
		if !hmac.Equal(hashSecret(request.Header.Get("X-CSRF-Token")), user.CSRFHash) {
			writeError(writer, request, http.StatusForbidden, "CSRF_REJECTED", "请求验证令牌无效。")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *apiServer) login(writer http.ResponseWriter, request *http.Request) {
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
	_, err = server.pool.Exec(request.Context(), `INSERT INTO sessions(user_id,token_hash,csrf_hash,idle_expires_at,absolute_expires_at,user_agent) VALUES($1,$2,$3,now()+interval '8 hours',$4,$5)`, user.ID, hashSecret(token), hashSecret(csrf), absolute, truncate(request.UserAgent(), 300))
	if err != nil {
		serverError(writer, request, err)
		return
	}
	_, _ = server.pool.Exec(request.Context(), `UPDATE users SET last_login_at=now(),updated_at=now() WHERE id=$1`, user.ID)
	server.setSessionCookie(writer, token, absolute)
	roles, _ := server.userRoles(request.Context(), user.ID)
	user.Roles = roles
	writeJSON(writer, http.StatusOK, map[string]any{"user": user, "csrf_token": csrf})
}
func (server *apiServer) me(writer http.ResponseWriter, request *http.Request) {
	user := currentUser(request)
	csrf, err := auth.RandomSecret(24)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	if _, err := server.pool.Exec(request.Context(), `UPDATE sessions SET csrf_hash=$1 WHERE id=$2`, hashSecret(csrf), user.SessionID); err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"user": user, "csrf_token": csrf})
}
func (server *apiServer) logout(writer http.ResponseWriter, request *http.Request) {
	user := currentUser(request)
	_, _ = server.pool.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE id=$1`, user.SessionID)
	server.clearSessionCookie(writer)
	writer.WriteHeader(http.StatusNoContent)
}
func (server *apiServer) changePassword(writer http.ResponseWriter, request *http.Request) {
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
	server.clearSessionCookie(writer)
	writeJSON(writer, http.StatusOK, map[string]string{"status": "password_changed", "message": "密码已修改，请重新登录。"})
}
func truncate(value string, limit int) string {
	characters := []rune(value)
	if len(characters) > limit {
		return string(characters[:limit])
	}
	return value
}
func (server *apiServer) userRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := server.pool.Query(ctx, `SELECT r.key FROM roles r JOIN user_roles ur ON ur.role_id=r.id WHERE ur.user_id=$1 ORDER BY r.key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (server *apiServer) audit(ctx context.Context, tx pgx.Tx, actor sessionUser, action, targetType, targetID string, metadata any, requestID string) error {
	bytes, _ := json.Marshal(metadata)
	_, err := tx.Exec(ctx, `INSERT INTO admin_audit_logs(actor_user_id,action,target_type,target_id,request_id,metadata) VALUES($1,$2,$3,$4,$5,$6)`, actor.ID, action, targetType, targetID, requestID, bytes)
	return err
}
func requestID(request *http.Request) string { return middleware.GetReqID(request.Context()) }
func parseLimit(request *http.Request) int {
	value, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	if value < 1 || value > 100 {
		return 30
	}
	return value
}
func remoteAddress(request *http.Request) string {
	value := request.RemoteAddr
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return value
}
func reportDigest(key, source string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(source))
	return mac.Sum(nil)
}
func hexString(value []byte) string { return hex.EncodeToString(value) }
func notFound(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, request, http.StatusNotFound, "NOT_FOUND", "资源不存在。")
}
func scanRowsError(writer http.ResponseWriter, request *http.Request, err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		notFound(writer, request)
		return true
	}
	if err != nil {
		serverError(writer, request, err)
		return true
	}
	return false
}
func requireGame(writer http.ResponseWriter, request *http.Request, game *mh3g.Adapter) bool {
	if game == nil {
		writeError(writer, request, http.StatusServiceUnavailable, "GAME_DATA_UNAVAILABLE", "MH3G 游戏数据不可用。")
		return false
	}
	return true
}
func parseInteger(request *http.Request, key string, defaultValue int) int {
	value, err := strconv.Atoi(request.URL.Query().Get(key))
	if err != nil {
		return defaultValue
	}
	return value
}

var _ = fmt.Sprintf
