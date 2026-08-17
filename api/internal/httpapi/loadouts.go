package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/AlexQFMM2/mhed-platform/api/internal/game/mh3g"
	"github.com/jackc/pgx/v5"
)

type loadoutInput struct {
	OwnerUserID string          `json:"owner_user_id"`
	Remark      string          `json:"remark"`
	Status      string          `json:"status"`
	Version     int             `json:"version,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}
type loadoutView struct {
	ID            string          `json:"id"`
	OwnerUserID   string          `json:"owner_user_id"`
	OwnerPublicID int64           `json:"owner_public_id"`
	OwnerNickname string          `json:"owner_nickname"`
	OwnerUsername string          `json:"owner_username"`
	Name          string          `json:"name"`
	Remark        string          `json:"remark"`
	Status        string          `json:"status"`
	Version       int             `json:"version"`
	DataVersion   string          `json:"data_version"`
	IsLegal       bool            `json:"is_legal"`
	RiskSummary   json.RawMessage `json:"risk_summary"`
	LikeCount     int             `json:"like_count"`
	LikedByMe     bool            `json:"liked_by_me"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	PublishedAt   *string         `json:"published_at"`
	UpdatedAt     string          `json:"updated_at"`
}
type publicLoadoutView struct {
	ID            string          `json:"id"`
	OwnerPublicID int64           `json:"owner_public_id"`
	OwnerNickname string          `json:"owner_nickname"`
	Name          string          `json:"name"`
	Remark        string          `json:"remark"`
	DataVersion   string          `json:"data_version"`
	IsLegal       bool            `json:"is_legal"`
	RiskSummary   json.RawMessage `json:"risk_summary"`
	LikeCount     int             `json:"like_count"`
	LikedByMe     bool            `json:"liked_by_me"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	PublishedAt   *string         `json:"published_at"`
	UpdatedAt     string          `json:"updated_at"`
}

func publicLoadout(value loadoutView) publicLoadoutView {
	return publicLoadoutView{ID: value.ID, OwnerPublicID: value.OwnerPublicID, OwnerNickname: value.OwnerNickname, Name: value.Name, Remark: value.Remark, DataVersion: value.DataVersion, IsLegal: value.IsLegal, RiskSummary: value.RiskSummary, LikeCount: value.LikeCount, LikedByMe: value.LikedByMe, Payload: value.Payload, PublishedAt: value.PublishedAt, UpdatedAt: value.UpdatedAt}
}

func (server *apiServer) validateLoadout(writer http.ResponseWriter, request *http.Request, input loadoutInput) (mh3g.Result, bool) {
	if !requireGame(writer, request, server.game) {
		return mh3g.Result{}, false
	}
	if len([]rune(input.Remark)) > 500 {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "备注最多 500 个字符。")
		return mh3g.Result{}, false
	}
	payload, err := mh3g.Decode(input.Payload)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return mh3g.Result{}, false
	}
	result, err := server.game.Validate(request.Context(), payload, true)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return mh3g.Result{}, false
	}
	return result, true
}
func (server *apiServer) previewLoadout(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Payload json.RawMessage `json:"payload"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	result, ok := server.validateLoadout(writer, request, loadoutInput{Payload: input.Payload})
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *apiServer) createLoadout(writer http.ResponseWriter, request *http.Request) {
	var input loadoutInput
	if !readJSON(writer, request, &input) {
		return
	}
	if input.Status == "" {
		input.Status = "published"
	}
	if input.Status != "published" && input.Status != "hidden" {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "配装状态无效。")
		return
	}
	result, ok := server.validateLoadout(writer, request, input)
	if !ok {
		return
	}
	if server.rejectDuplicateLoadout(writer, request, result.BuildHash, "") {
		return
	}
	actor := currentUser(request)
	if input.OwnerUserID == "" {
		input.OwnerUserID = actor.ID
	}
	summary, _ := json.Marshal(result.Summary)
	tx, err := server.pool.Begin(request.Context())
	var id string
	if err == nil {
		err = tx.QueryRow(request.Context(), `INSERT INTO loadouts(owner_user_id,game,schema_version,data_version,name,remark,payload,content_hash,build_hash,risk_summary,is_legal,status,published_at,created_by,updated_by) VALUES($1::uuid,'mh3g',1,$2,$3,$4,$5::jsonb,$6,$7,$8::jsonb,$9,$10::varchar,CASE WHEN $10::varchar='published' THEN now() END,$11::uuid,$11::uuid) RETURNING id`, input.OwnerUserID, server.game.DataVersion(), result.Payload.Name, strings.TrimSpace(input.Remark), result.Canonical, result.Hash, result.BuildHash, summary, result.IsLegal, input.Status, actor.ID).Scan(&id)
	}
	if err == nil {
		err = writeLoadoutIndexes(request, tx, id, result)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "loadout.create", "loadout", id, map[string]any{"status": input.Status, "is_legal": result.IsLegal}, requestID(request))
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

