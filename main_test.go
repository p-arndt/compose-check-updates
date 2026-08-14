package main

import (
	"testing"

	"github.com/p-arndt/compose-check-updates/internal/modes"
	"github.com/stretchr/testify/assert"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name    string
		outcome modes.Outcome
		want    int
	}{
		{"nothing found", modes.Outcome{}, exitUpToDate},
		{"updates reported but not applied", modes.Outcome{Updates: 3, Pending: 3}, exitOutdated},
		// -u wrote every one of them, so there is nothing left for the caller to
		// act on and the run succeeded at what it was asked to do.
		{"every update applied", modes.Outcome{Updates: 3}, exitUpToDate},
		{"some applied, some not", modes.Outcome{Updates: 3, Pending: 1}, exitOutdated},
		// A failure outranks a pending update: the run could not see everything,
		// so reporting merely "outdated" would understate it.
		{"failure with updates pending", modes.Outcome{Updates: 2, Pending: 2, Failed: true}, exitError},
		{"failure alone", modes.Outcome{Failed: true}, exitError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, exitCode(tt.outcome))
		})
	}
}
