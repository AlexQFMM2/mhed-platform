package httpapi

import (
	"net/http"
	"strings"

	"github.com/AlexQFMM2/mhed-platform/api/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type userView struct {
	ID                 string   `json:"id"`
	PublicID           int64    `json:"public_id"`
	Nickname           string   `json:"nickname"`
	Username           string   `json:"username"`
	Email              *string  `json:"email"`
	EmailVerified      bool     `json:"email_verified"`
	Status             string   `json:"status"`
	MustChangePassword bool     `json:"must_change_password"`
	Roles              []string `json:"roles"`
	CreatedAt          string   `json:"created_at"`
	LastLoginAt        *string  `json:"last_login_at"`
}

func (server *apiServer) users(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.pool.Query(request.Context(), `SELECT u.id,u.public_id,u.nickname,u.username,u.email,u.email_verified_at IS NOT NULL,u.status,u.must_change_password,to_char(u.created_at,'YYYY-MM-DD"T"HH24:MI:SSOF'),CASE WHEN u.last_login_at IS NULL THEN NULL ELSE to_char(u.last_login_at,'YYYY-MM-DD"T"HH24:MI:SSOF') END,coalesce(array_agg(r.key) FILTER(WHERE r.key IS NOT NULL),'{}') FROM users u LEFT JOIN user_roles ur ON ur.user_id=u.id LEFT JOIN roles r ON r.id=ur.role_id GROUP BY u.id ORDER BY u.created_at DESC LIMIT 200`)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	values := []userView{}
	for rows.Next() {
		var row userView
		if err := rows.Scan(&row.ID, &row.PublicID, &row.Nickname, &row.Username, &row.Email, &row.EmailVerified, &row.Status, &row.MustChangePassword, &row.CreatedAt, &row.LastLoginAt, &row.Roles); err != nil {
			serverError(writer, request, err)
			return
		}
		values = append(values, row)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 32 {
		return false
	}
	for _, c := range value {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}
func (server *apiServer) createUser(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Username string  `json:"username"`
		Email    *string `json:"email"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !validUsername(input.Username) {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "用户名必须是 3～32 位字母、数字或下划线。")
		return
	}
	if input.Email != nil {
		trimmed := strings.TrimSpace(*input.Email)
		if trimmed == "" {
			input.Email = nil
		} else {
			input.Email = &trimmed
		}
	}
	password, err := auth.RandomPassword(16)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	var id, nickname string
	var publicID int64
	if err == nil {
		err = tx.QueryRow(request.Context(), `INSERT INTO users(username,email,password_hash,created_by) VALUES($1,$2,$3,$4) RETURNING id,public_id,nickname`, input.Username, input.Email, hash, actor.ID).Scan(&id, &publicID, &nickname)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "user.create", "user", id, map[string]any{"username": input.Username}, requestID(request))
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
	writeJSON(writer, http.StatusCreated, map[string]any{"id": id, "public_id": publicID, "nickname": nickname, "username": input.Username, "temporary_password": password, "must_change_password": true})
}

func (server *apiServer) updateUserStatus(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	if input.Status != "active" && input.Status != "disabled" && input.Status != "deleted" {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "用户状态无效。")
		return
	}
	id := chiURL(request, "id")
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	var command string
	if err == nil {
		tag, e := tx.Exec(request.Context(), `UPDATE users SET status=$1::text,updated_at=now(),deleted_at=CASE WHEN $1::text='deleted' THEN now() ELSE NULL END WHERE id=$2::uuid`, input.Status, id)
		err = e
		if tag.RowsAffected() == 0 && err == nil {
			err = pgx.ErrNoRows
		}
		command = "user." + input.Status
	}
	if err == nil && input.Status != "active" {
		_, err = tx.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, command, "user", id, map[string]any{}, requestID(request))
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	if scanRowsError(writer, request, err) {
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *apiServer) resetUserPassword(writer http.ResponseWriter, request *http.Request) {
	id := chiURL(request, "id")
	password, err := auth.RandomPassword(16)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		tag, e := tx.Exec(request.Context(), `UPDATE users SET password_hash=$1,must_change_password=true,updated_at=now() WHERE id=$2 AND status<>'deleted'`, hash, id)
		err = e
		if tag.RowsAffected() == 0 && err == nil {
			err = pgx.ErrNoRows
		}
	}
	if err == nil {
		_, err = tx.Exec(request.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1`, id)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "user.password_reset", "user", id, map[string]any{}, requestID(request))
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	if scanRowsError(writer, request, err) {
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"temporary_password": password, "must_change_password": true})
}

