package httpapi

import "net/http"

func (server *apiServer) gameMeta(writer http.ResponseWriter, request *http.Request) {
	if !requireGame(writer, request, server.game) {
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"game": "mh3g", "schema_version": 1, "data_version": server.game.DataVersion()})
}

func (server *apiServer) gameEquipment(writer http.ResponseWriter, request *http.Request) {
	if !requireGame(writer, request, server.game) {
		return
	}
	items, err := server.game.SearchEquipment(request.Context(), request.URL.Query().Get("slot"), request.URL.Query().Get("q"), parseLimit(request))
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items, "data_version": server.game.DataVersion()})
}
func (server *apiServer) gameDecorations(writer http.ResponseWriter, request *http.Request) {
	if !requireGame(writer, request, server.game) {
		return
	}
	items, err := server.game.SearchDecorations(request.Context(), request.URL.Query().Get("q"), parseLimit(request))
	if err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}
func (server *apiServer) gameSkills(writer http.ResponseWriter, request *http.Request) {
	if !requireGame(writer, request, server.game) {
		return
	}
	items, err := server.game.SearchSkills(request.Context(), request.URL.Query().Get("q"), parseLimit(request))
	if err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}
func (server *apiServer) gameCharmClasses(writer http.ResponseWriter, request *http.Request) {
	if !requireGame(writer, request, server.game) {
		return
	}
	items, err := server.game.CharmClasses(request.Context())
	if err != nil {
		serverError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": items})
}
func (server *apiServer) gameCharmRules(writer http.ResponseWriter, request *http.Request) {
	if !requireGame(writer, request, server.game) {
		return
	}
	value, err := server.game.CharmRules(request.Context(), parseInteger(request, "class_id", 0), parseInteger(request, "skill1_id", 0), parseInteger(request, "skill2_id", 0))
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, value)
}
