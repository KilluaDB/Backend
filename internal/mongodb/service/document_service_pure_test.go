package service

import (
	"context"
	"testing"
	"time"

	"backend/internal/mongodb/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestValidateUpdateOperators(t *testing.T) {
	t.Run("valid operators", func(t *testing.T) {
		err := validateUpdateOperators(map[string]interface{}{
			"$set": map[string]interface{}{"a": 1},
			"$inc": map[string]interface{}{"b": 1},
		})
		assert.NoError(t, err)
	})

	t.Run("unknown operator", func(t *testing.T) {
		err := validateUpdateOperators(map[string]interface{}{
			"$unknown": map[string]interface{}{"a": 1},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported update operator: $unknown")
	})

	t.Run("missing prefix", func(t *testing.T) {
		err := validateUpdateOperators(map[string]interface{}{
			"set": map[string]interface{}{"a": 1},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "update must use update operators")
	})
}

func TestValidateSameType(t *testing.T) {
	t.Run("same type", func(t *testing.T) {
		assert.NoError(t, validateSameType("hello", "world"))
		assert.NoError(t, validateSameType(int64(5), int64(10)))
		assert.NoError(t, validateSameType(true, false))
	})

	t.Run("int to float", func(t *testing.T) {
		// Existing int32, incoming float64
		assert.NoError(t, validateSameType(int32(5), float64(5.5)))
		// Existing int64, incoming float64
		assert.NoError(t, validateSameType(int64(5), float64(5.5)))
	})

	t.Run("type mismatch", func(t *testing.T) {
		err := validateSameType("hello", int64(5))
		assert.ErrorIs(t, err, ErrTypeMismatch)
	})

	t.Run("nil values", func(t *testing.T) {
		assert.NoError(t, validateSameType(nil, "world"))
		assert.NoError(t, validateSameType("hello", nil))
		assert.NoError(t, validateSameType(nil, nil))
	})
}

func TestCastToType(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		v, err := castToType(123, "string")
		assert.NoError(t, err)
		assert.Equal(t, "123", v)
	})

	t.Run("int32", func(t *testing.T) {
		v, err := castToType(123.5, "int32")
		assert.NoError(t, err)
		assert.Equal(t, int32(123), v)
	})

	t.Run("int64", func(t *testing.T) {
		v, err := castToType(int32(123), "int64")
		assert.NoError(t, err)
		assert.Equal(t, int64(123), v)
	})

	t.Run("double", func(t *testing.T) {
		v, err := castToType(123, "double")
		assert.NoError(t, err)
		assert.Equal(t, float64(123), v)
	})

	t.Run("boolean valid", func(t *testing.T) {
		v, err := castToType(true, "boolean")
		assert.NoError(t, err)
		assert.Equal(t, true, v)
	})

	t.Run("boolean invalid", func(t *testing.T) {
		_, err := castToType("true", "boolean")
		assert.Error(t, err)
	})

	t.Run("date valid", func(t *testing.T) {
		str := "2023-01-01T15:04:05Z"
		v, err := castToType(str, "date")
		assert.NoError(t, err)
		parsedTime, _ := time.Parse(time.RFC3339, str)
		assert.Equal(t, parsedTime, v)
	})

	t.Run("date invalid", func(t *testing.T) {
		_, err := castToType(123, "date")
		assert.Error(t, err)
	})

	t.Run("null", func(t *testing.T) {
		v, err := castToType("something", "null")
		assert.NoError(t, err)
		assert.Nil(t, v)
	})

	t.Run("object valid", func(t *testing.T) {
		obj := map[string]interface{}{"a": 1}
		v, err := castToType(obj, "object")
		assert.NoError(t, err)
		assert.Equal(t, obj, v)
	})

	t.Run("object invalid", func(t *testing.T) {
		_, err := castToType("not-obj", "object")
		assert.Error(t, err)
	})

	t.Run("array valid", func(t *testing.T) {
		arr := []interface{}{1, 2, 3}
		v, err := castToType(arr, "array")
		assert.NoError(t, err)
		assert.Equal(t, arr, v)
	})

	t.Run("array invalid", func(t *testing.T) {
		_, err := castToType("not-arr", "array")
		assert.Error(t, err)
	})

	t.Run("empty string type", func(t *testing.T) {
		v, err := castToType(123, "")
		assert.NoError(t, err)
		assert.Equal(t, 123, v)
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := castToType(123, "unknown")
		assert.Error(t, err)
	})
}

func TestMapToBsonD(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		doc, err := mapToBsonD(map[string]interface{}{})
		assert.NoError(t, err)
		assert.Equal(t, bson.D{}, doc)
	})

	t.Run("valid map", func(t *testing.T) {
		m := map[string]interface{}{
			"a": 1,
			"b": "string",
		}
		doc, err := mapToBsonD(m)
		assert.NoError(t, err)
		
		// The unmarshaled result will have the correct elements, though order is preserved from BSON serialization
		// Verify that both keys exist
		foundA, foundB := false, false
		for _, e := range doc {
			if e.Key == "a" {
				foundA = true
			}
			if e.Key == "b" {
				foundB = true
			}
		}
		assert.True(t, foundA)
		assert.True(t, foundB)
	})
}

