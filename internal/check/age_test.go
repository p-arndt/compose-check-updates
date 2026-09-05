package check

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		published time.Time
		want      string
	}{
		{name: "unknown", published: time.Time{}, want: ""},
		{name: "seconds", published: now.Add(-30 * time.Second), want: "just now"},
		{name: "minutes", published: now.Add(-45 * time.Minute), want: "45m ago"},
		{name: "hours", published: now.Add(-5 * time.Hour), want: "5h ago"},
		{name: "days", published: now.Add(-3 * 24 * time.Hour), want: "3d ago"},
		{name: "months", published: now.Add(-70 * 24 * time.Hour), want: "2mo ago"},
		{name: "years", published: now.Add(-800 * 24 * time.Hour), want: "2y ago"},
		// A build date in the future is a skewed clock, not an age.
		{name: "future", published: now.Add(time.Hour), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Age(tt.published, now))
		})
	}
}

// The age belongs to the tag it was resolved for. Pointing the update at a
// different target has to take it away rather than let it describe the new one.
func TestPublishedAtFollowsTheSelectedTag(t *testing.T) {
	t.Parallel()

	built := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := Update{CurrentTag: "1.2.3", LatestTag: "1.2.4", PatchTag: "1.2.4", MinorTag: "1.3.0"}
	u.SetPublished("1.2.4", built)

	assert.True(t, built.Equal(u.PublishedAt()))

	u.SelectTarget("minor")
	assert.True(t, u.PublishedAt().IsZero())
	assert.Empty(t, u.Age())
}
