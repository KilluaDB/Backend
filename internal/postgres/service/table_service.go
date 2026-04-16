package service

import (
	"backend/internal/postgres/repository"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TableService struct {
	instanceConn InstanceConnectionService
	tableRepo    *repository.TableRepository
}

func NewTableService(
	instanceConn InstanceConnectionService,
	tableRepo *repository.TableRepository,
) *TableService {
	return &TableService{
		instanceConn: instanceConn,
		tableRepo:    tableRepo,
	}
}

// withProjectPool runs fn with a shared project pool; callers must not Close the pool.
func withProjectPool[T any](s *TableService, ctx context.Context, userID, projectID uuid.UUID, fn func(*pgxpool.Pool) (T, error)) (T, error) {
	var zero T
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return zero, err
	}
	return fn(pool)
}

// withProjectPoolErr runs fn with a shared project pool when only an error is returned.
func withProjectPoolErr(s *TableService, ctx context.Context, userID, projectID uuid.UUID, fn func(*pgxpool.Pool) error) error {
	_, err := withProjectPool(s, ctx, userID, projectID, func(pool *pgxpool.Pool) (struct{}, error) {
		return struct{}{}, fn(pool)
	})
	return err
}
