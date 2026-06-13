package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCollectionName(t *testing.T) {
	assert.NoError(t, validateCollectionName("users"))
	assert.ErrorIs(t, validateCollectionName(""), ErrInvalidCollectionName)
	assert.ErrorIs(t, validateCollectionName("bad\x00name"), ErrInvalidCollectionName)
}

func TestValidateFieldName(t *testing.T) {
	assert.NoError(t, validateFieldName("field"))
	assert.ErrorIs(t, validateFieldName(""), ErrInvalidFieldName)
}
