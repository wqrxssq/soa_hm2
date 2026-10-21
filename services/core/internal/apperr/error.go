package apperr

import "errors"

type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	return e.Message
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func WithDetails(status int, code, message string, details map[string]any) *Error {
	return &Error{Status: status, Code: code, Message: message, Details: details}
}

func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func Fields(pairs ...string) map[string]any {
	fields := make([]map[string]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		fields = append(fields, map[string]string{
			"field":     pairs[i],
			"violation": pairs[i+1],
		})
	}
	return map[string]any{"fields": fields}
}