func (server *apiServer) replaceUserRoles(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		RoleIDs []string `json:"role_ids"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	id := chiURL(request, "id")
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		_, err = tx.Exec(request.Context(), `DELETE FROM user_roles WHERE user_id=$1`, id)
	}
	if err == nil {
		for _, roleID := range input.RoleIDs {
			if _, err = tx.Exec(request.Context(), `INSERT INTO user_roles(user_id,role_id,assigned_by) VALUES($1,$2,$3)`, id, roleID, actor.ID); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "user.roles_replace", "user", id, map[string]any{"role_ids": input.RoleIDs}, requestID(request))
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
	writer.WriteHeader(http.StatusNoContent)
}

type roleView struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsSystem    bool     `json:"is_system"`
	Permissions []string `json:"permissions"`
	MemberCount int      `json:"member_count"`
}

func (server *apiServer) roles(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.pool.Query(request.Context(), `SELECT r.id,r.key,r.name,r.description,r.is_system,coalesce(array_agg(DISTINCT p.key) FILTER(WHERE p.key IS NOT NULL),'{}'),count(DISTINCT ur.user_id) FROM roles r LEFT JOIN role_permissions rp ON rp.role_id=r.id LEFT JOIN permissions p ON p.id=rp.permission_id LEFT JOIN user_roles ur ON ur.role_id=r.id GROUP BY r.id ORDER BY r.is_system DESC,r.key`)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	values := []roleView{}
	for rows.Next() {
		var row roleView
		if err := rows.Scan(&row.ID, &row.Key, &row.Name, &row.Description, &row.IsSystem, &row.Permissions, &row.MemberCount); err != nil {
			serverError(writer, request, err)
			return
		}
		values = append(values, row)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}
func (server *apiServer) permissions(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.pool.Query(request.Context(), `SELECT id,key,name,description FROM permissions ORDER BY key`)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	values := []map[string]any{}
	for rows.Next() {
		var id, key, name, description string
		if err := rows.Scan(&id, &key, &name, &description); err != nil {
			serverError(writer, request, err)
			return
		}
		values = append(values, map[string]any{"id": id, "key": key, "name": name, "description": description})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

type roleInput struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func (server *apiServer) createRole(writer http.ResponseWriter, request *http.Request) {
	var input roleInput
	if !readJSON(writer, request, &input) {
		return
	}
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	var id string
	if err == nil {
		err = tx.QueryRow(request.Context(), `INSERT INTO roles(key,name,description) VALUES($1,$2,$3) RETURNING id`, input.Key, truncate(strings.TrimSpace(input.Name), 80), truncate(strings.TrimSpace(input.Description), 300)).Scan(&id)
	}
	if err == nil {
		err = replaceRolePermissions(request, tx, id, input.Permissions)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "role.create", "role", id, map[string]any{"key": input.Key}, requestID(request))
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
	writeJSON(writer, http.StatusCreated, map[string]string{"id": id})
}
func (server *apiServer) updateRole(writer http.ResponseWriter, request *http.Request) {
	var input roleInput
	if !readJSON(writer, request, &input) {
		return
	}
	id := chiURL(request, "id")
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		tag, e := tx.Exec(request.Context(), `UPDATE roles SET key=CASE WHEN is_system THEN key ELSE $1 END,name=$2,description=$3,updated_at=now() WHERE id=$4`, input.Key, truncate(strings.TrimSpace(input.Name), 80), truncate(strings.TrimSpace(input.Description), 300), id)
		err = e
		if tag.RowsAffected() == 0 && err == nil {
			err = pgx.ErrNoRows
		}
	}
	if err == nil {
		err = replaceRolePermissions(request, tx, id, input.Permissions)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "role.update", "role", id, map[string]any{}, requestID(request))
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	if scanRowsError(writer, request, err) {
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
func replaceRolePermissions(request *http.Request, tx pgx.Tx, roleID string, keys []string) error {
	if _, err := tx.Exec(request.Context(), `DELETE FROM role_permissions WHERE role_id=$1`, roleID); err != nil {
		return err
	}
	for _, key := range keys {
		tag, err := tx.Exec(request.Context(), `INSERT INTO role_permissions(role_id,permission_id) SELECT $1,id FROM permissions WHERE key=$2`, roleID, key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	}
	return nil
}
func (server *apiServer) deleteRole(writer http.ResponseWriter, request *http.Request) {
	id := chiURL(request, "id")
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		tag, e := tx.Exec(request.Context(), `DELETE FROM roles WHERE id=$1 AND is_system=false`, id)
		err = e
		if tag.RowsAffected() == 0 && err == nil {
			err = pgx.ErrNoRows
		}
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "role.delete", "role", id, map[string]any{}, requestID(request))
	}
	if err == nil {
		err = tx.Commit(request.Context())
	} else if tx != nil {
		tx.Rollback(request.Context())
	}
	if scanRowsError(writer, request, err) {
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
func chiURL(request *http.Request, key string) string { return chi.URLParam(request, key) }
