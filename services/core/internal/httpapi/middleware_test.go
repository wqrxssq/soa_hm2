package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"marketplace/core/internal/api"
	"marketplace/core/internal/config"
)

func TestAccessLogMasksPassword(t *testing.T) {
	var logs bytes.Buffer
	handler := accessLog(&logs)(requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})))
	body := []byte(`{"email":"a@b.c","password":"Sup3rSecret","role":"USER"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
	if strings.Contains(logs.String(), "Sup3rSecret") {
		t.Fatalf("password leaked into log: %s", logs.String())
	}
	var record accessRecord
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Method != http.MethodPost || record.Endpoint != "/auth/register" || record.StatusCode != http.StatusCreated {
		t.Fatalf("record = %+v", record)
	}
	if record.UserID != nil {
		t.Fatal("anonymous request must log null user_id")
	}
	if record.RequestID == "" || record.Timestamp == "" {
		t.Fatalf("record = %+v", record)
	}
	payload, ok := record.RequestBody.(map[string]any)
	if !ok || payload["password"] != "***" {
		t.Fatalf("body = %#v", record.RequestBody)
	}
}

func TestValidatorRejectsPriceBeforeHandler(t *testing.T) {
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	validator, err := newValidator(spec)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := validator(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	body := []byte(`{"name":"Book","price":0,"stock":1,"category":"books","status":"ACTIVE"}`)
	request := httptest.NewRequest(http.MethodPost, "/products", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if called {
		t.Fatal("invalid request reached the handler")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response api.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ErrorCode != api.VALIDATIONERROR {
		t.Fatalf("code = %s", response.ErrorCode)
	}
	if response.Details == nil || !strings.Contains(recorder.Body.String(), "price") {
		t.Fatalf("details = %s", recorder.Body.String())
	}
}

func TestValidatorRejectsUnsupportedMethod(t *testing.T) {
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	validator, err := newValidator(spec)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := validator(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodPost, "/health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if called {
		t.Fatal("unsupported method reached the handler")
	}
	assertError(t, recorder, http.StatusMethodNotAllowed, api.METHODNOTALLOWED)
}

func TestUnknownRouteUsesContractError(t *testing.T) {
	handler, err := New(config.Config{
		JWTSecret:      []byte("test-secret"),
		AccessTTL:      time.Minute,
		RefreshTTL:     time.Hour,
		OrderRateLimit: time.Minute,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/missing", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertError(t, recorder, http.StatusNotFound, api.NOTFOUND)
}

func assertError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code api.ErrorResponseErrorCode) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response api.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ErrorCode != code || response.Message == "" {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
