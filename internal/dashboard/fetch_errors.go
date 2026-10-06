package dashboard

import (
	"errors"
	"fmt"
)

// UnavailableError means the portal could not reach the daemon or hub.
// Unwrap exposes context.DeadlineExceeded, net.Error, syscall.ECONNREFUSED,
// and os.ErrNotExist when the caller wrapped one of those.
type UnavailableError struct {
	Err error
}

func (e *UnavailableError) Error() string {
	if e == nil || e.Err == nil {
		return "daemon unavailable"
	}
	return e.Err.Error()
}

func (e *UnavailableError) Unwrap() error { return e.Err }

// UpstreamError is a daemon or hub error reply: the call was delivered and
// the peer answered with failure (APIResponse.Success == false).
type UpstreamError struct {
	Msg string
}

func (e *UpstreamError) Error() string {
	if e == nil || e.Msg == "" {
		return "upstream error"
	}
	return e.Msg
}

func unavailable(err error) error {
	if err == nil {
		return &UnavailableError{Err: fmt.Errorf("daemon unavailable")}
	}
	var already *UnavailableError
	if errors.As(err, &already) {
		return err
	}
	return &UnavailableError{Err: err}
}
