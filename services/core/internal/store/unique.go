package store

import "errors"

func errorAs(err error, target *uniqueError) bool {
	return errors.As(err, target)
}
