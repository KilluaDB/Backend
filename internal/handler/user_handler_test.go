package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"unsafe"

	"backend/internal/mocks"
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/response"
	"backend/internal/service"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userStoreFailingFindByID struct {
	repository.UserStore
}

func (s *userStoreFailingFindByID) FindUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	return nil, errors.New("db failure")
}

type userStoreFailingFindAll struct {
	repository.UserStore
}

func (s *userStoreFailingFindAll) FindAll(ctx context.Context) ([]model.User, error) {
	return nil, errors.New("db failure")
}

func newUserHandler(users *mocks.UserStore) *UserHandler {
	return NewUserHandler(service.NewUserService(users, mocks.NewProjectStore(), nil))
}

func userRouter(h *UserHandler, userID uuid.UUID) *gin.Engine {
	r := gin.New()
	withUser := func(c *gin.Context) {
		if userID != uuid.Nil {
			c.Set(utils.UserIDContextKey, userID)
		}
	}
	r.GET("/users/me", withUser, h.GetMe)
	r.GET("/users/:user_id", h.GetUser)
	r.PATCH("/users/me", withUser, h.UpdateMe)
	r.PATCH("/users/:user_id", withUser, h.UpdateUser)
	r.DELETE("/users/me", withUser, h.DeleteMe)
	r.DELETE("/users/:user_id", withUser, h.DeleteUser)
	r.GET("/users", h.ListUsers)
	return r
}

