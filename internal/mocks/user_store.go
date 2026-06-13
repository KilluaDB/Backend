package mocks

import (
	"backend/internal/model"
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserStore is an in-memory UserStore for tests.
type UserStore struct {
	mu    sync.Mutex
	users map[uuid.UUID]*model.User
	byEmail map[string]uuid.UUID
}

func NewUserStore() *UserStore {
	return &UserStore{
		users:   make(map[uuid.UUID]*model.User),
		byEmail: make(map[string]uuid.UUID),
	}
}

func (m *UserStore) Create(ctx context.Context, user *model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user.Prepare()
	cp := *user
	m.users[user.ID] = &cp
	m.byEmail[user.Email] = user.ID
	return nil
}

func (m *UserStore) FindUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok || u.DeletedAt != nil {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (m *UserStore) FindUserByEmail(ctx context.Context, email string) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byEmail[email]
	if !ok {
		return nil, nil
	}
	u := m.users[id]
	if u == nil || u.DeletedAt != nil {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (m *UserStore) FindUserByEmailIncludingDeleted(ctx context.Context, email string) (*model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byEmail[email]
	if !ok {
		return nil, nil
	}
	cp := *m.users[id]
	return &cp, nil
}

func (m *UserStore) HardDeleteSoftDeletedByEmail(ctx context.Context, email string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byEmail[email]
	if !ok {
		return nil
	}
	delete(m.users, id)
	delete(m.byEmail, email)
	return nil
}

func (m *UserStore) Update(ctx context.Context, user *model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *user
	m.users[user.ID] = &cp
	m.byEmail[user.Email] = user.ID
	return nil
}

func (m *UserStore) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		now := u.CreatedAt
		if now.IsZero() {
			now = u.CreatedAt
		}
		t := now
		u.DeletedAt = &t
		u.Status = "deleted"
	}
	return nil
}

func (m *UserStore) DeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return m.Delete(ctx, id)
}

func (m *UserStore) FindAll(ctx context.Context) ([]model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.User, 0, len(m.users))
	for _, u := range m.users {
		if u.DeletedAt == nil {
			out = append(out, *u)
		}
	}
	return out, nil
}

func (m *UserStore) CountUsers(ctx context.Context) (int, error) {
	all, err := m.FindAll(ctx)
	return len(all), err
}

func (m *UserStore) CountAdmins(ctx context.Context) (int, error) {
	all, err := m.FindAll(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range all {
		if u.Role == "admin" {
			n++
		}
	}
	return n, nil
}

// SeedUser adds a user with hashed password for login tests.
func (m *UserStore) SeedUser(email, passwordHash, role string) *model.User {
	u := &model.User{Email: email, PasswordHash: passwordHash, Role: role, Status: "active"}
	_ = m.Create(context.Background(), u)
	return u
}
