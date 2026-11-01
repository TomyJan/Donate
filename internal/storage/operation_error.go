package storage

import (
	"fmt"
	"path/filepath"
)

// OperationError carries safe operational context separately from the cause.
// Log these fields with a controlled reason, rather than logging Err directly.
type OperationError struct {
	Operation string
	Phase     string
	Path      string
	Source    string
	Target    string
	Err       error
}

func (e *OperationError) Error() string {
	if e.Operation == "restore" {
		return fmt.Sprintf("database %s %s source=%q target=%q: %v", e.Operation, e.Phase, filepath.ToSlash(e.Source), filepath.ToSlash(e.Target), e.Err)
	}
	return fmt.Sprintf("database %s %s path=%q: %v", e.Operation, e.Phase, filepath.ToSlash(e.Path), e.Err)
}

func (e *OperationError) Unwrap() error { return e.Err }
