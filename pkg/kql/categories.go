package kql

import (
	"fmt"
	"strings"
)

// Standard logical dependency categories
const (
	CategorySQL   = "SQL"
	CategoryHTTP  = "HTTP"
	CategoryRedis = "REDIS"
	CategoryOther = "OTHER"
)

// SQLDependencyPredicate returns the single source of truth KQL filter expression
// for database and SQL dependencies.
func SQLDependencyPredicate() string {
	return "type in~ ('SQL', 'Azure SQL', 'PostgreSQL', 'postgres', 'postgresql', 'mysql', 'MySQL') or type has 'sql' or type has 'postgres' or type has 'mysql'"
}

// HTTPDependencyPredicate returns the single source of truth KQL filter expression
// for external APIs and HTTP dependencies.
func HTTPDependencyPredicate() string {
	return "type in~ ('HTTP', 'Http (tracked component)', 'gRPC', 'Webservice') or type has 'http'"
}

// RedisDependencyPredicate returns the single source of truth KQL filter expression
// for cache and Redis dependencies.
func RedisDependencyPredicate() string {
	return "type in~ ('Redis', 'Azure Redis') or type has 'redis'"
}

// formatColumnInOrEquals formats column =~ 'val' for a single type or column in~ ('val1', 'val2') for multiple
func formatColumnInOrEquals(column string, values []string) string {
	var clean []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			clean = append(clean, fmt.Sprintf("'%s'", sanitize(v)))
		}
	}
	if len(clean) == 0 {
		return ""
	}
	if len(clean) == 1 {
		return fmt.Sprintf("%s =~ %s", column, clean[0])
	}
	return fmt.Sprintf("%s in~ (%s)", column, strings.Join(clean, ", "))
}

// DynamicSQLDependencyPredicate returns custom SQL types if configured, or falls back to SQLDependencyPredicate().
func DynamicSQLDependencyPredicate(sqlTypes []string) string {
	if len(sqlTypes) > 0 {
		if pred := formatColumnInOrEquals("type", sqlTypes); pred != "" {
			return pred
		}
	}
	return SQLDependencyPredicate()
}

// DynamicHTTPDependencyPredicate returns custom HTTP types if configured, or falls back to HTTPDependencyPredicate().
func DynamicHTTPDependencyPredicate(httpTypes []string) string {
	if len(httpTypes) > 0 {
		if pred := formatColumnInOrEquals("type", httpTypes); pred != "" {
			return pred
		}
	}
	return HTTPDependencyPredicate()
}

// DynamicRedisDependencyPredicate returns custom cache/Redis types if configured, or falls back to RedisDependencyPredicate().
func DynamicRedisDependencyPredicate(cacheTypes []string) string {
	if len(cacheTypes) > 0 {
		if pred := formatColumnInOrEquals("type", cacheTypes); pred != "" {
			return pred
		}
	}
	return RedisDependencyPredicate()
}

// DependencyCategoryFilter returns the KQL WHERE clause for a specific category,
// or an empty string if no category filter applies.
func DependencyCategoryFilter(depType string) string {
	cleanType := strings.ToUpper(strings.TrimSpace(depType))
	switch cleanType {
	case CategorySQL:
		return fmt.Sprintf("| where %s\n", SQLDependencyPredicate())
	case CategoryHTTP:
		return fmt.Sprintf("| where %s\n", HTTPDependencyPredicate())
	case CategoryRedis:
		return fmt.Sprintf("| where %s\n", RedisDependencyPredicate())
	case "", "ALL":
		return ""
	default:
		return fmt.Sprintf("| where type =~ '%s'\n", sanitize(depType))
	}
}

// DynamicDependencyCategoryFilter returns the KQL WHERE clause for a category respecting configured types.
func DynamicDependencyCategoryFilter(depType string, sqlTypes, httpTypes, cacheTypes []string) string {
	cleanType := strings.ToUpper(strings.TrimSpace(depType))
	switch cleanType {
	case CategorySQL:
		return fmt.Sprintf("| where %s\n", DynamicSQLDependencyPredicate(sqlTypes))
	case CategoryHTTP:
		return fmt.Sprintf("| where %s\n", DynamicHTTPDependencyPredicate(httpTypes))
	case CategoryRedis:
		return fmt.Sprintf("| where %s\n", DynamicRedisDependencyPredicate(cacheTypes))
	case "", "ALL":
		return ""
	default:
		return fmt.Sprintf("| where type =~ '%s'\n", sanitize(depType))
	}
}
