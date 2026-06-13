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

func TestUserRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)INSERT INTO users.*VALUES.*\$1`).
			WithArgs(pgxmock.AnyArg(), "test@example.com", "hash", "user", "active", pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		repo := NewUserRepository(mock)
		user := &model.User{Email: "test@example.com", PasswordHash: "hash"}
		err := repo.Create(context.Background(), user)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, user.ID)
		assert.Equal(t, "user", user.Role)
		assert.Equal(t, "active", user.Status)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)INSERT INTO users.*VALUES.*\$1`).
			WithArgs(pgxmock.AnyArg(), "test@example.com", "hash", "user", "active", pgxmock.AnyArg()).
			WillReturnError(errors.New("duplicate key"))

		repo := NewUserRepository(mock)
		user := &model.User{Email: "test@example.com", PasswordHash: "hash"}
		err := repo.Create(context.Background(), user)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key")
	})
}

func TestUserRepository_FindUserByID(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	uid := uuid.New()
	columns := []string{"id", "email", "password_hash", "role", "status", "created_at", "last_login_at", "deleted_at"}

	t.Run("found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email, password_hash, role, status, created_at, last_login_at, deleted_at FROM users WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(uid).
			WillReturnRows(pgxmock.NewRows(columns).AddRow(uid, "a@b.com", "hash", "admin", "active", now, nil, nil))

		repo := NewUserRepository(mock)
		user, err := repo.FindUserByID(context.Background(), uid)
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, uid, user.ID)
		assert.Equal(t, "a@b.com", user.Email)
		assert.Equal(t, "admin", user.Role)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(uid).
			WillReturnError(pgx.ErrNoRows)

		repo := NewUserRepository(mock)
		user, err := repo.FindUserByID(context.Background(), uid)
		require.NoError(t, err)
		assert.Nil(t, user)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(uid).
			WillReturnError(errors.New("connection refused"))

		repo := NewUserRepository(mock)
		_, err := repo.FindUserByID(context.Background(), uid)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})
}

func TestUserRepository_FindUserByEmail(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	uid := uuid.New()
	columns := []string{"id", "email", "password_hash", "role", "status", "created_at", "last_login_at", "deleted_at"}

	t.Run("found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE email = \$1 AND deleted_at IS NULL`).
			WithArgs("a@b.com").
			WillReturnRows(pgxmock.NewRows(columns).AddRow(uid, "a@b.com", "hash", "user", "active", now, nil, nil))

		repo := NewUserRepository(mock)
		user, err := repo.FindUserByEmail(context.Background(), "a@b.com")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, uid, user.ID)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE email = \$1 AND deleted_at IS NULL`).
			WithArgs("missing@b.com").
			WillReturnError(pgx.ErrNoRows)

		repo := NewUserRepository(mock)
		user, err := repo.FindUserByEmail(context.Background(), "missing@b.com")
		require.NoError(t, err)
		assert.Nil(t, user)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE email = \$1 AND deleted_at IS NULL`).
			WithArgs("a@b.com").
			WillReturnError(errors.New("timeout"))

		repo := NewUserRepository(mock)
		_, err := repo.FindUserByEmail(context.Background(), "a@b.com")
		require.Error(t, err)
	})
}

func TestUserRepository_FindUserByEmailIncludingDeleted(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	uid := uuid.New()
	deletedAt := now.Add(-time.Hour)
	columns := []string{"id", "email", "password_hash", "role", "status", "created_at", "last_login_at", "deleted_at"}

	t.Run("found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE email = \$1 ORDER BY deleted_at DESC NULLS LAST LIMIT 1`).
			WithArgs("a@b.com").
			WillReturnRows(pgxmock.NewRows(columns).AddRow(uid, "a@b.com", "hash", "user", "deleted", now, nil, &deletedAt))

		repo := NewUserRepository(mock)
		user, err := repo.FindUserByEmailIncludingDeleted(context.Background(), "a@b.com")
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, uid, user.ID)
		assert.NotNil(t, user.DeletedAt)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE email = \$1 ORDER BY deleted_at DESC NULLS LAST LIMIT 1`).
			WithArgs("missing@b.com").
			WillReturnError(pgx.ErrNoRows)

		repo := NewUserRepository(mock)
		user, err := repo.FindUserByEmailIncludingDeleted(context.Background(), "missing@b.com")
		require.NoError(t, err)
		assert.Nil(t, user)
	})
}

func TestUserRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE users SET email = \$2, role = \$3, status = \$4 WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(pgxmock.AnyArg(), "new@b.com", "admin", "active").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		repo := NewUserRepository(mock)
		user := &model.User{ID: uuid.New(), Email: "new@b.com", Role: "admin", Status: "active"}
		err := repo.Update(context.Background(), user)
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE users SET email.*WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg(), "new@b.com", "admin", "active").
			WillReturnError(errors.New("constraint violation"))

		repo := NewUserRepository(mock)
		user := &model.User{ID: uuid.New(), Email: "new@b.com", Role: "admin", Status: "active"}
		err := repo.Update(context.Background(), user)
		require.Error(t, err)
	})
}

func TestUserRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE users SET deleted_at = NOW\(\), status = 'deleted' WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		repo := NewUserRepository(mock)
		err := repo.Delete(context.Background(), uuid.New())
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)UPDATE users SET deleted_at.*WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		repo := NewUserRepository(mock)
		err := repo.Delete(context.Background(), uuid.New())
		require.Error(t, err)
	})
}

func TestUserRepository_HardDeleteSoftDeletedByEmail(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM users WHERE email = \$1 AND deleted_at IS NOT NULL`).
			WithArgs("old@b.com").
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		repo := NewUserRepository(mock)
		err := repo.HardDeleteSoftDeletedByEmail(context.Background(), "old@b.com")
		require.NoError(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectExec(`(?s)DELETE FROM users WHERE email = \$1 AND deleted_at IS NOT NULL`).
			WithArgs("old@b.com").
			WillReturnError(errors.New("fk violation"))

		repo := NewUserRepository(mock)
		err := repo.HardDeleteSoftDeletedByEmail(context.Background(), "old@b.com")
		require.Error(t, err)
	})
}

