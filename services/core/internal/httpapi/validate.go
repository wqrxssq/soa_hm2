package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"marketplace/core/internal/apperr"
)

func newValidator(spec *openapi3.T) (func(http.Handler) http.Handler, error) {
	spec.Servers = openapi3.Servers{&openapi3.Server{URL: "/"}}
	if err := spec.Validate(context.Background()); err != nil {
		return nil, err
	}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, pathParams, err := router.FindRoute(r)
			if err != nil {
				if errors.Is(err, routers.ErrPathNotFound) {
					next.ServeHTTP(w, r)
					return
				}
				if errors.Is(err, routers.ErrMethodNotAllowed) {
					writeError(w, methodNotAllowed())
					return
				}
				writeError(w, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields("request", err.Error())))
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields("body", err.Error())))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			err = openapi3filter.ValidateRequest(r.Context(), &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
				Options: &openapi3filter.Options{
					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
				},
			})
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err != nil {
				writeError(w, validationError(err))
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func validationError(err error) error {
	fields := make([]map[string]string, 0)
	collectFields(err, &fields)
	if len(fields) == 0 {
		return apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", map[string]any{
			"error": err.Error(),
		})
	}
	return apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", map[string]any{
		"fields": fields,
	})
}

func collectFields(err error, out *[]map[string]string) {
	if err == nil {
		return
	}
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, item := range multi {
			collectFields(item, out)
		}
		return
	}
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		before := len(*out)
		if schemaErr.Origin != nil {
			collectFields(schemaErr.Origin, out)
		}
		if len(*out) > before {
			return
		}
		field := strings.Join(schemaErr.JSONPointer(), "/")
		if field == "" {
			field = schemaErr.SchemaField
		}
		reason := schemaErr.Reason
		if reason == "" {
			reason = schemaErr.Error()
		}
		*out = append(*out, map[string]string{"field": field, "violation": reason})
		return
	}
	var requestErr *openapi3filter.RequestError
	if errors.As(err, &requestErr) {
		if requestErr.Parameter != nil && requestErr.Err == nil {
			*out = append(*out, map[string]string{
				"field":     requestErr.Parameter.Name,
				"violation": requestErr.Reason,
			})
			return
		}
		before := len(*out)
		collectFields(requestErr.Err, out)
		if len(*out) == before {
			field := "body"
			if requestErr.Parameter != nil {
				field = requestErr.Parameter.Name
			}
			reason := requestErr.Reason
			if requestErr.Err != nil && reason == "" {
				reason = requestErr.Err.Error()
			}
			if reason == "" {
				reason = requestErr.Error()
			}
			*out = append(*out, map[string]string{"field": field, "violation": reason})
		}
	}
}
