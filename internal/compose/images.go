package compose

import (
	"bufio"
	"os"
	"regexp"
)

var imagePattern = regexp.MustCompile(`^\s*image:\s*(\S+)\s*$`)

// Occurrence is one image reference as a file spells it.
type Occurrence struct {
	Line string // the line verbatim, so it can be found again to rewrite it
	// Raw is the reference exactly as written, `${TAG}` and all, and Reference is
	// the same after interpolation — what a registry can actually be asked
	// about. The two differ only for a reference built from variables.
	Raw       string
	Reference string // the reference, tag and digest included
	Service   string // the compose service declaring it, empty outside one

	// Expansions records the variables Reference was built from, in file order,
	// so a caller can tell which part of the reference a rewrite has to change
	// and in which file that part lives.
	Expansions []Expansion
	// EnvPath is the .env consulted for this file, whether or not it exists.
	EnvPath string
}

// Images returns the images a compose file declares, in file order.
func Images(path string) ([]Occurrence, error) {
	var found []Occurrence
	tracker := newServiceTracker()
	env, envPath := readEnv(path)

	err := eachLine(path, func(line string) {
		service := tracker.observe(line)
		if m := imagePattern.FindStringSubmatch(line); m != nil {
			raw := m[1]
			reference, expansions := Expand(raw, env)
			found = append(found, Occurrence{
				Line:       line,
				Raw:        raw,
				Reference:  reference,
				Service:    service,
				Expansions: expansions,
				EnvPath:    envPath,
			})
		}
	})

	return found, err
}

func eachLine(path string, fn func(line string)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fn(scanner.Text())
	}
	return scanner.Err()
}