func TestUserRepository_FindAll(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	uid1, uid2 := uuid.New(), uuid.New()
	columns := []string{"id", "email", "password_hash", "role", "status", "created_at", "last_login_at", "deleted_at"}

	t.Run("returns users", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email, password_hash, role, status, created_at, last_login_at, deleted_at FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC`).
			WillReturnRows(pgxmock.NewRows(columns).
				AddRow(uid1, "a@b.com", "h1", "user", "active", now, nil, nil).
				AddRow(uid2, "b@b.com", "h2", "admin", "active", now, nil, nil))

		repo := NewUserRepository(mock)
		users, err := repo.FindAll(context.Background())
		require.NoError(t, err)
		assert.Len(t, users, 2)
	})

	t.Run("empty", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC`).
			WillReturnRows(pgxmock.NewRows(columns))

		repo := NewUserRepository(mock)
		users, err := repo.FindAll(context.Background())
		require.NoError(t, err)
		assert.Empty(t, users)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT id, email.*FROM users WHERE deleted_at IS NULL ORDER BY created_at DESC`).
			WillReturnError(errors.New("disk full"))

		repo := NewUserRepository(mock)
		_, err := repo.FindAll(context.Background())
		require.Error(t, err)
	})
}

func TestUserRepository_CountUsers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM users WHERE deleted_at IS NULL`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(42)))

		repo := NewUserRepository(mock)
		count, err := repo.CountUsers(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 42, count)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM users WHERE deleted_at IS NULL`).
			WillReturnError(errors.New("timeout"))

		repo := NewUserRepository(mock)
		_, err := repo.CountUsers(context.Background())
		require.Error(t, err)
	})
}

func TestUserRepository_CountAdmins(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM users WHERE role = 'admin' AND deleted_at IS NULL`).
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(3)))

		repo := NewUserRepository(mock)
		count, err := repo.CountAdmins(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("query error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM users WHERE role = 'admin' AND deleted_at IS NULL`).
			WillReturnError(errors.New("permission denied"))

		repo := NewUserRepository(mock)
		_, err := repo.CountAdmins(context.Background())
		require.Error(t, err)
	})
}

func TestUserRepository_FindUserByName(t *testing.T) {
	repo := NewUserRepository(nil)
	user, err := repo.FindUserByName("any")
	assert.Nil(t, user)
	assert.EqualError(t, err, "not implemented")
}

func TestUserRepository_DeleteTx(t *testing.T) {
	t.Run("nil tx", func(t *testing.T) {
		repo := NewUserRepository(nil)
		err := repo.DeleteTx(context.Background(), nil, uuid.New())
		assert.EqualError(t, err, "transaction is required")
	})

	t.Run("success", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE users SET deleted_at = NOW\(\), status = 'deleted' WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		tx, err := mock.Begin(context.Background())
		require.NoError(t, err)

		repo := NewUserRepository(mock)
		err = repo.DeleteTx(context.Background(), tx, uuid.New())
		require.NoError(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE users SET deleted_at.*WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		tx, err := mock.Begin(context.Background())
		require.NoError(t, err)

		repo := NewUserRepository(mock)
		err = repo.DeleteTx(context.Background(), tx, uuid.New())
		assert.EqualError(t, err, "user not found")
	})

	t.Run("exec error", func(t *testing.T) {
		mock := newRepoMock(t)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE users SET deleted_at.*WHERE id = \$1`).
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("exec failed"))

		tx, err := mock.Begin(context.Background())
		require.NoError(t, err)

		repo := NewUserRepository(mock)
		err = repo.DeleteTx(context.Background(), tx, uuid.New())
		assert.EqualError(t, err, "exec failed")
	})
}
