package kql

import (
	"fmt"
	"strings"
)

// Standard logical dependency categories
const (
	CategorySQL    = "SQL"
	CategoryHTTP   = "HTTP"
	CategoryRedis  = "REDIS"
	CategoryCosmos = "COSMOS"
	CategoryOther  = "OTHER"
)

// SQLDependencyPredicate returns the single source of truth KQL filter expression
// for database and SQL dependencies.
func SQLDependencyPredicate() string {
	return "type in~ ('SQL', 'Azure SQL', 'SqlServer', 'PostgreSQL', 'postgres', 'postgresql', 'mysql', 'MySQL', 'SQL Server') or type has 'sql' or type has 'postgres' or type has 'mysql'"
}

// HTTPDependencyPredicate returns the single source of truth KQL filter expression
// for external APIs and HTTP dependencies.
func HTTPDependencyPredicate() string {
	return "type in~ ('HTTP', 'Http (tracked component)', 'gRPC', 'Webservice') or type has 'http'"
}

// RedisDependencyPredicate returns the single source of truth KQL filter expression
// for cache and Redis dependencies.
func RedisDependencyPredicate() string {
	return "type in~ ('Redis', 'Azure Redis', 'Memcached') or type has 'redis' or type has 'memcached'"
}

// CosmosDependencyPredicate returns the single source of truth KQL filter expression
// for Azure DocumentDB and Cosmos DB dependencies.
func CosmosDependencyPredicate() string {
	return "type in~ ('Azure DocumentDB', 'Cosmos', 'CosmosDB')"
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
	case CategoryCosmos, "COSMOSDB":
		return fmt.Sprintf("| where %s\n", CosmosDependencyPredicate())
	case "", "ALL":
		return ""
	default:
		return fmt.Sprintf("| where type =~ '%s'\n", sanitize(depType))
	}
}
