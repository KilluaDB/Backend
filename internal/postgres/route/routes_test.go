package route_test

import (
	"sort"
	"testing"

	pghandler "backend/internal/postgres/handler"
	pgroute "backend/internal/postgres/route"
	"backend/internal/repository"

	"github.com/gin-gonic/gin"
)

// route is a (method, path) pair we expect to be registered exactly once.
type route struct {
	method string
	path   string
}

// collectRoutes returns the registered routes as a multiset keyed by
// "METHOD path" so duplicates can be detected.
func collectRoutes(engine *gin.Engine) map[route]int {
	got := make(map[route]int)
	for _, ri := range engine.Routes() {
		got[route{ri.Method, ri.Path}]++
	}
	return got
}

// assertRoutes asserts that the engine's route table is exactly `want`:
// every expected route present exactly once, and no unexpected routes.
func assertRoutes(t *testing.T, engine *gin.Engine, want []route) {
	t.Helper()
	got := collectRoutes(engine)

	for _, w := range want {
		switch got[w] {
		case 0:
			t.Errorf("missing route: %s %s", w.method, w.path)
		case 1:
			// exactly once, good
		default:
			t.Errorf("route registered %d times (want 1): %s %s", got[w], w.method, w.path)
		}
		delete(got, w)
	}

	// Anything left in `got` is unexpected.
	extras := make([]string, 0, len(got))
	for r, n := range got {
		for i := 0; i < n; i++ {
			extras = append(extras, r.method+" "+r.path)
		}
	}
	if len(extras) > 0 {
		sort.Strings(extras)
		t.Errorf("unexpected routes registered: %v", extras)
	}
}

// newPostgresHandler builds a PostgresHandler whose sub-handlers are non-nil
// zero-value structs. Route registration only takes method values (it never
// invokes the handlers), so the internal dependencies may stay nil.
func newPostgresHandler() *pghandler.PostgresHandler {
	return &pghandler.PostgresHandler{
		Table:     &pghandler.TableHandler{},
		Schema:    &pghandler.SchemaHandler{},
		Query:     &pghandler.QueryHandler{},
		Dashboard: &pghandler.DashboardHandler{},
		TextToSQL: &pghandler.TextToSQLHandler{},
	}
}

func TestPostgresRoutes_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")

	// nil ProjectRepository is fine: RequirePostgresProject takes a ProjectStore
	// interface and merely closes over it at registration time.
	routes := pgroute.NewPostgresRoutes((*repository.ProjectRepository)(nil), newPostgresHandler())
	routes.RegisterRoutes(api)

	const base = "/api/v1/projects/:id/postgres"
	want := []route{
		// Dashboard
		{"GET", base + "/dashboard/overview"},
		{"GET", base + "/dashboard/metrics"},
		// Tables
		{"POST", base + "/tables"},
		{"GET", base + "/tables"},
		{"GET", base + "/tables/:table"},
		{"PATCH", base + "/tables/:table"},
		{"DELETE", base + "/tables/:table"},
		// Rows
		{"GET", base + "/tables/:table/rows"},
		{"POST", base + "/tables/:table/rows"},
		{"PATCH", base + "/tables/:table/rows"},
		{"DELETE", base + "/tables/:table/rows"},
		// Columns
		{"POST", base + "/tables/:table/columns"},
		{"DELETE", base + "/tables/:table/columns/:column"},
		// Indexes
		{"GET", base + "/tables/:table/indexes"},
		{"POST", base + "/tables/:table/indexes"},
		{"DELETE", base + "/tables/:table/indexes/:index"},
		// Schema
		{"GET", base + "/schemas"},
		{"GET", base + "/schema/visualize"},
		{"POST", base + "/schema/from-text"},
		{"POST", base + "/schema/from-text/stream"},
		{"POST", base + "/schema/approve"},
		// Query
		{"POST", base + "/query/execute"},
		{"GET", base + "/query/history"},
		// Text-to-SQL
		{"POST", base + "/text-to-sql"},
	}

	assertRoutes(t, engine, want)
}
