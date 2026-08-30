package compose

import (
	"bufio"
	"os"
	"regexp"
)

var imagePattern = regexp.MustCompile(`^\s*image:\s*(\S+)\s*$`)

// Occurrence is one image reference as a file spells it.
type Occurrence struct {
	Line      string // the line verbatim, so it can be found again to rewrite it
	Reference string // the reference, tag and digest included
	Service   string // the compose service declaring it, empty outside one
}

// Images returns the images a compose file declares, in file order.
func Images(path string) ([]Occurrence, error) {
	var found []Occurrence
	tracker := newServiceTracker()

	err := eachLine(path, func(line string) {
		service := tracker.observe(line)
		if m := imagePattern.FindStringSubmatch(line); m != nil {
			found = append(found, Occurrence{Line: line, Reference: m[1], Service: service})
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
