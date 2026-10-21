package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"marketplace/core/internal/apperr"
	"marketplace/core/internal/auth"
	"marketplace/core/internal/identity"
)

type contextKey int

const (
	actorKey contextKey = iota
	bagKey
)

type requestBag struct {
	RequestID string
	UserID    *string
}

func accessLog(out io.Writer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bag := &requestBag{}
			ctx := context.WithValue(r.Context(), bagKey, bag)
			recorder := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			body := readBody(r)
			started := time.Now()
			next.ServeHTTP(recorder, r.WithContext(ctx))
			record := accessRecord{
				RequestID:   bag.RequestID,
				Method:      r.Method,
				Endpoint:    r.URL.Path,
				StatusCode:  recorder.status,
				DurationMS:  time.Since(started).Milliseconds(),
				UserID:      bag.UserID,
				Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
				RequestBody: loggedBody(r.Method, body),
			}
			line, err := json.Marshal(record)
			if err != nil {
				slog.Error("marshal access log", "err", err)
				return
			}
			_, _ = out.Write(append(line, '\n'))
		})
	}
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		if bag, ok := r.Context().Value(bagKey).(*requestBag); ok {
			bag.RequestID = id
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic", "err", recovered)
				writeError(w, apperr.New(http.StatusInternalServerError, "INTERNAL_ERROR", "внутренняя ошибка"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func authenticate(service *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublic(r) {
				next.ServeHTTP(w, r)
				return
			}
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeError(w, apperr.New(http.StatusUnauthorized, "TOKEN_INVALID", "токен недействителен"))
				return
			}
			actor, err := service.ParseAccess(token)
			if err != nil {
				writeError(w, err)
				return
			}
			if bag, ok := r.Context().Value(bagKey).(*requestBag); ok {
				id := actor.ID
				bag.UserID = &id
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey, actor)))
		})
	}
}

func requireActor(r *http.Request) (identity.Actor, error) {
	actor, ok := r.Context().Value(actorKey).(identity.Actor)
	if !ok || actor.ID == "" {
		return identity.Actor{}, apperr.New(http.StatusUnauthorized, "TOKEN_INVALID", "токен недействителен")
	}
	return actor, nil
}

func isPublic(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		return true
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/auth/"):
		return true
	default:
		return false
	}
}

func bearerToken(header string) (string, bool) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

type accessRecord struct {
	RequestID   string  `json:"request_id"`
	Method      string  `json:"method"`
	Endpoint    string  `json:"endpoint"`
	StatusCode  int     `json:"status_code"`
	DurationMS  int64   `json:"duration_ms"`
	UserID      *string `json:"user_id"`
	Timestamp   string  `json:"timestamp"`
	RequestBody any     `json:"request_body,omitempty"`
}

func readBody(r *http.Request) []byte {
	if r.Body == nil || r.Method == http.MethodGet {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))
	return payload
}

func loggedBody(method string, payload []byte) any {
	if method != http.MethodPost && method != http.MethodPut && method != http.MethodDelete {
		return nil
	}
	if len(payload) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return string(payload)
	}
	return maskValue(decoded)
}

func maskValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if sensitive(key) {
				typed[key] = "***"
				continue
			}
			typed[key] = maskValue(nested)
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = maskValue(typed[i])
		}
		return typed
	default:
		return value
	}
}

func sensitive(key string) bool {
	switch strings.ToLower(key) {
	case "password", "password_hash", "refresh_token", "access_token", "token":
		return true
	default:
		return false
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(payload []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}
