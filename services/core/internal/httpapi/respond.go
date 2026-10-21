package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"marketplace/core/internal/api"
	"marketplace/core/internal/apperr"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	if target, ok := apperr.As(err); ok {
		response := api.ErrorResponse{
			ErrorCode: api.ErrorResponseErrorCode(target.Code),
			Message:   target.Message,
		}
		if target.Details != nil {
			details := map[string]any(target.Details)
			response.Details = &details
		}
		writeJSON(w, target.Status, response)
		return
	}
	slog.Error("unhandled error", "err", err)
	writeJSON(w, http.StatusInternalServerError, api.ErrorResponse{
		ErrorCode: api.INTERNALERROR,
		Message:   "внутренняя ошибка",
	})
}

func routeNotFound() error {
	return apperr.New(http.StatusNotFound, "NOT_FOUND", "маршрут не найден")
}

func methodNotAllowed() error {
	return apperr.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "метод не поддерживается")
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields("body", err.Error()))
	}
	return nil
}
