package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)INSERT INTO projects.*VALUES.*\$1`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "my project", pgxmock.AnyArg(), "postgresql", "free", pgxmock.AnyArg(), "creating", pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		repo := NewProjectRepository(mock)
		project := &model.Project{
			UserID: uuid.New(),
			Name:   "my project",
			DBType: "postgresql",
		}
		err := repo.Create(context.Background(), project)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, project.ID)
		assert.Equal(t, "free", project.ResourceTier)
		assert.Equal(t, "creating", project.Status)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)INSERT INTO projects.*VALUES.*\$1`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "my project", pgxmock.AnyArg(), "postgresql", "free", pgxmock.AnyArg(), "creating", pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(errors.New("duplicate key"))

		repo := NewProjectRepository(mock)
		project := &model.Project{
			UserID: uuid.New(),
			Name:   "my project",
			DBType: "postgresql",
		}
		err := repo.Create(context.Background(), project)
		require.Error(t, err)
	})
}

func TestProjectRepository_GetByID(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	pid := uuid.New()
	columns := []string{"id", "user_id", "name", "description", "db_type", "resource_tier", "created_at", "status", "runtime_created_at", "runtime_updated_at"}

	t.Run("found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at FROM projects WHERE id = \$1`).
			WithArgs(pid).
			WillReturnRows(pgxmock.NewRows(columns).AddRow(pid, uuid.New(), "proj", nil, "postgresql", "free", now, "running", &now, &now))

		repo := NewProjectRepository(mock)
		project, err := repo.GetByID(context.Background(), pid)
		require.NoError(t, err)
		require.NotNil(t, project)
		assert.Equal(t, pid, project.ID)
		assert.Equal(t, "proj", project.Name)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id.*FROM projects WHERE id = \$1`).
			WithArgs(pid).
			WillReturnError(pgx.ErrNoRows)

		repo := NewProjectRepository(mock)
		project, err := repo.GetByID(context.Background(), pid)
		require.NoError(t, err)
		assert.Nil(t, project)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id.*FROM projects WHERE id = \$1`).
			WithArgs(pid).
			WillReturnError(errors.New("db down"))

		repo := NewProjectRepository(mock)
		_, err := repo.GetByID(context.Background(), pid)
		require.Error(t, err)
	})
}

func TestProjectRepository_GetByIDAndUserID(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	pid, uid := uuid.New(), uuid.New()
	columns := []string{"id", "user_id", "name", "description", "db_type", "resource_tier", "created_at", "status", "runtime_created_at", "runtime_updated_at"}

	t.Run("found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at FROM projects WHERE id = \$1 AND user_id = \$2`).
			WithArgs(pid, uid).
			WillReturnRows(pgxmock.NewRows(columns).AddRow(pid, uid, "proj", nil, "postgresql", "free", now, "running", &now, &now))

		repo := NewProjectRepository(mock)
		project, err := repo.GetByIDAndUserID(context.Background(), pid, uid)
		require.NoError(t, err)
		require.NotNil(t, project)
		assert.Equal(t, pid, project.ID)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id.*FROM projects WHERE id = \$1 AND user_id = \$2`).
			WithArgs(pid, uid).
			WillReturnError(pgx.ErrNoRows)

		repo := NewProjectRepository(mock)
		project, err := repo.GetByIDAndUserID(context.Background(), pid, uid)
		require.NoError(t, err)
		assert.Nil(t, project)
	})
}

func TestProjectRepository_GetByUserID(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	uid := uuid.New()
	columns := []string{"id", "user_id", "name", "description", "db_type", "resource_tier", "created_at", "status", "runtime_created_at", "runtime_updated_at"}

	t.Run("returns projects", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id, name, description, db_type, resource_tier, created_at, status, runtime_created_at, runtime_updated_at FROM projects WHERE user_id = \$1 ORDER BY created_at DESC`).
			WithArgs(uid).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow(uuid.New(), uid, "p1", nil, "postgresql", "free", now, "running", nil, nil).
				AddRow(uuid.New(), uid, "p2", nil, "mongodb", "basic", now, "creating", nil, nil))

		repo := NewProjectRepository(mock)
		projects, err := repo.GetByUserID(context.Background(), uid)
		require.NoError(t, err)
		assert.Len(t, projects, 2)
	})

	t.Run("empty", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id.*FROM projects WHERE user_id = \$1 ORDER BY created_at DESC`).
			WithArgs(uid).
			WillReturnRows(pgxmock.NewRows(columns))

		repo := NewProjectRepository(mock)
		projects, err := repo.GetByUserID(context.Background(), uid)
		require.NoError(t, err)
		assert.Empty(t, projects)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, user_id.*FROM projects WHERE user_id = \$1 ORDER BY created_at DESC`).
			WithArgs(uid).
			WillReturnError(errors.New("timeout"))

		repo := NewProjectRepository(mock)
		_, err := repo.GetByUserID(context.Background(), uid)
		require.Error(t, err)
	})
}

