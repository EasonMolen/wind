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

// parseVersion supports both semantic three-part versions (1.2.3) and the
// four-part Windows version format (1.2.3.4). A missing build component is
// treated as zero, so 1.2.3 and 1.2.3.0 compare as equal.
func parseVersion(value string) ([4]int, error) {
	var result [4]int
	base := strings.SplitN(normalizeVersion(value), "-", 2)[0]
	parts := strings.Split(base, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return result, fmt.Errorf("expected major.minor.patch or major.minor.patch.build")
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
