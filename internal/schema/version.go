package schema

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
)

var versionPattern = regexp.MustCompile(`^\s*version\s*:\s*["']?([^"']+)["']?\s*$`)

func ReadVersion(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open schema file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		matches := versionPattern.FindStringSubmatch(scanner.Text())
		if len(matches) == 2 {
			return matches[1], nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan schema file: %w", err)
	}

	return "", fmt.Errorf("schema version not found in %s", path)
}
