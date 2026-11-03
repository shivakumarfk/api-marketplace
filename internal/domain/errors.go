// Package domain holds the core types and business rules. It has no I/O and
// no framework dependencies; every other layer depends on it, never the reverse.
package domain

import (
	"errors"
	"fmt"
)

// Kind classifies an error so the transport layer can map it to a status
// code without knowing anything about the business rule that produced it.
type Kind int

const (
	KindInternal Kind = iota
	KindInvalid
	KindNotFound
	KindConflict
	KindUnauthorized
	KindForbidden
)

// Error is a business error with a stable, machine-readable code.
type Error struct {
	Kind    Kind
	Code    string // e.g. INVALID_GSTIN; part of the public API contract
	Message string
	Err     error // optional cause, never shown to clients
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func Invalid(code, msg string) *Error  { return &Error{Kind: KindInvalid, Code: code, Message: msg} }
func NotFound(code, msg string) *Error { return &Error{Kind: KindNotFound, Code: code, Message: msg} }
func Conflict(code, msg string) *Error { return &Error{Kind: KindConflict, Code: code, Message: msg} }
func Forbidden(code, msg string) *Error {
	return &Error{Kind: KindForbidden, Code: code, Message: msg}
}

// ErrNotFound is returned by repositories when a row does not exist; services
// translate it into a specific NotFound error.
var ErrNotFound = errors.New("not found")

// AsError extracts a *Error, if any.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
