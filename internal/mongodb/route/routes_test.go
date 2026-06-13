package route_test

import (
	"sort"
	"testing"

	mongohandler "backend/internal/mongodb/handler"
	mongoroute "backend/internal/mongodb/route"
	"backend/internal/repository"

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

// assertRoutes asserts the engine's route table is exactly `want`.
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

func TestMongoRoutes_RegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")

	// nil ProjectRepository is fine: RequireMongoProject takes a ProjectStore
	// interface and merely closes over it at registration time. The handler's
	// Collection sub-handler must be non-nil because route registration takes a
	// method value off it (without invoking it).
	mongo := &mongohandler.MongoHandler{
		Collection: &mongohandler.CollectionHandler{},
		Document:   &mongohandler.DocumentHandler{},
		Dashboard:  &mongohandler.MongoDashboardHandler{},
	}
	routes := mongoroute.NewMongoRoutes((*repository.ProjectRepository)(nil), mongo)
	routes.RegisterRoutes(api)

	const base = "/api/v1/projects/:id/mongodb"
	want := []route{
		{"GET", base + "/dashboard/metrics"},
		{"GET", base + "/collections"},
		{"POST", base + "/collections"},
		{"DELETE", base + "/collections/:collection"},
		{"POST", base + "/collections/:collection/fields"},
		{"DELETE", base + "/collections/:collection/fields/:field"},
		{"GET", base + "/collections/:collection/documents"},
		{"POST", base + "/collections/:collection/documents"},
		{"PATCH", base + "/collections/:collection/documents"},
		{"DELETE", base + "/collections/:collection/documents"},
		{"POST", base + "/collections/:collection/documents/count"},
		{"POST", base + "/collections/:collection/documents/query"},
		{"GET", base + "/collections/:collection/documents/:docId"},
		{"DELETE", base + "/collections/:collection/documents/:docId"},
		{"POST", base + "/collections/:collection/documents/:docId/fields"},
		{"PATCH", base + "/collections/:collection/documents/:docId/fields/:field"},
		{"DELETE", base + "/collections/:collection/documents/:docId/fields/:field"},
	}

	assertRoutes(t, engine, want)
}