func TestProjectRepository_UpdateRuntimeStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE projects SET status = \$2, runtime_updated_at = \$3 WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg(), "running", pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		repo := NewProjectRepository(mock)
		err := repo.UpdateRuntimeStatus(context.Background(), uuid.New(), "running")
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE projects SET status.*WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg(), "running", pgxmock.AnyArg()).
			WillReturnError(errors.New("update failed"))

		repo := NewProjectRepository(mock)
		err := repo.UpdateRuntimeStatus(context.Background(), uuid.New(), "running")
		require.Error(t, err)
	})
}

func TestProjectRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE projects SET name = \$2, description = \$3, db_type = \$4, resource_tier = \$5 WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg(), "newname", pgxmock.AnyArg(), "postgresql", "premium").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		repo := NewProjectRepository(mock)
		project := &model.Project{ID: uuid.New(), Name: "newname", DBType: "postgresql", ResourceTier: "premium"}
		err := repo.Update(context.Background(), project)
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE projects SET name.*WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg(), "newname", pgxmock.AnyArg(), "postgresql", "premium").
			WillReturnError(errors.New("update failed"))

		repo := NewProjectRepository(mock)
		project := &model.Project{ID: uuid.New(), Name: "newname", DBType: "postgresql", ResourceTier: "premium"}
		err := repo.Update(context.Background(), project)
		require.Error(t, err)
	})
}

func TestProjectRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		repo := NewProjectRepository(mock)
		err := repo.Delete(context.Background(), uuid.New())
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("fk violation"))

		repo := NewProjectRepository(mock)
		err := repo.Delete(context.Background(), uuid.New())
		require.Error(t, err)
	})
}

func TestProjectRepository_DeleteByIDAndUserID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE id = \$1 AND user_id = \$2`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		repo := NewProjectRepository(mock)
		err := repo.DeleteByIDAndUserID(context.Background(), uuid.New(), uuid.New())
		require.NoError(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE id = \$1 AND user_id = \$2`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		repo := NewProjectRepository(mock)
		err := repo.DeleteByIDAndUserID(context.Background(), uuid.New(), uuid.New())
		assert.EqualError(t, err, "project not found or access denied")
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE id = \$1 AND user_id = \$2`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		repo := NewProjectRepository(mock)
		err := repo.DeleteByIDAndUserID(context.Background(), uuid.New(), uuid.New())
		require.Error(t, err)
	})
}

func TestProjectRepository_DeleteByUserIDTx(t *testing.T) {
	t.Run("nil tx", func(t *testing.T) {
		repo := NewProjectRepository(nil)
		err := repo.DeleteByUserIDTx(context.Background(), nil, uuid.New())
		assert.EqualError(t, err, "transaction is required")
	})

	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE user_id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 3))

		tx, err := mock.Begin(context.Background())
		require.NoError(t, err)

		repo := NewProjectRepository(mock)
		err = repo.DeleteByUserIDTx(context.Background(), tx, uuid.New())
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)DELETE FROM projects WHERE user_id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("exec failed"))

		tx, err := mock.Begin(context.Background())
		require.NoError(t, err)

		repo := NewProjectRepository(mock)
		err = repo.DeleteByUserIDTx(context.Background(), tx, uuid.New())
		require.Error(t, err)
	})
}