func writeLoadoutIndexes(request *http.Request, tx pgx.Tx, id string, result mh3g.Result) error {
	if _, err := tx.Exec(request.Context(), `DELETE FROM loadout_equipment_index WHERE loadout_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(request.Context(), `DELETE FROM loadout_skill_index WHERE loadout_id=$1`, id); err != nil {
		return err
	}
	for _, item := range result.Equipment {
		if _, err := tx.Exec(request.Context(), `INSERT INTO loadout_equipment_index(loadout_id,slot,save_type,save_id) VALUES($1,$2,$3,$4)`, id, item.Slot, item.SaveType, item.SaveID); err != nil {
			return err
		}
	}
	for _, item := range result.Skills {
		if _, err := tx.Exec(request.Context(), `INSERT INTO loadout_skill_index(loadout_id,skill_tree_id,points,active_skill_id) VALUES($1,$2,$3,$4)`, id, item.SkillTreeID, item.Points, item.ActiveSkillID); err != nil {
			return err
		}
	}
	return nil
}

func (server *apiServer) updateLoadout(writer http.ResponseWriter, request *http.Request) {
	var input loadoutInput
	if !readJSON(writer, request, &input) {
		return
	}
	if input.Status != "published" && input.Status != "hidden" {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "配装状态无效。")
		return
	}
	result, ok := server.validateLoadout(writer, request, input)
	if !ok {
		return
	}
	id := chiURL(request, "id")
	if server.rejectDuplicateLoadout(writer, request, result.BuildHash, id) {
		return
	}
	actor := currentUser(request)
	if input.OwnerUserID == "" {
		input.OwnerUserID = actor.ID
	}
	summary, _ := json.Marshal(result.Summary)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		tag, e := tx.Exec(request.Context(), `UPDATE loadouts SET owner_user_id=$1::uuid,name=$2,remark=$3,payload=$4::jsonb,content_hash=$5,build_hash=$6,risk_summary=$7::jsonb,is_legal=$8,status=$9::varchar,published_at=CASE WHEN $9::varchar='published' THEN coalesce(published_at,now()) ELSE published_at END,version=version+1,updated_by=$10::uuid,updated_at=now() WHERE id=$11::uuid AND status<>'deleted' AND ($12::integer=0 OR version=$12::integer)`, input.OwnerUserID, result.Payload.Name, strings.TrimSpace(input.Remark), result.Canonical, result.Hash, result.BuildHash, summary, result.IsLegal, input.Status, actor.ID, id, input.Version)
		err = e
		if tag.RowsAffected() == 0 && err == nil {
			err = pgx.ErrNoRows
		}
	}
	if err == nil {
		err = writeLoadoutIndexes(request, tx, id, result)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "loadout.update", "loadout", id, map[string]any{"status": input.Status, "is_legal": result.IsLegal}, requestID(request))
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

func loadoutScan(row pgx.Row, includePayload bool) (loadoutView, error) {
	var value loadoutView
	if includePayload {
		err := row.Scan(&value.ID, &value.OwnerUserID, &value.OwnerPublicID, &value.OwnerNickname, &value.OwnerUsername, &value.Name, &value.Remark, &value.Status, &value.Version, &value.DataVersion, &value.IsLegal, &value.RiskSummary, &value.LikeCount, &value.LikedByMe, &value.Payload, &value.PublishedAt, &value.UpdatedAt)
		return value, err
	}
	err := row.Scan(&value.ID, &value.OwnerUserID, &value.OwnerPublicID, &value.OwnerNickname, &value.OwnerUsername, &value.Name, &value.Remark, &value.Status, &value.Version, &value.DataVersion, &value.IsLegal, &value.RiskSummary, &value.LikeCount, &value.LikedByMe, &value.PublishedAt, &value.UpdatedAt)
	return value, err
}

const loadoutColumns = `l.id,l.owner_user_id,u.public_id,u.nickname,u.username,l.name,l.remark,l.status,l.version,l.data_version,l.is_legal,l.risk_summary,(SELECT count(*) FROM loadout_likes likes WHERE likes.loadout_id=l.id),false,CASE WHEN l.published_at IS NULL THEN NULL ELSE to_char(l.published_at,'YYYY-MM-DD"T"HH24:MI:SSOF') END,to_char(l.updated_at,'YYYY-MM-DD"T"HH24:MI:SSOF')`

func (server *apiServer) adminLoadouts(writer http.ResponseWriter, request *http.Request) {
	status := request.URL.Query().Get("status")
	query := `SELECT ` + loadoutColumns + ` FROM loadouts l JOIN users u ON u.id=l.owner_user_id WHERE ($1='' OR l.status=$1) ORDER BY l.updated_at DESC LIMIT 200`
	rows, err := server.pool.Query(request.Context(), query, status)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	values := []loadoutView{}
	for rows.Next() {
		row, err := loadoutScan(rows, false)
		if err != nil {
			serverError(writer, request, err)
			return
		}
		values = append(values, row)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}
func (server *apiServer) adminLoadout(writer http.ResponseWriter, request *http.Request) {
	query := `SELECT l.id,l.owner_user_id,u.public_id,u.nickname,u.username,l.name,l.remark,l.status,l.version,l.data_version,l.is_legal,l.risk_summary,(SELECT count(*) FROM loadout_likes likes WHERE likes.loadout_id=l.id),false,l.payload,CASE WHEN l.published_at IS NULL THEN NULL ELSE to_char(l.published_at,'YYYY-MM-DD"T"HH24:MI:SSOF') END,to_char(l.updated_at,'YYYY-MM-DD"T"HH24:MI:SSOF') FROM loadouts l JOIN users u ON u.id=l.owner_user_id WHERE l.id=$1`
	value, err := loadoutScan(server.pool.QueryRow(request.Context(), query, chiURL(request, "id")), true)
	if scanRowsError(writer, request, err) {
		return
	}
	writeJSON(writer, http.StatusOK, value)
}
func (server *apiServer) updateLoadoutStatus(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	if input.Status != "published" && input.Status != "hidden" && input.Status != "deleted" {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "配装状态无效。")
		return
	}
	id := chiURL(request, "id")
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	if err == nil {
		tag, e := tx.Exec(request.Context(), `UPDATE loadouts SET status=$1::varchar,published_at=CASE WHEN $1::varchar='published' THEN coalesce(published_at,now()) ELSE published_at END,deleted_at=CASE WHEN $1::varchar='deleted' THEN now() ELSE NULL END,updated_by=$2::uuid,updated_at=now(),version=version+1 WHERE id=$3::uuid`, input.Status, actor.ID, id)
		err = e
		if tag.RowsAffected() == 0 && err == nil {
			err = pgx.ErrNoRows
		}
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "loadout."+input.Status, "loadout", id, map[string]any{}, requestID(request))
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

func (server *apiServer) publicLoadouts(writer http.ResponseWriter, request *http.Request) {
	needle := "%" + strings.TrimSpace(request.URL.Query().Get("q")) + "%"
	legalOnly := request.URL.Query().Get("legal_only") == "true" || request.URL.Query().Get("legal_only") == "1"
	equipment, activeSkills, ok := publicLoadoutFilters(writer, request)
	if !ok {
		return
	}
	limit := parseLimit(request)
	viewerID := ""
	if viewer, ok := optionalCurrentUser(request); ok {
		viewerID = viewer.ID
	}
	columns := `l.id,l.owner_user_id,u.public_id,u.nickname,u.username,l.name,l.remark,l.status,l.version,l.data_version,l.is_legal,l.risk_summary,(SELECT count(*) FROM loadout_likes likes WHERE likes.loadout_id=l.id),EXISTS(SELECT 1 FROM loadout_likes mine WHERE mine.loadout_id=l.id AND mine.user_id=NULLIF($4,'')::uuid),CASE WHEN l.published_at IS NULL THEN NULL ELSE to_char(l.published_at,'YYYY-MM-DD"T"HH24:MI:SSOF') END,to_char(l.updated_at,'YYYY-MM-DD"T"HH24:MI:SSOF')`
	rows, err := server.pool.Query(request.Context(), `SELECT `+columns+` FROM loadouts l JOIN users u ON u.id=l.owner_user_id
		WHERE l.status='published' AND l.game='mh3g' AND (l.name ILIKE $1 OR l.remark ILIKE $1) AND (NOT $2::boolean OR l.is_legal)
		AND (cardinality($5::text[])=0 OR NOT EXISTS (
			SELECT 1 FROM unnest($5::text[]) required(value) WHERE NOT EXISTS (
				SELECT 1 FROM loadout_equipment_index equipment_index
				WHERE equipment_index.loadout_id=l.id AND concat(equipment_index.save_type,':',equipment_index.save_id)=required.value)))
		AND (cardinality($6::integer[])=0 OR NOT EXISTS (
			SELECT 1 FROM unnest($6::integer[]) required(value) WHERE NOT EXISTS (
				SELECT 1 FROM loadout_skill_index skill_index
				WHERE skill_index.loadout_id=l.id AND skill_index.active_skill_id=required.value)))
		ORDER BY l.published_at DESC,l.id DESC LIMIT $3`, needle, legalOnly, limit, viewerID, equipment, activeSkills)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	values := []publicLoadoutView{}
	for rows.Next() {
		value, err := loadoutScan(rows, false)
		if err != nil {
			serverError(writer, request, err)
			return
		}
		values = append(values, publicLoadout(value))
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values, "next_cursor": nil})
}

func publicLoadoutFilters(writer http.ResponseWriter, request *http.Request) ([]string, []int32, bool) {
	equipmentValues := request.URL.Query()["equipment"]
	activeValues := request.URL.Query()["active_skill"]
	if len(equipmentValues) > 8 || len(activeValues) > 8 {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "装备和发动技能筛选条件分别最多 8 项。")
		return nil, nil, false
	}
	equipment := make([]string, 0, len(equipmentValues))
	for _, value := range equipmentValues {
		parts := strings.Split(value, ":")
		if len(parts) != 2 {
			writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "装备筛选条件格式无效。")
			return nil, nil, false
		}
		saveType, typeErr := strconv.Atoi(parts[0])
		saveID, idErr := strconv.Atoi(parts[1])
		if typeErr != nil || idErr != nil || saveType < 1 || saveType > 255 || saveID < 0 || saveID > 65535 {
			writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "装备筛选条件超出范围。")
			return nil, nil, false
		}
		equipment = append(equipment, strconv.Itoa(saveType)+":"+strconv.Itoa(saveID))
	}
	activeSkills := make([]int32, 0, len(activeValues))
	for _, value := range activeValues {
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 || id > 65535 {
			writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "发动技能筛选条件无效。")
			return nil, nil, false
		}
		activeSkills = append(activeSkills, int32(id))
	}
	return equipment, activeSkills, true
}
func (server *apiServer) publicLoadout(writer http.ResponseWriter, request *http.Request) {
	viewerID := ""
	if viewer, ok := optionalCurrentUser(request); ok {
		viewerID = viewer.ID
	}
	query := `SELECT l.id,l.owner_user_id,u.public_id,u.nickname,u.username,l.name,l.remark,l.status,l.version,l.data_version,l.is_legal,l.risk_summary,(SELECT count(*) FROM loadout_likes likes WHERE likes.loadout_id=l.id),EXISTS(SELECT 1 FROM loadout_likes mine WHERE mine.loadout_id=l.id AND mine.user_id=NULLIF($2,'')::uuid),l.payload,CASE WHEN l.published_at IS NULL THEN NULL ELSE to_char(l.published_at,'YYYY-MM-DD"T"HH24:MI:SSOF') END,to_char(l.updated_at,'YYYY-MM-DD"T"HH24:MI:SSOF') FROM loadouts l JOIN users u ON u.id=l.owner_user_id WHERE l.id=$1 AND l.status='published'`
	value, err := loadoutScan(server.pool.QueryRow(request.Context(), query, chiURL(request, "id"), viewerID), true)
	if scanRowsError(writer, request, err) {
		return
	}
	// Detail views use a fresh server calculation so summaries saved by an older
	// adapter also gain new derived fields such as per-equipment skill columns.
	if payload, decodeErr := mh3g.Decode(value.Payload); decodeErr == nil {
		if calculated, calculateErr := server.game.Validate(request.Context(), payload, true); calculateErr == nil {
			if summary, marshalErr := json.Marshal(calculated.Summary); marshalErr == nil {
				value.RiskSummary = summary
				value.IsLegal = calculated.IsLegal
			}
		}
	}
	writeJSON(writer, http.StatusOK, publicLoadout(value))
}

func (server *apiServer) createReport(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	allowed := map[string]bool{"inappropriate": true, "spam": true, "invalid_data": true, "infringement": true, "other": true}
	if !allowed[input.Reason] || len([]rune(input.Details)) > 500 {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "举报内容无效。")
		return
	}
	user := currentUser(request)
	digest := reportDigest(server.config.ReportHMACKey, user.ID)
	var recent int
	_ = server.pool.QueryRow(request.Context(), `SELECT count(*) FROM loadout_reports WHERE source_digest=$1 AND created_at>now()-interval '1 hour'`, digest).Scan(&recent)
	if recent >= 5 {
		writeError(writer, request, http.StatusTooManyRequests, "RATE_LIMITED", "举报过于频繁，请稍后再试。")
		return
	}
	id := chiURL(request, "id")
	tag, err := server.pool.Exec(request.Context(), `INSERT INTO loadout_reports(loadout_id,reporter_user_id,source_digest,reason,details,evidence_name,evidence_remark,evidence_content_hash) SELECT id,$2::uuid,$3,$4,$5,name,remark,content_hash FROM loadouts WHERE id=$1 AND status='published' ON CONFLICT(loadout_id,source_digest) WHERE status='open' DO NOTHING`, id, user.ID, digest, input.Reason, strings.TrimSpace(input.Details))
	if err != nil {
		databaseError(writer, request, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(writer, request, http.StatusConflict, "REPORT_ALREADY_OPEN", "当前来源已提交过待处理举报。")
		return
	}
	writer.WriteHeader(http.StatusCreated)
}

func (server *apiServer) reports(writer http.ResponseWriter, request *http.Request) {
	status := request.URL.Query().Get("status")
	rows, err := server.pool.Query(request.Context(), `SELECT r.id,r.loadout_id,r.reason,r.details,r.evidence_name,r.evidence_remark,encode(r.evidence_content_hash,'hex'),r.status,r.resolution_note,to_char(r.created_at,'YYYY-MM-DD"T"HH24:MI:SSOF'),reporter.public_id,reporter.nickname,handler.username FROM loadout_reports r LEFT JOIN users reporter ON reporter.id=r.reporter_user_id LEFT JOIN users handler ON handler.id=r.handled_by WHERE ($1='' OR r.status=$1) ORDER BY r.created_at DESC LIMIT 200`, status)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, loadoutID, reason, details, name, remark, hash, statusValue, note, created string
		var reporterPublicID *int64
		var reporterNickname, handler *string
		if err := rows.Scan(&id, &loadoutID, &reason, &details, &name, &remark, &hash, &statusValue, &note, &created, &reporterPublicID, &reporterNickname, &handler); err != nil {
			serverError(writer, request, err)
			return
		}
		items = append(items, map[string]any{"id": id, "loadout_id": loadoutID, "reason": reason, "details": details, "evidence_name": name, "evidence_remark": remark, "content_hash": hash, "status": statusValue, "resolution_note": note, "created_at": created, "reporter_public_id": reporterPublicID, "reporter_nickname": reporterNickname, "handled_by": handler})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}
func (server *apiServer) resolveReport(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Status        string `json:"status"`
		Note          string `json:"note"`
		LoadoutAction string `json:"loadout_action"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	if input.Status != "resolved" && input.Status != "dismissed" {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "举报处理状态无效。")
		return
	}
	actions := map[string]string{"": "", "none": "", "hide": "hidden", "restore": "published", "delete": "deleted"}
	loadoutStatus, ok := actions[input.LoadoutAction]
	if !ok {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", "配装处理动作无效。")
		return
	}
	id := chiURL(request, "id")
	actor := currentUser(request)
	tx, err := server.pool.Begin(request.Context())
	var loadoutID string
	if err == nil {
		err = tx.QueryRow(request.Context(), `UPDATE loadout_reports SET status=$1,handled_by=$2,resolution_note=$3,handled_at=now() WHERE id=$4 AND status='open' RETURNING loadout_id`, input.Status, actor.ID, truncate(strings.TrimSpace(input.Note), 500), id).Scan(&loadoutID)
	}
	if err == nil && loadoutStatus != "" {
		_, err = tx.Exec(request.Context(), `UPDATE loadouts SET status=$1::varchar,updated_by=$2::uuid,updated_at=now(),deleted_at=CASE WHEN $1::varchar='deleted' THEN now() ELSE NULL END,published_at=CASE WHEN $1::varchar='published' THEN coalesce(published_at,now()) ELSE published_at END WHERE id=$3::uuid`, loadoutStatus, actor.ID, loadoutID)
	}
	if err == nil {
		err = server.audit(request.Context(), tx, actor, "report."+input.Status, "report", id, map[string]any{"loadout_action": input.LoadoutAction}, requestID(request))
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

func (server *apiServer) dashboard(writer http.ResponseWriter, request *http.Request) {
	var users, loadouts, reports int
	err := server.pool.QueryRow(request.Context(), `SELECT (SELECT count(*) FROM users WHERE status='active'),(SELECT count(*) FROM loadouts WHERE status='published'),(SELECT count(*) FROM loadout_reports WHERE status='open')`).Scan(&users, &loadouts, &reports)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]int{"active_users": users, "published_loadouts": loadouts, "open_reports": reports})
}
func (server *apiServer) auditLogs(writer http.ResponseWriter, request *http.Request) {
	rows, err := server.pool.Query(request.Context(), `SELECT a.id,a.action,a.target_type,a.target_id,a.request_id,a.metadata,to_char(a.created_at,'YYYY-MM-DD"T"HH24:MI:SSOF'),u.username FROM admin_audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id ORDER BY a.id DESC LIMIT 200`)
	if err != nil {
		serverError(writer, request, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var action, targetType, targetID, reqID, created string
		var metadata json.RawMessage
		var username *string
		if err := rows.Scan(&id, &action, &targetType, &targetID, &reqID, &metadata, &created, &username); err != nil {
			serverError(writer, request, err)
			return
		}
		items = append(items, map[string]any{"id": id, "action": action, "target_type": targetType, "target_id": targetID, "request_id": reqID, "metadata": metadata, "created_at": created, "actor_username": username})
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}

var _ = errors.Is
