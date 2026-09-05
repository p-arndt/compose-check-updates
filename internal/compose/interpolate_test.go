package compose

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandResolvesTheFormsComposeUnderstands(t *testing.T) {
	env := map[string]EnvEntry{
		"TAG":   {Value: "1.2.3"},
		"EMPTY": {Value: ""},
	}

	tests := []struct {
		name     string
		text     string
		expanded string
		source   VarSource
	}{
		{"braced", "nginx:${TAG}", "nginx:1.2.3", VarEnvFile},
		{"bare", "nginx:$TAG", "nginx:1.2.3", VarEnvFile},
		{"default is skipped when set", "nginx:${TAG:-latest}", "nginx:1.2.3", VarEnvFile},
		{"default is used when unset", "nginx:${MISSING:-latest}", "nginx:latest", VarDefault},
		{"colon-dash treats empty as unset", "nginx:${EMPTY:-latest}", "nginx:latest", VarDefault},
		{"dash treats empty as set", "nginx:${EMPTY-latest}", "nginx:", VarEnvFile},
		{"required reads like a plain reference", "nginx:${TAG:?missing}", "nginx:1.2.3", VarEnvFile},
		{"unset leaves nothing behind", "nginx:${MISSING}", "nginx:", VarUnset},
		{"alternate replaces the value", "nginx:${TAG:+fixed}", "nginx:fixed", VarAlternate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expanded, expansions := Expand(tt.text, env)

			assert.Equal(t, tt.expanded, expanded)
			require.Len(t, expansions, 1)
			assert.Equal(t, tt.source, expansions[0].Source)
			// The range has to point at the value in the expanded string, because
			// that is how a caller tells a tag variable from a repository one.
			assert.Equal(t, expansions[0].Value, expanded[expansions[0].Start:expansions[0].End])
		})
	}
}

// The process environment wins over the .env, as it does for docker compose, so
// ccu resolves the tag the running stack resolves.
func TestExpandPrefersTheProcessEnvironmentOverTheEnvFile(t *testing.T) {
	t.Setenv("CCU_TEST_TAG", "from-shell")

	expanded, expansions := Expand("nginx:${CCU_TEST_TAG}", map[string]EnvEntry{
		"CCU_TEST_TAG": {Value: "from-file"},
	})

	assert.Equal(t, "nginx:from-shell", expanded)
	require.Len(t, expansions, 1)
	assert.Equal(t, VarProcessEnv, expansions[0].Source)
}

func TestExpandKeepsLiteralsAndReportsEveryVariable(t *testing.T) {
	env := map[string]EnvEntry{"REGISTRY": {Value: "reg.example.com"}, "TAG": {Value: "1.2.3"}}

	expanded, expansions := Expand("${REGISTRY}/app:${TAG}-alpine", env)

	assert.Equal(t, "reg.example.com/app:1.2.3-alpine", expanded)
	require.Len(t, expansions, 2)
	assert.Equal(t, "REGISTRY", expansions[0].Name)
	assert.Equal(t, "TAG", expansions[1].Name)
	assert.Equal(t, "1.2.3", expanded[expansions[1].Start:expansions[1].End])
}

// `$$` is compose's escape for a literal dollar, and it shifts every range after
// it — a value whose offsets ignored it would be rewritten in the wrong place.
func TestExpandHonoursTheEscapedDollar(t *testing.T) {
	expanded, expansions := Expand("app:$$-${TAG}", map[string]EnvEntry{"TAG": {Value: "1.0"}})

	assert.Equal(t, "app:$-1.0", expanded)
	require.Len(t, expansions, 1)
	assert.Equal(t, "1.0", expanded[expansions[0].Start:expansions[0].End])
}

func TestExpandLeavesAReferenceWithoutVariablesAlone(t *testing.T) {
	expanded, expansions := Expand("nginx:1.2.3", nil)

	assert.Equal(t, "nginx:1.2.3", expanded)
	assert.Empty(t, expansions)
}
