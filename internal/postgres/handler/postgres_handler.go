// Package handler implements HTTP handlers for PostgreSQL-backed projects under
// /api/v1/projects/{id}/postgres/*. Request and response shapes, including JSON samples,
// are documented in the repository root openapi.yaml under the Postgres tag.
package handler

// PostgresHandler groups HTTP handlers for all Postgres project APIs (tables/rows/columns, schema, query).
type PostgresHandler struct {
	Table  *TableHandler
	Schema *SchemaHandler
	Query  *QueryHandler
	Dashboard *DashboardHandler
}

func NewPostgresHandler(table *TableHandler, schema *SchemaHandler, query *QueryHandler, dashboard *DashboardHandler) *PostgresHandler {
	return &PostgresHandler{
		Table:     table,
		Schema:    schema,
		Query:     query,
		Dashboard: dashboard,
	}
}
