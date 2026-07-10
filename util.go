package main

import (
	"fmt"
	"strings"
)

// fmtErrorf is a thin wrapper around fmt.Errorf to centralize error formatting.
func fmtErrorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// (string utility — keep stdlib strings usage through these wrappers for clarity)
func trim(s string) string  { return strings.TrimSpace(s) }
func lower(s string) string { return strings.ToLower(s) }
