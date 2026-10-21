package auth

import (
	"testing"
	"time"

	"marketplace/core/internal/apperr"
	"marketplace/core/internal/identity"
)

func TestParseAccess(t *testing.T) {
	svc := NewService(nil, []byte("secret"), time.Minute, time.Hour)
	token, err := svc.sign("user-1", identity.RoleUser, "access", "", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	actor, err := svc.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	if actor.ID != "user-1" || actor.Role != identity.RoleUser {
		t.Fatalf("actor = %+v", actor)
	}

	expired, err := svc.sign("user-1", identity.RoleUser, "access", "", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ParseAccess(expired)
	assertCode(t, err, "TOKEN_EXPIRED")

	refresh, err := svc.sign("user-1", identity.RoleUser, "refresh", "jti", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ParseAccess(refresh)
	assertCode(t, err, "TOKEN_INVALID")

	_, err = svc.ParseAccess("not-a-token")
	assertCode(t, err, "TOKEN_INVALID")
}

func TestParseRefreshRejectsAccessToken(t *testing.T) {
	svc := NewService(nil, []byte("secret"), time.Minute, time.Hour)
	token, err := svc.sign("user-1", identity.RoleSeller, "access", "", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.parse(token, "refresh"); err == nil {
		t.Fatal("access token must not parse as refresh")
	}
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	got, ok := apperr.As(err)
	if !ok || got.Code != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
}
