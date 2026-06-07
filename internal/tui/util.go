package tui

import (
	"sort"
	"strconv"
)

func sortStrings(s []string) { sort.Strings(s) }

func itoa(n int) string { return strconv.Itoa(n) }
