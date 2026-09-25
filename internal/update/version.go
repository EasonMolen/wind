package update

import (
	"fmt"
	"strconv"
	"strings"
)

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

func newerThan(latest, current string) (bool, error) {
	latestParts, err := parseVersion(latest)
	if err != nil {
		return false, fmt.Errorf("invalid latest version %q: %w", latest, err)
	}
	currentParts, err := parseVersion(current)
	if err != nil {
		return false, fmt.Errorf("invalid current version %q: %w", current, err)
	}
	for index := range latestParts {
		if latestParts[index] == currentParts[index] {
			continue
		}
		return latestParts[index] > currentParts[index], nil
	}
	return false, nil
}

func parseVersion(value string) ([3]int, error) {
	var result [3]int
	base := strings.SplitN(normalizeVersion(value), "-", 2)[0]
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("expected major.minor.patch")
	}
	for index, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return result, fmt.Errorf("invalid numeric component %q", part)
		}
		result[index] = parsed
	}
	return result, nil
}
