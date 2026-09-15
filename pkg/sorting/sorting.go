// Package sorting turns client sort parameters into SQL-safe ORDER BY expressions.
package sorting

import (
	"maps"
	"slices"
	"strings"
)

// Sort directions accepted from clients, matched case-insensitively.
const (
	Asc  = "asc"
	Desc = "desc"
)

// Columns maps API sort keys to SQL columns. Only mapped columns ever reach SQL.
type Columns map[string]string

// OrderBy returns an ORDER BY expression built only from mapped columns.
// An empty or unknown sortBy sorts by id; any sortDir other than desc sorts ascending.
func (c Columns) OrderBy(sortBy, sortDir string) string {
	dir := "ASC"
	if strings.EqualFold(sortDir, Desc) {
		dir = "DESC"
	}
	column, ok := c.column(sortBy)
	if !ok || column == "id" {
		return "id " + dir
	}
	return column + " " + dir + ", id " + dir
}

// Validate returns field errors for an unknown sortBy or a sortDir other than asc or desc.
// Empty values are valid and select the defaults.
func (c Columns) Validate(sortBy, sortDir string) map[string]string {
	fields := map[string]string{}
	if _, ok := c.column(sortBy); sortBy != "" && !ok {
		fields["sort_by"] = "must be one of: " + strings.Join(slices.Sorted(maps.Keys(c)), ", ")
	}
	if sortDir != "" && !strings.EqualFold(sortDir, Asc) && !strings.EqualFold(sortDir, Desc) {
		fields["sort_dir"] = "must be asc or desc"
	}
	return fields
}

// column finds the SQL column for sortBy, ignoring case.
func (c Columns) column(sortBy string) (string, bool) {
	if column, ok := c[sortBy]; ok {
		return column, true
	}
	for key, column := range c {
		if strings.EqualFold(key, sortBy) {
			return column, true
		}
	}
	return "", false
}
