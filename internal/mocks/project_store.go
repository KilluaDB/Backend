package mocks

import (
	"backend/internal/model"
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ProjectStore is an in-memory ProjectStore for tests.
type ProjectStore struct {
	mu       sync.Mutex
	projects map[uuid.UUID]*model.Project
}

func NewProjectStore() *ProjectStore {
	return &ProjectStore{projects: make(map[uuid.UUID]*model.Project)}
}

func (m *ProjectStore) Create(ctx context.Context, project *model.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	project.Prepare()
	cp := *project
	m.projects[project.ID] = &cp
	return nil
}

func (m *ProjectStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *ProjectStore) GetByIDAndUserID(ctx context.Context, id, userID uuid.UUID) (*model.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok || p.UserID != userID {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *ProjectStore) GetByUserID(ctx context.Context, userID uuid.UUID) ([]model.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Project
	for _, p := range m.projects {
		if p.UserID == userID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *ProjectStore) UpdateRuntimeStatus(ctx context.Context, id uuid.UUID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok {
		p.Status = status
	}
	return nil
}

func (m *ProjectStore) Update(ctx context.Context, project *model.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *project
	m.projects[project.ID] = &cp
	return nil
}

func (m *ProjectStore) Delete(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.projects, id)
	return nil
}

func (m *ProjectStore) DeleteByIDAndUserID(ctx context.Context, id, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok || p.UserID != userID {
		return nil
	}
	delete(m.projects, id)
	return nil
}

func (m *ProjectStore) DeleteByUserIDTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.projects {
		if p.UserID == userID {
			delete(m.projects, id)
		}
	}
	return nil
}

// SeedProject adds a project for middleware/handler tests.
func (m *ProjectStore) SeedProject(userID uuid.UUID, dbType string) *model.Project {
	p := &model.Project{UserID: userID, Name: "test", DBType: dbType, ResourceTier: "free", Status: "running"}
	_ = m.Create(context.Background(), p)
	return p
}
