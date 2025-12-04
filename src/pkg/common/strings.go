package common

import "strings"

func CleanString(input string) string {
	return strings.TrimSpace(strings.ReplaceAll(input, "\n", ""))
}
