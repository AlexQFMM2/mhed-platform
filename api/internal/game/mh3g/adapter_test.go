package mh3g

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadoutFingerprintIgnoresNameAndDecorationOrder(t *testing.T) {
	first := Loadout{Name: "甲", Weapon: Piece{Decorations: []int{3, 1, 2}}, Armor: Armor{Head: Piece{Decorations: []int{2, 1}}}}
	second := Loadout{Name: "乙", Weapon: Piece{Decorations: []int{2, 3, 1}}, Armor: Armor{Head: Piece{Decorations: []int{1, 2}}}}
	if !reflect.DeepEqual(loadoutFingerprint(first), loadoutFingerprint(second)) {
		t.Fatal("equivalent equipment content produced different fingerprints")
	}
	if first.Name != "甲" || !reflect.DeepEqual(first.Weapon.Decorations, []int{3, 1, 2}) {
		t.Fatal("fingerprint normalization mutated the submitted loadout")
	}
}

func testAdapter(t *testing.T) *Adapter {
	directory := os.Getenv("MHED_TEST_GAME_DATA")
	if directory == "" {
		t.Skip("MHED_TEST_GAME_DATA is not configured")
	}
	adapter, err := Open(filepath.Join(directory, "mh3g.sqlite"), filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter
}

func naturalLoadout(adapter *Adapter) Loadout {
	piece := func(saveType int) Piece { return Piece{SaveType: saveType, SaveID: 1, Decorations: []int{}} }
	return Loadout{Schema: "MH_LOADOUT", SchemaVersion: 1, Game: "mh3g", DataVersion: adapter.DataVersion(), Name: "回归测试", Gender: "male", Weapon: piece(7), Armor: Armor{Head: piece(5), Chest: piece(1), Arms: piece(2), Waist: piece(3), Legs: piece(4)}, Charm: Charm{ClassID: 1, Slots: 0, Skill1ID: 2, Skill1Points: 1, Skill2ID: 0, Skill2Points: 0, Decorations: []int{}}}
}

func TestValidateNaturalAndAdvisoryCharm(t *testing.T) {
	adapter := testAdapter(t)
	value := naturalLoadout(adapter)
	result, err := adapter.Validate(context.Background(), value, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Equipment) != 7 {
		t.Fatalf("equipment index count = %d", len(result.Equipment))
	}
	if result.IsLegal != (len(result.Summary.Diagnostics) == 0) {
		t.Fatal("legality does not match server diagnostics")
	}
	value.Charm.Skill2ID = 11
	value.Charm.Skill2Points = 99
	result, err = adapter.Validate(context.Background(), value, true)
	if err != nil {
		t.Fatalf("advisory charm was rejected: %v", err)
	}
	found := false
	for _, item := range result.Summary.Diagnostics {
		if item.Code == "CHARM_SKILL_POINTS_INVALID" || item.Code == "CHARM_COMBINATION_NOT_GENERATED" {
			found = true
		}
	}
	if !found {
		t.Fatal("unnatural charm did not produce a diagnostic")
	}
	if result.IsLegal {
		t.Fatal("loadout with advisory diagnostics was marked legal")
	}
}

func TestValidateRejectsUnknownEquipment(t *testing.T) {
	adapter := testAdapter(t)
	value := naturalLoadout(adapter)
	value.Weapon.SaveID = 65535
	if _, err := adapter.Validate(context.Background(), value, true); err == nil {
		t.Fatal("unknown equipment ID was accepted")
	}
}
