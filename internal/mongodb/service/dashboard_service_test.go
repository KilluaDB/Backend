package service

import (
	"context"
	"errors"
	"testing"

	"backend/internal/mongodb/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNewMongoDashboardMetricsService(t *testing.T) {
	conn := &mockInstanceConn{}
	colRepo := repository.NewCollectionRepository()
	docRepo := repository.NewDocumentRepository()

	svc := NewMongoDashboardMetricsService(conn, colRepo, docRepo)
	assert.NotNil(t, svc)
}

func TestToInt64(t *testing.T) {
	assert.Equal(t, int64(42), toInt64(int32(42)))
	assert.Equal(t, int64(42), toInt64(int64(42)))
	assert.Equal(t, int64(42), toInt64(float64(42.5)))
	assert.Equal(t, int64(42), toInt64(int(42)))
	assert.Equal(t, int64(0), toInt64("not-a-number"))
}

func TestToFloat64(t *testing.T) {
	assert.Equal(t, float64(42.5), toFloat64(float64(42.5)))
	assert.Equal(t, float64(42), toFloat64(int32(42)))
	assert.Equal(t, float64(42), toFloat64(int64(42)))
	assert.Equal(t, float64(42), toFloat64(int(42)))
	assert.Equal(t, float64(0), toFloat64("not-a-number"))
}

func TestBsonGet(t *testing.T) {
	d := bson.D{{Key: "a", Value: 1}, {Key: "b", Value: "test"}}
	assert.Equal(t, 1, bsonGet(d, "a"))
	assert.Equal(t, "test", bsonGet(d, "b"))
	assert.Nil(t, bsonGet(d, "c"))
}

func TestMongoDashboardMetricsService_GetMetrics_ConnError(t *testing.T) {
	conn := &mockInstanceConn{err: errors.New("conn error")}
	svc := NewMongoDashboardMetricsService(conn, nil, nil)

	_, err := svc.GetMetrics(context.Background(), uuid.New(), uuid.New())
	assert.ErrorContains(t, err, "conn error")
}
