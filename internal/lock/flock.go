package lock

import "errors"

var errLockHeld = errors.New("lock held")

func isLockHeld(err error) bool {
	return errors.Is(err, errLockHeld)
}
