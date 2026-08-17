package mh3g

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (adapter *Adapter) SearchEquipment(ctx context.Context, slot, query string, limit int) ([]Candidate, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	query = "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	if slot == "weapon" {
		rows, err := adapter.db.QueryContext(ctx, `SELECT save_type,save_id,name_cn,name_en,coalesce(slots,-1),coalesce(rarity,0),mapping_status
			FROM weapons WHERE lower(name_cn) LIKE ? OR lower(name_en) LIKE ? ORDER BY name_cn,save_type,save_id LIMIT ?`, query, query, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		values := []Candidate{}
		for rows.Next() {
			var row Candidate
			var status string
			if err := rows.Scan(&row.SaveType, &row.SaveID, &row.Name, &row.English, &row.Slots, &row.Rarity, &status); err != nil {
				return nil, err
			}
			row.Confirmed = status == "confirmed"
			values = append(values, row)
		}
		return values, rows.Err()
	}
	types := map[string]int{"head": 5, "chest": 1, "arms": 2, "waist": 3, "legs": 4}
	saveType, ok := types[slot]
	if !ok {
		return nil, errors.New("invalid equipment slot")
	}
	rows, err := adapter.db.QueryContext(ctx, `SELECT save_type,save_id,name_cn,name_en,coalesce(slots,-1),coalesce(rarity,0),coalesce(combat,0),coalesce(gender,0),mapping_status
		FROM armors WHERE save_type=? AND (lower(name_cn) LIKE ? OR lower(name_en) LIKE ?) ORDER BY name_cn,save_id LIMIT ?`, saveType, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Candidate{}
	for rows.Next() {
		var row Candidate
		var status string
		if err := rows.Scan(&row.SaveType, &row.SaveID, &row.Name, &row.English, &row.Slots, &row.Rarity, &row.Combat, &row.Gender, &status); err != nil {
			return nil, err
		}
		row.Confirmed = status == "confirmed"
		values = append(values, row)
	}
	return values, rows.Err()
}

func (adapter *Adapter) SearchDecorations(ctx context.Context, query string, limit int) ([]Candidate, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	query = "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	rows, err := adapter.db.QueryContext(ctx, `SELECT sd.save_id,sd.name_cn,sd.name_en,coalesce((SELECT slots FROM decorations d WHERE d.save_id=sd.save_id ORDER BY d.mapping_status='confirmed' DESC LIMIT 1),-1),sd.mapping_status FROM save_decorations sd WHERE lower(sd.name_cn) LIKE ? OR lower(sd.name_en) LIKE ? ORDER BY sd.name_cn,sd.save_id LIMIT ?`, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Candidate{}
	for rows.Next() {
		var row Candidate
		var status string
		if err := rows.Scan(&row.SaveID, &row.Name, &row.English, &row.Slots, &status); err != nil {
			return nil, err
		}
		row.Confirmed = status == "confirmed"
		values = append(values, row)
	}
	return values, rows.Err()
}

func (adapter *Adapter) SearchSkills(ctx context.Context, query string, limit int) ([]NamedID, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	needle := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	values := []NamedID{{ID: 0, Name: "无", English: "None"}}
	rows, err := adapter.db.QueryContext(ctx, `SELECT id,name_cn,name_en FROM skill_trees WHERE lower(name_cn) LIKE ? OR lower(name_en) LIKE ? ORDER BY name_cn,id LIMIT ?`, needle, needle, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row NamedID
		if err := rows.Scan(&row.ID, &row.Name, &row.English); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	return values, rows.Err()
}

func (adapter *Adapter) CharmClasses(ctx context.Context) ([]NamedID, error) {
	rows, err := adapter.db.QueryContext(ctx, `SELECT save_id,name_cn,name_en FROM charm_classes ORDER BY save_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []NamedID{}
	for rows.Next() {
		var row NamedID
		if err := rows.Scan(&row.ID, &row.Name, &row.English); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	return values, rows.Err()
}

type CharmRules struct {
	Slots        []int `json:"slots"`
	Skill1Points []int `json:"skill1_points"`
	Skill2Points []int `json:"skill2_points"`
	PairExists   bool  `json:"pair_exists"`
}

func (adapter *Adapter) CharmRules(ctx context.Context, classID, skill1ID, skill2ID int) (CharmRules, error) {
	value := CharmRules{Slots: []int{}, Skill1Points: []int{}, Skill2Points: []int{}}
	var exists int
	if err := adapter.db.QueryRowContext(ctx, `SELECT count(*) FROM charm_classes WHERE save_id=?`, classID).Scan(&exists); err != nil || exists == 0 {
		return value, fmt.Errorf("charm class not found")
	}
	read := func(query string, args ...any) ([]int, error) {
		rows, err := adapter.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []int{}
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return out, rows.Err()
	}
	var err error
	value.Slots, err = read(`SELECT DISTINCT slots FROM charm_combinations WHERE class_id=? ORDER BY slots`, classID)
	if err != nil {
		return value, err
	}
	value.Skill1Points, err = read(`SELECT DISTINCT skill1_points FROM charm_combinations WHERE class_id=? AND skill1_id=? ORDER BY skill1_points`, classID, skill1ID)
	if err != nil {
		return value, err
	}
	value.Skill2Points, err = read(`SELECT DISTINCT skill2_points FROM charm_combinations WHERE class_id=? AND skill2_id=? ORDER BY skill2_points`, classID, skill2ID)
	if err != nil {
		return value, err
	}
	if skill1ID == 0 || skill2ID == 0 {
		value.PairExists = true
	} else {
		var count int
		if err := adapter.db.QueryRowContext(ctx, `SELECT count(*) FROM charm_combinations WHERE class_id=? AND skill1_id=? AND skill2_id=?`, classID, skill1ID, skill2ID).Scan(&count); err != nil {
			return value, err
		}
		value.PairExists = count > 0
	}
	return value, nil
}
