// Package gstin validates the format of an Indian GSTIN.
//
// Phase 1 checks format only (no GST portal lookup), per requirements §3:
// 2-digit state code, 10-character PAN, entity number, 'Z', check character.
package gstin

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var pattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

var (
	ErrFormat    = errors.New("gstin: must be 15 characters: state code, PAN, entity number, 'Z', check character")
	ErrStateCode = errors.New("gstin: state code must be between 01 and 38, or 97/99")
)

// Normalize trims spaces and upper-cases the input.
func Normalize(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ValidateFormat returns nil if s (after Normalize) is a well-formed GSTIN.
// It does not verify the check character or that the GSTIN is registered.
func ValidateFormat(s string) error {
	s = Normalize(s)
	if !pattern.MatchString(s) {
		return ErrFormat
	}
	code, _ := strconv.Atoi(s[:2])
	if !(code >= 1 && code <= 38) && code != 97 && code != 99 {
		return ErrStateCode
	}
	return nil
}