func TestParseDocumentID(t *testing.T) {
	t.Run("valid ObjectID hex", func(t *testing.T) {
		hex := "507f1f77bcf86cd799439011"
		id := parseDocumentID(hex)
		oid, ok := id.(bson.ObjectID)
		assert.True(t, ok)
		assert.Equal(t, hex, oid.Hex())
	})

	t.Run("invalid ObjectID hex string", func(t *testing.T) {
		str := "not-an-object-id"
		id := parseDocumentID(str)
		assert.Equal(t, str, id)
	})

	t.Run("empty string", func(t *testing.T) {
		id := parseDocumentID("")
		assert.Equal(t, "", id)
	})
}

func TestDocumentService_ConnError(t *testing.T) {
	conn := &mockInstanceConn{err: assert.AnError}
	svc := NewDocumentService(conn, nil)

	ctx := context.Background()
	uid := uuid.New()
	pid := uuid.New()
	col := "test"

	_, err := svc.InsertDocuments(ctx, uid, pid, col, model.InsertDocumentsRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.GetDocumentByID(ctx, uid, pid, col, "123")
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.GetDocuments(ctx, uid, pid, col, model.GetDocumentsRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.QueryDocuments(ctx, uid, pid, col, model.QueryDocumentsRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.CountDocuments(ctx, uid, pid, col, model.CountDocumentsRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.UpdateDocuments(ctx, uid, pid, col, model.UpdateDocumentsRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	_, err = svc.DeleteDocuments(ctx, uid, pid, col, model.DeleteDocumentsRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	err = svc.UpdateDocumentField(ctx, uid, pid, col, "123", "f", model.UpdateFieldRequest{})
	assert.ErrorIs(t, err, assert.AnError)

	err = svc.AddDocumentField(ctx, uid, pid, col, "123", model.AddDocumentFieldRequest{Field: "valid_field"})
	assert.ErrorIs(t, err, assert.AnError)

	err = svc.DeleteDocumentField(ctx, uid, pid, col, "123", "f")
	assert.ErrorIs(t, err, assert.AnError)

	err = svc.DeleteDocument(ctx, uid, pid, col, "123")
	assert.ErrorIs(t, err, assert.AnError)
}

func TestDocumentService_FieldOperations(t *testing.T) {
	conn := &mockInstanceConn{err: nil}
	svc := NewDocumentService(conn, nil)
	ctx := context.Background()
	uid := uuid.New()
	pid := uuid.New()

	t.Run("UpdateDocumentField invalid collection", func(t *testing.T) {
		err := svc.UpdateDocumentField(ctx, uid, pid, "", "123", "f", model.UpdateFieldRequest{})
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})

	t.Run("UpdateDocumentField invalid field", func(t *testing.T) {
		err := svc.UpdateDocumentField(ctx, uid, pid, "col", "123", "$invalid", model.UpdateFieldRequest{})
		assert.ErrorIs(t, err, ErrInvalidFieldName)
	})

	t.Run("AddDocumentField invalid collection", func(t *testing.T) {
		err := svc.AddDocumentField(ctx, uid, pid, "", "123", model.AddDocumentFieldRequest{Field: "valid"})
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})

	t.Run("DeleteDocumentField invalid collection", func(t *testing.T) {
		err := svc.DeleteDocumentField(ctx, uid, pid, "", "123", "valid")
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})
	
	t.Run("DeleteDocument invalid collection", func(t *testing.T) {
		err := svc.DeleteDocument(ctx, uid, pid, "", "123")
		assert.ErrorIs(t, err, ErrInvalidCollectionName)
	})
}