func parseAPIResponse(t *testing.T, w *httptest.ResponseRecorder) response.APIResponse {
	t.Helper()
	var resp response.APIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestUserHandler_GetMe(t *testing.T) {
	users := mocks.NewUserStore()
	u := users.SeedUser("me@test.com", "hash", "user")
	h := newUserHandler(users)

	tests := []struct {
		name       string
		userID     uuid.UUID
		wantStatus int
	}{
		{"success", u.ID, http.StatusOK},
		{"unauthorized", uuid.Nil, http.StatusUnauthorized},
		{"not found", uuid.New(), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := userRouter(h, tt.userID)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestUserHandler_GetUser(t *testing.T) {
	users := mocks.NewUserStore()
	u := users.SeedUser("u@test.com", "hash", "user")
	h := newUserHandler(users)
	r := userRouter(h, uuid.Nil)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"success", "/users/" + u.ID.String(), http.StatusOK},
		{"invalid id", "/users/not-a-uuid", http.StatusBadRequest},
		{"not found", "/users/" + uuid.New().String(), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestUserHandler_GetUser_serviceError(t *testing.T) {
	users := &userStoreFailingFindByID{UserStore: mocks.NewUserStore()}
	svc := service.NewUserService(users, mocks.NewProjectStore(), nil)
	h := NewUserHandler(svc)
	r := userRouter(h, uuid.Nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/"+uuid.New().String(), nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUserHandler_UpdateMe(t *testing.T) {
	users := mocks.NewUserStore()
	u := users.SeedUser("u@test.com", "hash", "user")
	h := newUserHandler(users)
	r := userRouter(h, u.ID)

	tests := []struct {
		name       string
		body       any
		wantStatus int
	}{
		{"success email", map[string]string{"email": "new@test.com"}, http.StatusOK},
		{"forbidden role change", map[string]string{"role": "admin"}, http.StatusForbidden},
		{"invalid body", map[string]int{"email": 1}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/users/me", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestUserHandler_UpdateUser(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	target := users.SeedUser("target@test.com", "hash", "user")
	h := newUserHandler(users)

	tests := []struct {
		name       string
		userID     string
		body       any
		wantStatus int
	}{
		{"admin promotes user", target.ID.String(), map[string]string{"role": "admin"}, http.StatusOK},
		{"invalid user id", "bad", map[string]string{}, http.StatusBadRequest},
		{"unauthorized", target.ID.String(), map[string]string{"email": "x@y.com"}, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routerUser := admin.ID
			if tt.name == "unauthorized" {
				routerUser = uuid.Nil
			}
			router := userRouter(h, routerUser)
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/users/"+tt.userID, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestUserHandler_DeleteUser(t *testing.T) {
	users := mocks.NewUserStore()
	admin1 := users.SeedUser("a1@test.com", "hash", "admin")
	admin2 := users.SeedUser("a2@test.com", "hash", "admin")
	h := newUserHandler(users)
	router := userRouter(h, admin1.ID)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+admin2.ID.String(), nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
	resp := parseAPIResponse(t, w)
	assert.Contains(t, resp.Message, "admins cannot delete other admins")
}

func TestUserHandler_DeleteMe(t *testing.T) {
	users := mocks.NewUserStore()
	users.SeedUser("u@test.com", "hash", "user")
	h := newUserHandler(users)

	t.Run("unauthorized", func(t *testing.T) {
		r := userRouter(h, uuid.Nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/me", nil))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		r := userRouter(h, uuid.New())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/me", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestUserHandler_GetUser_unauthorizedPaths(t *testing.T) {
	h := newUserHandler(mocks.NewUserStore())
	r := userRouter(h, uuid.Nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPatch, "/users/"+uuid.New().String(), bytes.NewReader([]byte(`{}`))))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserHandler_ListUsers(t *testing.T) {
	users := mocks.NewUserStore()
	users.SeedUser("a@test.com", "hash", "user")
	users.SeedUser("b@test.com", "hash", "user")
	h := newUserHandler(users)
	r := userRouter(h, uuid.Nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	resp := parseAPIResponse(t, w)
	assert.Equal(t, "success", resp.Status)
}

func TestUserHandler_DeleteMe_success(t *testing.T) {
	users := mocks.NewUserStore()
	user := users.SeedUser("regular@test.com", "hash", "user")
	users.SeedUser("admin@test.com", "hash", "admin")

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
	r := userRouter(h, user.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/me", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUserHandler_UpdateMe_notFound(t *testing.T) {
	users := mocks.NewUserStore()
	h := newUserHandler(users)
	r := userRouter(h, uuid.New())

	body, _ := json.Marshal(map[string]string{"email": "x@test.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/users/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserHandler_UpdateUser_notFound(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	h := newUserHandler(users)
	r := userRouter(h, admin.ID)

	body, _ := json.Marshal(map[string]string{"email": "x@test.com"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/users/"+uuid.New().String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserHandler_DeleteUser_notFound(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	h := newUserHandler(users)
	r := userRouter(h, admin.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+uuid.New().String(), nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserHandler_DeleteUser_unauthorized(t *testing.T) {
	users := mocks.NewUserStore()
	h := newUserHandler(users)
	r := userRouter(h, uuid.Nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+uuid.New().String(), nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUserHandler_DeleteUser_invalidID(t *testing.T) {
	users := mocks.NewUserStore()
	admin := users.SeedUser("admin@test.com", "hash", "admin")
	h := newUserHandler(users)
	r := userRouter(h, admin.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/not-a-uuid", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUserHandler_DeleteUser_serviceError(t *testing.T) {
	users := &userStoreFailingFindByID{UserStore: mocks.NewUserStore()}
	svc := service.NewUserService(users, mocks.NewProjectStore(), nil)
	h := NewUserHandler(svc)
	r := userRouter(h, uuid.New())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+uuid.New().String(), nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUserHandler_DeleteUser_selfDelete(t *testing.T) {
	users := mocks.NewUserStore()
	solo := users.SeedUser("solo@test.com", "hash", "admin")
	h := newUserHandler(users)
	r := userRouter(h, solo.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+solo.ID.String(), nil))
	assert.Equal(t, http.StatusForbidden, w.Code)
	resp := parseAPIResponse(t, w)
	assert.Contains(t, resp.Message, "cannot delete the last admin")
}

func TestUserHandler_ListUsers_empty(t *testing.T) {
	h := newUserHandler(mocks.NewUserStore())
	r := userRouter(h, uuid.Nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	resp := parseAPIResponse(t, w)
	assert.Equal(t, "success", resp.Status)
}

func TestUserHandler_GetMe_serviceError(t *testing.T) {
	users := &userStoreFailingFindByID{UserStore: mocks.NewUserStore()}
	svc := service.NewUserService(users, mocks.NewProjectStore(), nil)
	h := NewUserHandler(svc)
	r := userRouter(h, uuid.New())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUserHandler_ListUsers_serviceError(t *testing.T) {
	users := &userStoreFailingFindAll{UserStore: mocks.NewUserStore()}
	svc := service.NewUserService(users, mocks.NewProjectStore(), nil)
	h := NewUserHandler(svc)
	r := userRouter(h, uuid.Nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUserHandler_GetMe_passwordNotExposed(t *testing.T) {
	users := mocks.NewUserStore()
	u := users.SeedUser("pw@test.com", "super-secret-hash", "user")
	h := newUserHandler(users)
	r := userRouter(h, u.ID)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/me", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "password_hash")
	assert.NotContains(t, w.Body.String(), "super-secret-hash")
}
