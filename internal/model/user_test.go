package model

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUser_Prepare(t *testing.T) {
	u := &User{Email: "  test@Example.com  "}
	u.Prepare()
	assert.Equal(t, "test@Example.com", u.Email)
	assert.NotEqual(t, uuid.Nil, u.ID)

	u2 := &User{ID: uuid.New(), Email: "a@b.com"}
	id := u2.ID
	u2.Prepare()
	assert.Equal(t, id, u2.ID)
}
