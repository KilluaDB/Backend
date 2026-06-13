package route_test

import (
	"sort"
	"testing"

	"backend/internal/backup"
	"backend/internal/handler"
	mongohandler "backend/internal/mongodb/handler"
	pghandler "backend/internal/postgres/handler"
	"backend/internal/repository"
	approute "backend/internal/route"

	"github.com/gin-gonic/gin"
)

// route is a (method, path) pair we expect to be registered exactly once.
type route struct {
	method string
	path   string
}

// collectRoutes returns the registered routes as a multiset keyed by
// (method, path) so duplicates can be detected.
func collectRoutes(engine *gin.Engine) map[route]int {
	got := make(map[route]int)
	for _, ri := range engine.Routes() {
		got[route{ri.Method, ri.Path}]++
	}
	return got
}

// assertRoutes asserts the engine's route table is exactly `want`: every
// expected route present exactly once, and no unexpected routes.
func assertRoutes(t *testing.T, engine *gin.Engine, want []route) {
	t.Helper()
	got := collectRoutes(engine)

	for _, w := range want {
		switch got[w] {
		case 0:
			t.Errorf("missing route: %s %s", w.method, w.path)
		case 1:
		default:
			t.Errorf("route registered %d times (want 1): %s %s", got[w], w.method, w.path)
		}
		delete(got, w)
	}

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

// Handlers below are constructed as zero-value structs. Route registration only
// takes method values off them (it never invokes them), so their unexported
// dependencies may stay nil. Sub-handlers referenced via field access during
// registration (PostgresHandler.Table etc.) must be non-nil.

func newPostgresHandler() *pghandler.PostgresHandler {
	return &pghandler.PostgresHandler{
		Table:     &pghandler.TableHandler{},
		Schema:    &pghandler.SchemaHandler{},
		Query:     &pghandler.QueryHandler{},
		Dashboard: &pghandler.DashboardHandler{},
		TextToSQL: &pghandler.TextToSQLHandler{},
	}
}

// --- Individual route registrars -------------------------------------------

func TestAuthRoutes_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")

	routes := approute.NewAuthRoutes(&handler.AuthHandler{}, &handler.GoogleAuthHandler{})
	routes.RegisterRoutes(api)

	want := []route{
		{"POST", "/api/v1/auth/register"},
		{"POST", "/api/v1/auth/login"},
		{"GET", "/api/v1/auth/google/login"},
		{"GET", "/api/v1/auth/google/callback"},
		{"POST", "/api/v1/auth/logout"},
		{"POST", "/api/v1/auth/refresh"},
	}
	assertRoutes(t, engine, want)
}

func TestUserRoutes_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")

	// nil UserRepository is fine: RequireAdmin takes a UserStore interface and
	// merely closes over it at registration time.
	routes := approute.NewUserRoutes(&handler.UserHandler{}, (*repository.UserRepository)(nil))
	routes.RegisterRoutes(api)

	want := []route{
		{"GET", "/api/v1/users/me"},
		{"PATCH", "/api/v1/users/me"},
		{"DELETE", "/api/v1/users/me"},
		{"GET", "/api/v1/users"},
		{"GET", "/api/v1/users/:user_id"},
		{"PATCH", "/api/v1/users/:user_id"},
		{"DELETE", "/api/v1/users/:user_id"},
	}
	assertRoutes(t, engine, want)
}

func TestProjectRoutes_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")

	routes := approute.NewProjectRoutes(&handler.ProjectHandler{})
	routes.RegisterRoutes(api)

	want := []route{
		{"POST", "/api/v1/projects"},
		{"GET", "/api/v1/projects"},
		{"GET", "/api/v1/projects/:id"},
		{"GET", "/api/v1/projects/:id/access"},
		{"DELETE", "/api/v1/projects/:id"},
	}
	assertRoutes(t, engine, want)
}

// --- Composed top-level registrar ------------------------------------------

