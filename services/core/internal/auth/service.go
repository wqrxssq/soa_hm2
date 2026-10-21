package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"marketplace/core/internal/apperr"
	"marketplace/core/internal/identity"
	"marketplace/core/internal/store"
)

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

type Service struct {
	store      *store.Store
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewService(st *store.Store, secret []byte, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{store: st, secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

type claims struct {
	Role string `json:"role"`
	Use  string `json:"use"`
	jwt.RegisteredClaims
}

func (s *Service) Register(ctx context.Context, email, password, role string) (store.User, error) {
	switch role {
	case identity.RoleUser, identity.RoleSeller, identity.RoleAdmin:
	default:
		return store.User{}, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields("role", "enum"))
	}
	email = normalizeEmail(email)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return store.User{}, err
	}
	user, err := store.InsertUser(ctx, s.store.Query(), email, string(hash), role)
	if field, ok := store.IsUnique(err); ok {
		return store.User{}, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields(field, "already exists"))
	}
	if err != nil {
		return store.User{}, err
	}
	user.PasswordHash = ""
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	user, err := store.GetUserByEmail(ctx, s.store.Query(), normalizeEmail(email))
	if errors.Is(err, store.ErrNotFound) {
		return TokenPair{}, invalidCredentials()
	}
	if err != nil {
		return TokenPair{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return TokenPair{}, invalidCredentials()
	}
	return s.issue(ctx, s.store.Query(), user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	parsed, err := s.parse(refreshToken, "refresh")
	if err != nil {
		return TokenPair{}, apperr.New(http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "refresh token недействителен")
	}
	var pair TokenPair
	err = s.store.WithinTx(ctx, func(ctx context.Context, q store.DBTX) error {
		if err := store.ConsumeRefresh(ctx, q, parsed.ID, parsed.Subject); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return apperr.New(http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "refresh token недействителен")
			}
			return err
		}
		user, err := store.GetUserByID(ctx, q, parsed.Subject)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return apperr.New(http.StatusUnauthorized, "REFRESH_TOKEN_INVALID", "refresh token недействителен")
			}
			return err
		}
		pair, err = s.issue(ctx, q, user)
		return err
	})
	return pair, err
}

func (s *Service) ParseAccess(token string) (identity.Actor, error) {
	parsed, err := s.parse(token, "access")
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return identity.Actor{}, apperr.New(http.StatusUnauthorized, "TOKEN_EXPIRED", "токен истёк")
		}
		return identity.Actor{}, apperr.New(http.StatusUnauthorized, "TOKEN_INVALID", "токен недействителен")
	}
	if parsed.Subject == "" || parsed.Role == "" {
		return identity.Actor{}, apperr.New(http.StatusUnauthorized, "TOKEN_INVALID", "токен недействителен")
	}
	return identity.Actor{ID: parsed.Subject, Role: parsed.Role}, nil
}

func (s *Service) issue(ctx context.Context, q store.DBTX, user store.User) (TokenPair, error) {
	access, err := s.sign(user.ID, user.Role, "access", "", time.Now().UTC().Add(s.accessTTL))
	if err != nil {
		return TokenPair{}, err
	}
	jti := uuid.NewString()
	refreshExp := time.Now().UTC().Add(s.refreshTTL)
	refresh, err := s.sign(user.ID, user.Role, "refresh", jti, refreshExp)
	if err != nil {
		return TokenPair{}, err
	}
	if err := store.InsertRefresh(ctx, q, jti, user.ID, refreshExp); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(s.accessTTL.Seconds()),
	}, nil
}

func (s *Service) sign(userID, role, use, jti string, expires time.Time) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: role,
		Use:  use,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	})
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

func (s *Service) parse(token, use string) (*claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	parsedClaims, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid || parsedClaims.Use != use {
		return nil, fmt.Errorf("invalid token")
	}
	return parsedClaims, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func invalidCredentials() error {
	return apperr.New(http.StatusUnauthorized, "INVALID_CREDENTIALS", "неверный email или пароль")
}
