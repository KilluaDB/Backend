package service

import (
	"context"
	"testing"

	"backend/internal/mongodb/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestCollectionService_ValidationAndConnErrors(t *testing.T) {
	conn := &mockInstanceConn{err: assert.AnError}
	svc := NewCollectionService(conn, nil)
	ctx := context.Background()
	uid := uuid.New()
	pid := uuid.New()

	// Invalid collection name
	err := svc.CreateCollection(ctx, uid, pid, "")
	assert.ErrorIs(t, err, ErrInvalidCollectionName)

	err = svc.DeleteCollection(ctx, uid, pid, "")
	assert.ErrorIs(t, err, ErrInvalidCollectionName)

	_, err = svc.AddField(ctx, uid, pid, "", model.AddFieldRequest{Field: "valid"})
	assert.ErrorIs(t, err, ErrInvalidCollectionName)

	_, err = svc.RemoveField(ctx, uid, pid, "", "valid")
	assert.ErrorIs(t, err, ErrInvalidCollectionName)

	// Invalid field name
	_, err = svc.AddField(ctx, uid, pid, "valid", model.AddFieldRequest{Field: "$invalid"})
	assert.ErrorIs(t, err, ErrInvalidFieldName)

	_, err = svc.RemoveField(ctx, uid, pid, "valid", "$invalid")
	assert.ErrorIs(t, err, ErrInvalidFieldName)

	// Conn error
	err = svc.CreateCollection(ctx, uid, pid, "valid")
	assert.ErrorIs(t, err, assert.AnError)

	err = svc.DeleteCollection(ctx, uid, pid, "valid")
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.AddField(ctx, uid, pid, "valid", model.AddFieldRequest{Field: "valid"})
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.RemoveField(ctx, uid, pid, "valid", "valid")
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.ListCollections(ctx, uid, pid)
	assert.ErrorIs(t, err, assert.AnError)
}
