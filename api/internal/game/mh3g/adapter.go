package mh3g

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

const Game = "mh3g"

type Manifest struct {
	Format   string `json:"format"`
	Database struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	} `json:"database"`
}

type Adapter struct {
	db          *sql.DB
	dataVersion string
}

type Piece struct {
	SaveType    int   `json:"save_type"`
	SaveID      int   `json:"save_id"`
	Decorations []int `json:"decorations"`
}
type Charm struct {
	ClassID      int   `json:"class_id"`
	Slots        int   `json:"slots"`
	Skill1ID     int   `json:"skill1_id"`
	Skill1Points int   `json:"skill1_points"`
	Skill2ID     int   `json:"skill2_id"`
	Skill2Points int   `json:"skill2_points"`
	Decorations  []int `json:"decorations"`
}
type Armor struct {
	Head  Piece `json:"head"`
	Chest Piece `json:"chest"`
	Arms  Piece `json:"arms"`
	Waist Piece `json:"waist"`
	Legs  Piece `json:"legs"`
}
type Loadout struct {
	Schema        string `json:"schema"`
	SchemaVersion int    `json:"schema_version"`
	Game          string `json:"game"`
	DataVersion   string `json:"data_version"`
	Name          string `json:"name"`
	Gender        string `json:"gender"`
	Weapon        Piece  `json:"weapon"`
	Armor         Armor  `json:"armor"`
	Charm         Charm  `json:"charm"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}
type SkillSummary struct {
	SkillTreeID   int            `json:"skill_tree_id"`
	Name          string         `json:"name"`
	Points        int            `json:"points"`
	Columns       map[string]int `json:"columns"`
	ActiveSkillID *int           `json:"active_skill_id,omitempty"`
	ActiveSkill   string         `json:"active_skill,omitempty"`
}
type Summary struct {
	BaseDefense   int            `json:"base_defense"`
	MaxDefense    int            `json:"max_defense"`
	WeaponDefense int            `json:"weapon_defense"`
	FireRes       int            `json:"fire_res"`
	WaterRes      int            `json:"water_res"`
	IceRes        int            `json:"ice_res"`
	ThunderRes    int            `json:"thunder_res"`
	DragonRes     int            `json:"dragon_res"`
	TotalSlots    int            `json:"total_slots"`
	UsedSlots     int            `json:"used_slots"`
	Skills        []SkillSummary `json:"skills"`
	Diagnostics   []Diagnostic   `json:"diagnostics"`
}
type EquipmentIndex struct {
	Slot     string
	SaveType int
	SaveID   int
}
type SkillIndex struct {
	SkillTreeID   int
	Points        int
	ActiveSkillID *int
}
type Result struct {
	Payload   Loadout          `json:"payload"`
	Summary   Summary          `json:"summary"`
	IsLegal   bool             `json:"is_legal"`
	Canonical []byte           `json:"-"`
	Hash      []byte           `json:"-"`
	BuildHash []byte           `json:"-"`
	Equipment []EquipmentIndex `json:"-"`
	Skills    []SkillIndex     `json:"-"`
}
type Candidate struct {
	SaveType  int    `json:"save_type,omitempty"`
	SaveID    int    `json:"save_id"`
	Name      string `json:"name"`
	English   string `json:"english"`
	Slots     int    `json:"slots"`
	Rarity    int    `json:"rarity,omitempty"`
	Combat    int    `json:"combat,omitempty"`
	Gender    int    `json:"gender,omitempty"`
	Confirmed bool   `json:"confirmed"`
}
type NamedID struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	English string `json:"english"`
}

func Open(databasePath, manifestPath string) (*Adapter, error) {
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read game manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parse game manifest: %w", err)
	}
	if manifest.Format != "mh3g-save-editor-data-manifest-v1" {
		return nil, errors.New("unsupported MH3G manifest")
	}
	file, err := os.Open(databasePath)
	if err != nil {
		return nil, fmt.Errorf("open game database: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if size != manifest.Database.Bytes || digest != manifest.Database.SHA256 {
		return nil, errors.New("MH3G database hash does not match manifest")
	}
	db, err := sql.Open("sqlite", "file:"+databasePath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Adapter{db: db, dataVersion: digest}, nil
}
func (adapter *Adapter) Close() error                   { return adapter.db.Close() }
func (adapter *Adapter) Ping(ctx context.Context) error { return adapter.db.PingContext(ctx) }
func (adapter *Adapter) DataVersion() string            { return adapter.dataVersion }

func Decode(data []byte) (Loadout, error) {
	var value Loadout
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("parse MH_LOADOUT: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return value, errors.New("MH_LOADOUT contains trailing data")
	}
	return value, nil
}

func (adapter *Adapter) Validate(ctx context.Context, loadout Loadout, requireComplete bool) (Result, error) {
	loadout.Name = strings.TrimSpace(loadout.Name)
	if loadout.Schema != "MH_LOADOUT" || loadout.SchemaVersion != 1 || loadout.Game != Game {
		return Result{}, errors.New("只支持 MH_LOADOUT v1 / mh3g")
	}
	if loadout.DataVersion != adapter.dataVersion {
		return Result{}, errors.New("配装数据版本与服务器 MH3G 数据不一致")
	}
	if len([]rune(loadout.Name)) < 1 || len([]rune(loadout.Name)) > 40 {
		return Result{}, errors.New("配装名称必须为 1～40 个字符")
	}
	if loadout.Gender != "male" && loadout.Gender != "female" {
		return Result{}, errors.New("配装性别无效")
	}
	pairs := []struct {
		name     string
		piece    Piece
		expected int
	}{
		{"weapon", loadout.Weapon, 0}, {"head", loadout.Armor.Head, 5}, {"chest", loadout.Armor.Chest, 1}, {"arms", loadout.Armor.Arms, 2}, {"waist", loadout.Armor.Waist, 3}, {"legs", loadout.Armor.Legs, 4},
	}
	result := Result{Payload: loadout, Summary: Summary{Skills: []SkillSummary{}, Diagnostics: []Diagnostic{}}}
	points := map[int]map[string]int{}
	weaponRanged := false
	for _, entry := range pairs {
		if requireComplete && (entry.piece.SaveType == 0 || entry.piece.SaveID == 0) {
			return Result{}, fmt.Errorf("请选择%s", slotChinese(entry.name))
		}
		if entry.name == "weapon" {
			if entry.piece.SaveType < 7 || entry.piece.SaveType > 19 || entry.piece.SaveType == 12 {
				return Result{}, errors.New("武器类型无效")
			}
			weaponRanged = entry.piece.SaveType == 11 || entry.piece.SaveType == 13 || entry.piece.SaveType == 17
		} else if entry.piece.SaveType != entry.expected {
			return Result{}, fmt.Errorf("%s的装备类型与部位不一致", slotChinese(entry.name))
		}
		if err := validateDecorationsRange(entry.piece.Decorations); err != nil {
			return Result{}, fmt.Errorf("%s：%w", slotChinese(entry.name), err)
		}
		detail, err := adapter.equipment(ctx, entry.piece.SaveType, entry.piece.SaveID)
		if err != nil {
			return Result{}, fmt.Errorf("%s ID 无法解析", slotChinese(entry.name))
		}
		result.Summary.TotalSlots += max(detail.Slots, 0)
		if entry.name == "weapon" {
			result.Summary.WeaponDefense = detail.Defense
			result.Summary.BaseDefense += detail.Defense
			result.Summary.MaxDefense += detail.Defense
		} else {
			result.Summary.BaseDefense += detail.BaseDefense
			result.Summary.MaxDefense += detail.MaxDefense
			result.Summary.FireRes += detail.FireRes
			result.Summary.WaterRes += detail.WaterRes
			result.Summary.IceRes += detail.IceRes
			result.Summary.ThunderRes += detail.ThunderRes
			result.Summary.DragonRes += detail.DragonRes
			requiredCombat := 1
			if weaponRanged {
				requiredCombat = 2
			}
			if detail.Combat > 0 && detail.Combat != requiredCombat {
				result.Summary.Diagnostics = append(result.Summary.Diagnostics, diagnostic("ARMOR_COMBAT_MISMATCH", entry.name, slotChinese(entry.name)+"与当前武器的近战/远程类型不适用。"))
			}
			requiredGender := 1
			if loadout.Gender == "female" {
				requiredGender = 2
			}
			if detail.Gender > 0 && detail.Gender != requiredGender {
				result.Summary.Diagnostics = append(result.Summary.Diagnostics, diagnostic("ARMOR_GENDER_MISMATCH", entry.name, slotChinese(entry.name)+"与当前配装性别不适用。"))
			}
		}
		if !detail.Confirmed {
			result.Summary.Diagnostics = append(result.Summary.Diagnostics, diagnostic("EQUIPMENT_PARAMETERS_UNKNOWN", entry.name, detail.Name+"的原生参数映射尚未完全确认。"))
		}
		for skillID, value := range detail.SkillPoints {
			addPoint(points, skillID, entry.name, value)
		}
		used, err := adapter.addDecorations(ctx, points, entry.name, entry.piece.Decorations)
		if err != nil {
			return Result{}, err
		}
		result.Summary.UsedSlots += used
		if detail.Slots >= 0 && used > detail.Slots {
			result.Summary.Diagnostics = append(result.Summary.Diagnostics, diagnostic("SLOT_CAPACITY_EXCEEDED", entry.name, fmt.Sprintf("%s装饰珠共占 %d 孔，但装备只有 %d 孔。", slotChinese(entry.name), used, detail.Slots)))
		}
		result.Equipment = append(result.Equipment, EquipmentIndex{entry.name, entry.piece.SaveType, entry.piece.SaveID})
	}
	if err := adapter.validateCharm(ctx, loadout.Charm, &result.Summary); err != nil {
		return Result{}, err
	}
	result.Summary.TotalSlots += loadout.Charm.Slots
	if loadout.Charm.Skill1ID > 0 {
		addPoint(points, loadout.Charm.Skill1ID, "charm", loadout.Charm.Skill1Points)
	}
	if loadout.Charm.Skill2ID > 0 {
		addPoint(points, loadout.Charm.Skill2ID, "charm", loadout.Charm.Skill2Points)
	}
	used, err := adapter.addDecorations(ctx, points, "charm", loadout.Charm.Decorations)
	if err != nil {
		return Result{}, err
	}
	result.Summary.UsedSlots += used
	if used > loadout.Charm.Slots {
		result.Summary.Diagnostics = append(result.Summary.Diagnostics, diagnostic("SLOT_CAPACITY_EXCEEDED", "charm", fmt.Sprintf("护石装饰珠共占 %d 孔，但护石只有 %d 孔。", used, loadout.Charm.Slots)))
	}
	result.Equipment = append(result.Equipment, EquipmentIndex{"charm", 6, loadout.Charm.ClassID})
	applyTorsoUp(points)
	if err := adapter.finishSkills(ctx, points, &result); err != nil {
		return Result{}, err
	}
	canonical, err := json.Marshal(result.Payload)
	if err != nil {
		return Result{}, err
	}
	digest := sha256.Sum256(canonical)
	fingerprintBytes, err := json.Marshal(loadoutFingerprint(result.Payload))
	if err != nil {
		return Result{}, err
	}
	buildDigest := sha256.Sum256(fingerprintBytes)
	result.Canonical = canonical
	result.Hash = digest[:]
	result.BuildHash = buildDigest[:]
	result.IsLegal = len(result.Summary.Diagnostics) == 0
	return result, nil
}

func loadoutFingerprint(value Loadout) Loadout {
	value.Name = ""
	value.Weapon.Decorations = sortedDecorations(value.Weapon.Decorations)
	value.Armor.Head.Decorations = sortedDecorations(value.Armor.Head.Decorations)
	value.Armor.Chest.Decorations = sortedDecorations(value.Armor.Chest.Decorations)
	value.Armor.Arms.Decorations = sortedDecorations(value.Armor.Arms.Decorations)
	value.Armor.Waist.Decorations = sortedDecorations(value.Armor.Waist.Decorations)
	value.Armor.Legs.Decorations = sortedDecorations(value.Armor.Legs.Decorations)
	value.Charm.Decorations = sortedDecorations(value.Charm.Decorations)
	return value
}

func sortedDecorations(values []int) []int {
	result := append([]int(nil), values...)
	sort.Ints(result)
	return result
}

type equipmentDetail struct {
	Name                                                                                                      string
	Slots, Combat, Gender, Defense, BaseDefense, MaxDefense, FireRes, WaterRes, IceRes, ThunderRes, DragonRes int
	Confirmed                                                                                                 bool
	SkillPoints                                                                                               map[int]int
}

func (adapter *Adapter) equipment(ctx context.Context, saveType, saveID int) (equipmentDetail, error) {
	value := equipmentDetail{SkillPoints: map[int]int{}}
	if saveType >= 7 {
		var status string
		err := adapter.db.QueryRowContext(ctx, `SELECT name_cn,coalesce(slots,-1),coalesce(defense,0),mapping_status FROM weapons WHERE save_type=? AND save_id=?`, saveType, saveID).Scan(&value.Name, &value.Slots, &value.Defense, &status)
		value.Confirmed = status == "confirmed"
		return value, err
	}
	var status string
	var dexID int
	err := adapter.db.QueryRowContext(ctx, `SELECT name_cn,coalesce(slots,-1),coalesce(combat,0),coalesce(gender,0),coalesce(base_defense,0),coalesce(max_defense,0),coalesce(fire_res,0),coalesce(water_res,0),coalesce(ice_res,0),coalesce(thunder_res,0),coalesce(dragon_res,0),mapping_status,dex_id FROM armors WHERE save_type=? AND save_id=?`, saveType, saveID).Scan(&value.Name, &value.Slots, &value.Combat, &value.Gender, &value.BaseDefense, &value.MaxDefense, &value.FireRes, &value.WaterRes, &value.IceRes, &value.ThunderRes, &value.DragonRes, &status, &dexID)
	if err != nil {
		return value, err
	}
	value.Confirmed = status == "confirmed"
	rows, err := adapter.db.QueryContext(ctx, `SELECT skill_tree_id,points FROM armor_skill_points WHERE armor_dex_id=?`, dexID)
	if err != nil {
		return value, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, p int
		if err := rows.Scan(&id, &p); err != nil {
			return value, err
		}
		value.SkillPoints[id] += p
	}
	return value, rows.Err()
}

func (adapter *Adapter) addDecorations(ctx context.Context, points map[int]map[string]int, slot string, decorations []int) (int, error) {
	used := 0
	for _, id := range decorations {
		if id == 0 {
			continue
		}
		var dex, slots int
		var status string
		err := adapter.db.QueryRowContext(ctx, `SELECT dex_id,slots,mapping_status FROM decorations WHERE save_id=? ORDER BY mapping_status='confirmed' DESC LIMIT 1`, id).Scan(&dex, &slots, &status)
		if err != nil {
			return 0, fmt.Errorf("装饰珠 ID %d 无法解析", id)
		}
		used += slots
		rows, err := adapter.db.QueryContext(ctx, `SELECT skill_tree_id,points FROM decoration_skill_points WHERE decoration_dex_id=?`, dex)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var skill, value int
			if err := rows.Scan(&skill, &value); err != nil {
				rows.Close()
				return 0, err
			}
			addPoint(points, skill, slot, value)
		}
		rows.Close()
	}
	return used, nil
}

func validateDecorationsRange(values []int) error {
	if len(values) > 3 {
		return errors.New("每件装备最多记录三个装饰珠")
	}
	for _, id := range values {
		if id < 0 || id > 65535 {
			return errors.New("装饰珠 ID 超出范围")
		}
	}
	return nil
}
func addPoint(values map[int]map[string]int, id int, slot string, points int) {
	if _, ok := values[id]; !ok {
		values[id] = map[string]int{}
	}
	values[id][slot] += points
}
func applyTorsoUp(values map[int]map[string]int) {
	markers := values[1]
	if len(markers) == 0 {
		return
	}
	for slot, count := range markers {
		if slot == "chest" || count <= 0 {
			continue
		}
		for id, row := range values {
			if id != 1 {
				row[slot] += row["chest"]
			}
		}
	}
}
func diagnostic(code, field, message string) Diagnostic {
	return Diagnostic{Code: code, Field: field, Message: message}
}
func slotChinese(slot string) string {
	return map[string]string{"weapon": "武器", "head": "头部", "chest": "胸部", "arms": "腕部", "waist": "腰部", "legs": "腿部", "charm": "护石"}[slot]
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (adapter *Adapter) finishSkills(ctx context.Context, values map[int]map[string]int, result *Result) error {
	ids := make([]int, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		total := 0
		for _, value := range values[id] {
			total += value
		}
		var name string
		if err := adapter.db.QueryRowContext(ctx, `SELECT name_cn FROM skill_trees WHERE id=?`, id).Scan(&name); err != nil {
			return err
		}
		var activeID *int
		activeName := ""
		rows, err := adapter.db.QueryContext(ctx, `SELECT id,points,name_cn FROM active_skills WHERE skill_tree_id=? ORDER BY points`, id)
		if err != nil {
			return err
		}
		bestPositive := -1 << 30
		bestNegative := 1 << 30
		for rows.Next() {
			var candidate, threshold int
			var candidateName string
			if err := rows.Scan(&candidate, &threshold, &candidateName); err != nil {
				rows.Close()
				return err
			}
			if threshold > 0 && total >= threshold && threshold > bestPositive {
				v := candidate
				activeID = &v
				activeName = candidateName
				bestPositive = threshold
			}
			if threshold < 0 && total <= threshold && threshold < bestNegative {
				v := candidate
				activeID = &v
				activeName = candidateName
				bestNegative = threshold
			}
		}
		rows.Close()
		columns := map[string]int{
			"weapon": values[id]["weapon"], "head": values[id]["head"],
			"chest": values[id]["chest"], "arms": values[id]["arms"],
			"waist": values[id]["waist"], "legs": values[id]["legs"],
			"charm": values[id]["charm"],
		}
		result.Summary.Skills = append(result.Summary.Skills, SkillSummary{
			SkillTreeID: id, Name: name, Points: total, Columns: columns,
			ActiveSkillID: activeID, ActiveSkill: activeName,
		})
		result.Skills = append(result.Skills, SkillIndex{id, total, activeID})
	}
	return nil
}

func (adapter *Adapter) validateCharm(ctx context.Context, charm Charm, summary *Summary) error {
	if charm.ClassID < 0 || charm.ClassID > 65535 || charm.Slots < 0 || charm.Slots > 3 || charm.Skill1ID < 0 || charm.Skill1ID > 255 || charm.Skill2ID < 0 || charm.Skill2ID > 255 || charm.Skill1Points < -128 || charm.Skill1Points > 127 || charm.Skill2Points < -128 || charm.Skill2Points > 127 {
		return errors.New("护石字段超出存档可表示范围")
	}
	if err := validateDecorationsRange(charm.Decorations); err != nil {
		return err
	}
	var className string
	if err := adapter.db.QueryRowContext(ctx, `SELECT name_cn FROM charm_classes WHERE save_id=?`, charm.ClassID).Scan(&className); err != nil {
		return errors.New("护石品级 ID 不存在")
	}
	for _, skill := range []int{charm.Skill1ID, charm.Skill2ID} {
		if skill == 0 {
			continue
		}
		var exists int
		if err := adapter.db.QueryRowContext(ctx, `SELECT count(*) FROM skill_trees WHERE id=?`, skill).Scan(&exists); err != nil || exists == 0 {
			return fmt.Errorf("护石技能 ID %d 不存在", skill)
		}
	}
	if charm.Skill1ID == 0 && charm.Skill1Points != 0 {
		summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_SKILL_POINTS_WITHOUT_SKILL", "skill1_points", fmt.Sprintf("第1技能为“无”时点数必须为 0，当前为 %d。", charm.Skill1Points)))
	}
	if charm.Skill2ID == 0 && charm.Skill2Points != 0 {
		summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_SKILL_POINTS_WITHOUT_SKILL", "skill2_points", fmt.Sprintf("第2技能为“无”时点数必须为 0，当前为 %d。", charm.Skill2Points)))
	}
	if charm.Skill1ID != 0 && charm.Skill1ID == charm.Skill2ID {
		summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_DUPLICATE_SKILL", "skill", "护石的两项技能系相同。"))
	}
	var slotExists int
	_ = adapter.db.QueryRowContext(ctx, `SELECT count(*) FROM charm_combinations WHERE class_id=? AND slots=?`, charm.ClassID, charm.Slots).Scan(&slotExists)
	if slotExists == 0 {
		summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_SLOT_NOT_GENERATED", "slots", fmt.Sprintf("%s不支持 %d 孔。", className, charm.Slots)))
	}
	for position, item := range []struct{ id, points int }{{charm.Skill1ID, charm.Skill1Points}, {charm.Skill2ID, charm.Skill2Points}} {
		column := "skill1_id"
		pointsColumn := "skill1_points"
		label := "第1"
		if position == 1 {
			column = "skill2_id"
			pointsColumn = "skill2_points"
			label = "第2"
		}
		query := fmt.Sprintf(`SELECT count(*) FROM charm_combinations WHERE class_id=? AND %s=?`, column)
		var exists int
		_ = adapter.db.QueryRowContext(ctx, query, charm.ClassID, item.id).Scan(&exists)
		if exists == 0 {
			summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_SKILL_POSITION_INVALID", column, fmt.Sprintf("该技能不能作为%s的%s技能。", className, label)))
		} else {
			query = fmt.Sprintf(`SELECT count(*) FROM charm_combinations WHERE class_id=? AND %s=? AND %s=?`, column, pointsColumn)
			var pointExists int
			_ = adapter.db.QueryRowContext(ctx, query, charm.ClassID, item.id, item.points).Scan(&pointExists)
			if pointExists == 0 {
				summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_SKILL_POINTS_INVALID", pointsColumn, fmt.Sprintf("%s的%s技能不支持 %d 点。", className, label, item.points)))
			}
		}
	}
	if charm.Skill1ID != 0 && charm.Skill2ID != 0 {
		var pair int
		_ = adapter.db.QueryRowContext(ctx, `SELECT count(*) FROM charm_combinations WHERE class_id=? AND skill1_id=? AND skill2_id=?`, charm.ClassID, charm.Skill1ID, charm.Skill2ID).Scan(&pair)
		if pair == 0 {
			summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_SKILL_PAIR_NOT_GENERATED", "skill", className+"不会自然生成当前技能对。"))
		}
	}
	var exact int
	_ = adapter.db.QueryRowContext(ctx, `SELECT count(*) FROM charm_combinations WHERE class_id=? AND slots=? AND skill1_id=? AND skill1_points=? AND skill2_id=? AND skill2_points=?`, charm.ClassID, charm.Slots, charm.Skill1ID, charm.Skill1Points, charm.Skill2ID, charm.Skill2Points).Scan(&exact)
	if exact == 0 {
		summary.Diagnostics = append(summary.Diagnostics, diagnostic("CHARM_COMBINATION_NOT_GENERATED", "charm", "该品级、孔数、技能与点数组合不在原生生成记录中。"))
	}
	return nil
}
