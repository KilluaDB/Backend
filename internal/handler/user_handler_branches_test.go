package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"unsafe"

	"backend/internal/mocks"
	"backend/internal/service"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingUserHandler builds a UserHandler whose store fails every FindUserByID,
// so Update/Delete return a generic (non-"user not found", non-policy) error and
// the handler maps it to 500.
func failingUserHandler() *UserHandler {
	users := &userStoreFailingFindByID{UserStore: mocks.NewUserStore()}
	return NewUserHandler(service.NewUserService(users, mocks.NewProjectStore(), nil))
}

func patchJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestUserHandler_UpdateMe_unauthorized(t *testing.T) {
	h := newUserHandler(mocks.NewUserStore())
	r := userRouter(h, uuid.Nil)
	w := patchJSON(t, r, "/users/me", map[string]string{"email": "x@test.com"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserHandler_UpdateMe_serviceError(t *testing.T) {
	h := failingUserHandler()
	r := userRouter(h, uuid.New())
	w := patchJSON(t, r, "/users/me", map[string]string{"email": "x@test.com"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUserHandler_UpdateUser_invalidBody(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	target := users.SeedUser("t@test.com", "hash", "user")
	h := newUserHandler(users)
	r := userRouter(h, admin.ID)

	// email as int -> bind failure
	w := patchJSON(t, r, "/users/"+target.ID.String(), map[string]int{"email": 1})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUserHandler_UpdateUser_forbiddenSelfDemote(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	h := newUserHandler(users)
	r := userRouter(h, admin.ID)

	// Admin demoting themselves -> "admin cannot demote themselves" -> 403
	w := patchJSON(t, r, "/users/"+admin.ID.String(), map[string]string{"role": "user"})
	assert.Equal(t, http.StatusForbidden, w.Code)
	resp := parseAPIResponse(t, w)
	assert.Contains(t, resp.Message, "admin cannot demote themselves")
}

func TestUserHandler_UpdateUser_serviceError(t *testing.T) {
	h := failingUserHandler()
	r := userRouter(h, uuid.New())
	w := patchJSON(t, r, "/users/"+uuid.New().String(), map[string]string{"email": "x@test.com"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUserHandler_DeleteMe_forbiddenLastAdmin(t *testing.T) {
	users := mocks.NewUserStore()
	solo := users.SeedUser("solo@test.com", "hash", "admin")
	h := newUserHandler(users)
	r := userRouter(h, solo.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/me", nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
	resp := parseAPIResponse(t, w)
	assert.Contains(t, resp.Message, "cannot delete the last admin")
}

func TestUserHandler_DeleteMe_serviceError(t *testing.T) {
	h := failingUserHandler()
	r := userRouter(h, uuid.New())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/me", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DeleteUser success returns 204 and goes through the transaction commit path.
func TestUserHandler_DeleteUser_success(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	users.SeedUser("admin2@test.com", "hash", "admin") // ensure not the last admin
	target := users.SeedUser("target@test.com", "hash", "user")

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()

	svc := service.NewUserService(users, mocks.NewProjectStore(), nil)
	svcVal := reflect.ValueOf(svc).Elem()
	field := svcVal.FieldByName("txPool")
	rf := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	rf.Set(reflect.ValueOf(mock))

	h := NewUserHandler(svc)
	r := userRouter(h, admin.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+target.ID.String(), nil))
	assert.Equal(t, http.StatusNoContent, w.Code)
}
