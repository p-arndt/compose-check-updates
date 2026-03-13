package internal

import (
	"os"
	"os/exec"
	"strings"

	"github.com/Masterminds/semver/v3"
)

type UpdateInfo struct {
	FilePath      string
	RawLine       string
	ImageName     string
	FullImageName string
	CurrentTag    string
	LatestTag     string
}

func (u *UpdateInfo) HasNewVersion(major, minor, patch bool) bool {
	if u.CurrentTag == "" || u.LatestTag == "" {
		return false
	}

	current, err := semver.NewVersion(u.CurrentTag)
	if err != nil {
		return false
	}

	latest, err := semver.NewVersion(u.LatestTag)
	if err != nil {
		return false
	}

	return latest.GreaterThan(current)
}

// UpdateLevel returns the semantic version increment level between CurrentTag and LatestTag.
// Possible values are "major", "minor", "patch" or empty string when undetermined.
func (u *UpdateInfo) UpdateLevel() string {
	if u.CurrentTag == "" || u.LatestTag == "" {
		return ""
	}

	current, err := semver.NewVersion(u.CurrentTag)
	if err != nil {
		return ""
	}

	latest, err := semver.NewVersion(u.LatestTag)
	if err != nil {
		return ""
	}

	if latest.Major() > current.Major() {
		return "major"
	}
	if latest.Minor() > current.Minor() {
		return "minor"
	}
	if latest.Patch() > current.Patch() {
		return "patch"
	}
	return ""
}

func (u *UpdateInfo) Backup() error {
	input, err := os.ReadFile(u.FilePath)
	if err != nil {
		return err
	}

	// Do a backup of the original file
	err = os.WriteFile(u.FilePath+".ccu", input, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (u *UpdateInfo) Update() error {
	// check if a backup file exists
	_, err := os.Stat(u.FilePath + ".ccu")
	if err != nil {
		if os.IsNotExist(err) {
			// if the file does not exist, create a backup
			err = u.Backup()
			if err != nil {
				return err
			}
		}
	}

	input, err := os.ReadFile(u.FilePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		if strings.Contains(line, u.RawLine) {
			lines[i] = strings.Replace(line, u.CurrentTag, u.LatestTag, 1)
		}
	}

	output := strings.Join(lines, "\n")
	err = os.WriteFile(u.FilePath, []byte(output), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (u *UpdateInfo) Restart() error {
	dockerComposeCommand := "docker-compose"
	_, err := exec.LookPath(dockerComposeCommand)
	if err != nil {
		dockerComposeCommand = "docker compose"
		_, err = exec.LookPath(dockerComposeCommand)
		if err != nil {
			return err
		}
	}

	var cmd *exec.Cmd
	if dockerComposeCommand == "docker-compose" {
		cmd = exec.Command("docker-compose", "-f", u.FilePath, "up", "-d")
	} else {
		cmd = exec.Command("docker", "compose", "-f", u.FilePath, "up", "-d")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