func TestRegisterRoutes_All(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	approute.RegisterRoutes(
		engine,
		&handler.AuthHandler{},
		&handler.GoogleAuthHandler{},
		&handler.UserHandler{},
		(*repository.UserRepository)(nil),
		&handler.ProjectHandler{},
		(*repository.ProjectRepository)(nil),
		newPostgresHandler(),
		&mongohandler.MongoHandler{
			Collection: &mongohandler.CollectionHandler{},
			Document:   &mongohandler.DocumentHandler{},
			Dashboard:  &mongohandler.MongoDashboardHandler{},
		},
		&backup.Handler{},
	)

	const pg = "/api/v1/projects/:id/postgres"
	const mongo = "/api/v1/projects/:id/mongodb"

	want := []route{
		// Auth
		{"POST", "/api/v1/auth/register"},
		{"POST", "/api/v1/auth/login"},
		{"GET", "/api/v1/auth/google/login"},
		{"GET", "/api/v1/auth/google/callback"},
		{"POST", "/api/v1/auth/logout"},
		{"POST", "/api/v1/auth/refresh"},

		// Users
		{"GET", "/api/v1/users/me"},
		{"PATCH", "/api/v1/users/me"},
		{"DELETE", "/api/v1/users/me"},
		{"GET", "/api/v1/users"},
		{"GET", "/api/v1/users/:user_id"},
		{"PATCH", "/api/v1/users/:user_id"},
		{"DELETE", "/api/v1/users/:user_id"},

		// Projects
		{"POST", "/api/v1/projects"},
		{"GET", "/api/v1/projects"},
		{"GET", "/api/v1/projects/:id"},
		{"GET", "/api/v1/projects/:id/access"},
		{"DELETE", "/api/v1/projects/:id"},

		// Postgres
		{"GET", pg + "/dashboard/overview"},
		{"GET", pg + "/dashboard/metrics"},
		{"POST", pg + "/tables"},
		{"GET", pg + "/tables"},
		{"GET", pg + "/tables/:table"},
		{"PATCH", pg + "/tables/:table"},
		{"DELETE", pg + "/tables/:table"},
		{"GET", pg + "/tables/:table/rows"},
		{"POST", pg + "/tables/:table/rows"},
		{"PATCH", pg + "/tables/:table/rows"},
		{"DELETE", pg + "/tables/:table/rows"},
		{"POST", pg + "/tables/:table/columns"},
		{"DELETE", pg + "/tables/:table/columns/:column"},
		{"GET", pg + "/tables/:table/indexes"},
		{"POST", pg + "/tables/:table/indexes"},
		{"DELETE", pg + "/tables/:table/indexes/:index"},
		{"GET", pg + "/schemas"},
		{"GET", pg + "/schema/visualize"},
		{"POST", pg + "/schema/from-text"},
		{"POST", pg + "/schema/from-text/stream"},
		{"POST", pg + "/schema/approve"},
		{"POST", pg + "/query/execute"},
		{"GET", pg + "/query/history"},
		{"POST", pg + "/text-to-sql"},

		// MongoDB
		{"GET", mongo + "/dashboard/metrics"},
		{"GET", mongo + "/collections"},
		{"POST", mongo + "/collections"},
		{"DELETE", mongo + "/collections/:collection"},
		{"POST", mongo + "/collections/:collection/fields"},
		{"DELETE", mongo + "/collections/:collection/fields/:field"},
		{"GET", mongo + "/collections/:collection/documents"},
		{"POST", mongo + "/collections/:collection/documents"},
		{"PATCH", mongo + "/collections/:collection/documents"},
		{"DELETE", mongo + "/collections/:collection/documents"},
		{"POST", mongo + "/collections/:collection/documents/count"},
		{"POST", mongo + "/collections/:collection/documents/query"},
		{"GET", mongo + "/collections/:collection/documents/:docId"},
		{"DELETE", mongo + "/collections/:collection/documents/:docId"},
		{"POST", mongo + "/collections/:collection/documents/:docId/fields"},
		{"PATCH", mongo + "/collections/:collection/documents/:docId/fields/:field"},
		{"DELETE", mongo + "/collections/:collection/documents/:docId/fields/:field"},

		// Backup
		{"GET", "/api/v1/projects/:id/export"},
		{"POST", "/api/v1/projects/:id/import"},

		// Health
		{"GET", "/health"},
	}

	assertRoutes(t, engine, want)
}
