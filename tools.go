//go:build tools

package tools

// Tool dependencies are listed here so that `go mod tidy` does not remove them.
// Install with: go install <tool>@latest
//
// These are development-time dependencies only and are not compiled into the
// final binary.
import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)
