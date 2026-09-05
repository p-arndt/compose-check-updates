package cli

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// -refresh belongs to both modes: reopening the TUI is exactly the case where a
// user wants the cache bypassed, so naming it must not force the report.
func TestParseRefresh(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantRefresh bool
		wantCheck   bool
	}{
		{name: "absent", args: []string{}},
		{name: "on for the TUI", args: []string{"-refresh"}, wantRefresh: true},
		{name: "on for the report", args: []string{"check", "-refresh"}, wantRefresh: true, wantCheck: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = append([]string{"ccu"}, tt.args...)

			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

			result := Parse("test")
			assert.Equal(t, tt.wantRefresh, result.Refresh)
			assert.Equal(t, tt.wantCheck, result.Check)
		})
	}
}
