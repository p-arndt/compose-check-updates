package check

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedCommand is one argv the package handed to the host.
type recordedCommand struct {
	name string
	args []string
}

// stubHost replaces the two process seams for the duration of the test, so the
// compose shell-out can be asserted without a Docker daemon anywhere near it.
// onPath decides what `exec.LookPath` finds, probeOK whether the compose plugin
// answers `docker compose version`, and exitCode what the compose run itself
// exits with. The returned slice holds every command, in order.
func stubHost(t *testing.T, onPath []string, probeOK bool, exitCode int) *[]recordedCommand {
	t.Helper()

	originalExec, originalLookPath := execCommand, lookPath
	t.Cleanup(func() { execCommand, lookPath = originalExec, originalLookPath })

	var calls []recordedCommand

	lookPath = func(file string) (string, error) {
		for _, found := range onPath {
			if found == file {
				return "/usr/bin/" + file, nil
			}
		}
		return "", exec.ErrNotFound
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, recordedCommand{name: name, args: args})

		code := exitCode
		if isProbe(name, args) && !probeOK {
			code = 1
		} else if isProbe(name, args) {
			code = 0
		}
		return exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
	}

	return &calls
}

// isProbe reports whether this is the `docker compose version` capability check
// rather than the actual restart.
func isProbe(name string, args []string) bool {
	return name == "docker" && len(args) == 2 && args[0] == "compose" && args[1] == "version"
}

func TestComposeCommand(t *testing.T) {
	tests := []struct {
		name    string
		onPath  []string
		probeOK bool
		want    []string
		wantErr string
	}{
		{
			name:    "docker with the compose plugin is preferred",
			onPath:  []string{"docker", "docker-compose"},
			probeOK: true,
			want:    []string{"docker", "compose"},
		},
		{
			// `docker` on its own says nothing about the plugin being installed,
			// so the legacy binary still has to be reachable.
			name:    "docker without the plugin falls back to the legacy binary",
			onPath:  []string{"docker", "docker-compose"},
			probeOK: false,
			want:    []string{"docker-compose"},
		},
		{
			name:   "legacy binary alone is enough",
			onPath: []string{"docker-compose"},
			want:   []string{"docker-compose"},
		},
		{
			name:    "neither available",
			onPath:  nil,
			wantErr: "neither `docker compose` nor `docker-compose` is available in $PATH",
		},
		{
			// A `docker` that cannot compose is not a compose CLI, and pretending
			// otherwise would fail much later, mid-restart.
			name:    "docker without the plugin and without the legacy binary",
			onPath:  []string{"docker"},
			probeOK: false,
			wantErr: "neither `docker compose` nor `docker-compose` is available in $PATH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubHost(t, tt.onPath, tt.probeOK, 0)

			got, err := composeCommand()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.EqualError(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRestart(t *testing.T) {
	tests := []struct {
		name     string
		update   Update
		onPath   []string
		probeOK  bool
		exitCode int
		wantArgv []string
		wantErr  bool
	}{
		{
			name:     "compose plugin restarts the file the image sits in",
			update:   Update{FilePath: "/stacks/web/docker-compose.yml"},
			onPath:   []string{"docker"},
			probeOK:  true,
			wantArgv: []string{"docker", "compose", "-f", "/stacks/web/docker-compose.yml", "up", "-d"},
		},
		{
			name:     "legacy binary takes the same arguments without the subcommand",
			update:   Update{FilePath: "/stacks/web/docker-compose.yml"},
			onPath:   []string{"docker-compose"},
			wantArgv: []string{"docker-compose", "-f", "/stacks/web/docker-compose.yml", "up", "-d"},
		},
		{
			// A Dockerfile update reaches the container only through a rebuild,
			// and the compose file to restart is the one that builds it.
			name: "a Dockerfile update rebuilds and restarts the composing file",
			update: Update{
				FilePath:    "/stacks/web/Dockerfile",
				ComposePath: "/stacks/web/docker-compose.yml",
			},
			onPath:   []string{"docker"},
			probeOK:  true,
			wantArgv: []string{"docker", "compose", "-f", "/stacks/web/docker-compose.yml", "up", "-d", "--build"},
		},
		{
			// A stack that refused to come up must not look like a successful
			// restart, or the user is told an update landed that never ran.
			name:     "a non-zero exit surfaces as an error",
			update:   Update{FilePath: "/stacks/web/docker-compose.yml"},
			onPath:   []string{"docker"},
			probeOK:  true,
			exitCode: 3,
			wantArgv: []string{"docker", "compose", "-f", "/stacks/web/docker-compose.yml", "up", "-d"},
			wantErr:  true,
		},
		{
			name:    "no compose CLI at all",
			update:  Update{FilePath: "/stacks/web/docker-compose.yml"},
			onPath:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stubHost(t, tt.onPath, tt.probeOK, tt.exitCode)

			err := tt.update.Restart()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.wantArgv == nil {
				assert.Empty(t, *calls)
				return
			}

			// The probe is bookkeeping; the last command is the restart itself.
			require.NotEmpty(t, *calls)
			restart := (*calls)[len(*calls)-1]
			assert.Equal(t, tt.wantArgv, append([]string{restart.name}, restart.args...))
		})
	}
}
