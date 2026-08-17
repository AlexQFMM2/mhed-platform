package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type duplicateLoadout struct {
	ID            string
	Name          string
	OwnerNickname string
}

func (server *apiServer) findDuplicateLoadout(ctx context.Context, buildHash []byte, excludeID string) (duplicateLoadout, error) {
	var duplicate duplicateLoadout
	err := server.pool.QueryRow(ctx, `SELECT l.id,l.name,u.nickname FROM loadouts l JOIN users u ON u.id=l.owner_user_id WHERE l.game='mh3g' AND l.data_version=$1 AND l.build_hash=$2 AND l.status<>'deleted' AND (NULLIF($3,'') IS NULL OR l.id<>NULLIF($3,'')::uuid) LIMIT 1`, server.game.DataVersion(), buildHash, excludeID).Scan(&duplicate.ID, &duplicate.Name, &duplicate.OwnerNickname)
	return duplicate, err
}

func (server *apiServer) rejectDuplicateLoadout(writer http.ResponseWriter, request *http.Request, buildHash []byte, excludeID string) bool {
	duplicate, err := server.findDuplicateLoadout(request.Context(), buildHash, excludeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		serverError(writer, request, err)
		return true
	}
	writeJSON(writer, http.StatusConflict, map[string]any{"error": map[string]any{
		"code": "DUPLICATE_LOADOUT", "message": "已经有人上传过一模一样的配装。", "request_id": requestID(request),
		"existing_loadout_id": duplicate.ID, "existing_loadout_name": duplicate.Name, "owner_nickname": duplicate.OwnerNickname,
	}})
	return true
}

func (server *apiServer) createDesktopLoadout(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Remark  string          `json:"remark"`
		Payload json.RawMessage `json:"payload"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	validatedInput := loadoutInput{Remark: input.Remark, Status: "published", Payload: input.Payload}
	result, ok := server.validateLoadout(writer, request, validatedInput)
	if !ok {
		return
	}
	if server.rejectDuplicateLoadout(writer, request, result.BuildHash, "") {
		return
	}
	actor := currentUser(request)
	summary, _ := json.Marshal(result.Summary)
	tx, err := server.pool.Begin(request.Context())
	var id string
	if err == nil {
		err = tx.QueryRow(request.Context(), `INSERT INTO loadouts(owner_user_id,game,schema_version,data_version,name,remark,payload,content_hash,build_hash,risk_summary,is_legal,status,published_at,created_by,updated_by) VALUES($1::uuid,'mh3g',1,$2,$3,$4,$5::jsonb,$6,$7,$8::jsonb,$9,'published',now(),$1::uuid,$1::uuid) RETURNING id`, actor.ID, server.game.DataVersion(), result.Payload.Name, strings.TrimSpace(input.Remark), result.Canonical, result.Hash, result.BuildHash, summary, result.IsLegal).Scan(&id)
	}
	if err == nil {
		err = writeLoadoutIndexes(request, tx, id, result)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "loadout.desktop_create", "loadout", id, map[string]any{"is_legal": result.IsLegal}, requestID(request))
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
	writeJSON(writer, http.StatusCreated, map[string]any{"id": id, "is_legal": result.IsLegal, "summary": result.Summary})
}

func (server *apiServer) desktopOwnLoadouts(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.pool.Query(request.Context(), `SELECT `+loadoutColumns+` FROM loadouts l JOIN users u ON u.id=l.owner_user_id WHERE l.owner_user_id=$1 AND l.status<>'deleted' ORDER BY l.updated_at DESC LIMIT 200`, currentUser(request).ID)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	items := []publicLoadoutView{}
	for rows.Next() {
		value, err := loadoutScan(rows, false)
		if err != nil {
			serverError(writer, request, err)
			return
		}
		items = append(items, publicLoadout(value))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

func (server *apiServer) deleteDesktopLoadout(writer http.ResponseWriter, request *http.Request) {
	actor := currentUser(request)
	id := chiURL(request, "id")
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		tag, updateErr := tx.Exec(request.Context(), `UPDATE loadouts SET status='deleted',deleted_at=now(),updated_at=now(),updated_by=$1,version=version+1 WHERE id=$2 AND owner_user_id=$1 AND status<>'deleted'`, actor.ID, id)
		err = updateErr
		if err == nil && tag.RowsAffected() == 0 {
			err = pgx.ErrNoRows
		}
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "loadout.desktop_delete", "loadout", id, map[string]any{}, requestID(request))
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

func (server *apiServer) likeLoadout(writer http.ResponseWriter, request *http.Request) {
	tag, err := server.pool.Exec(request.Context(), `INSERT INTO loadout_likes(user_id,loadout_id) SELECT $1,id FROM loadouts WHERE id=$2 AND status='published' ON CONFLICT DO NOTHING`, currentUser(request).ID, chiURL(request, "id"))
	if err != nil {
		databaseError(writer, request, err)
		return
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		_ = server.pool.QueryRow(request.Context(), `SELECT EXISTS(SELECT 1 FROM loadouts WHERE id=$1 AND status='published')`, chiURL(request, "id")).Scan(&exists)
		if !exists {
			notFound(writer, request)
			return
		}
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *apiServer) unlikeLoadout(writer http.ResponseWriter, request *http.Request) {
	if _, err := server.pool.Exec(request.Context(), `DELETE FROM loadout_likes WHERE user_id=$1 AND loadout_id=$2`, currentUser(request).ID, chiURL(request, "id")); err != nil {
		databaseError(writer, request, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
