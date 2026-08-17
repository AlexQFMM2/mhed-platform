package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicLoadoutDoesNotExposeLoginIdentity(t *testing.T) {
	value := publicLoadout(loadoutView{ID: "loadout", OwnerUserID: "private-uuid", OwnerUsername: "private_login", OwnerPublicID: 42, OwnerNickname: "猎人"})
	bytes, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(bytes)
	if strings.Contains(text, "private_login") || strings.Contains(text, "private-uuid") || strings.Contains(text, "owner_username") || strings.Contains(text, "owner_user_id") {
		t.Fatalf("public loadout leaked login identity: %s", text)
	}
	if !strings.Contains(text, `"owner_public_id":42`) || !strings.Contains(text, `"owner_nickname":"猎人"`) {
		t.Fatalf("public loadout omitted public identity: %s", text)
	}
}

func TestPublicLoadoutFiltersPreserveAndConditions(t *testing.T) {
	request := httptest.NewRequest("GET", "/v1/desktop/loadouts?equipment=7%3A12&equipment=5%3A34&active_skill=101&active_skill=202", nil)
	recorder := httptest.NewRecorder()
	equipment, skills, ok := publicLoadoutFilters(recorder, request)
	if !ok || len(equipment) != 2 || equipment[0] != "7:12" || equipment[1] != "5:34" {
		t.Fatalf("unexpected equipment filters: %#v", equipment)
	}
	if len(skills) != 2 || skills[0] != 101 || skills[1] != 202 {
		t.Fatalf("unexpected skill filters: %#v", skills)
	}
}

func TestPublicLoadoutFiltersRejectInvalidValues(t *testing.T) {
	request := httptest.NewRequest("GET", "/v1/desktop/loadouts?equipment=not-an-item", nil)
	recorder := httptest.NewRecorder()
	if _, _, ok := publicLoadoutFilters(recorder, request); ok || recorder.Code != 400 {
		t.Fatalf("invalid equipment filter was accepted: status=%d", recorder.Code)
	}
}
