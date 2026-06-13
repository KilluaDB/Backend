package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// NOTE: The CollectionService database methods (ListCollections, CreateCollection,
// DeleteCollection, AddField, RemoveField) drive a live *mongo.Database obtained
// from infra.InstanceConnectionService and delegate to repository.CollectionRepository.
// Exercising them deterministically requires a Mongo seam / mtest harness (an
// in-memory command-reply mock), which is out of scope for this task. The tests
// below cover only the pure, server-independent functions: validateCollectionName,
// validateFieldName, and isMongoCommandCode.

func TestValidateCollectionName(t *testing.T) {
	// The current implementation rejects only empty/whitespace-only names and
	// names containing a NUL byte. It deliberately does NOT reject long names,
	// reserved "system." prefixes, or other special characters, so those cases
	// are asserted as valid to lock in the real behavior.
	longName := ""
	for i := 0; i < 200; i++ {
		longName += "a"
	}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple valid", input: "users", wantErr: false},
		{name: "valid with underscore", input: "user_events", wantErr: false},
		{name: "valid with dot", input: "billing.invoices", wantErr: false},
		{name: "valid surrounding spaces trimmed", input: "  users  ", wantErr: false},
		{name: "reserved prefix accepted", input: "system.users", wantErr: false},
		{name: "dollar char accepted", input: "weird$name", wantErr: false},
		{name: "very long accepted", input: longName, wantErr: false},
		{name: "empty rejected", input: "", wantErr: true},
		{name: "whitespace only rejected", input: "   ", wantErr: true},
		{name: "tab/newline only rejected", input: "\t\n", wantErr: true},
		{name: "embedded NUL rejected", input: "bad\x00name", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCollectionName(tt.input)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidCollectionName)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateFieldName(t *testing.T) {
	// The current implementation rejects empty/whitespace-only names, names
	// starting with '$', and names containing a NUL byte. Dotted paths (used by
	// MongoDB for nested fields) are intentionally permitted.
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple valid", input: "status", wantErr: false},
		{name: "valid dotted path", input: "address.city", wantErr: false},
		{name: "valid with underscore", input: "created_at", wantErr: false},
		{name: "valid surrounding spaces trimmed", input: "  status  ", wantErr: false},
		{name: "internal dollar accepted", input: "a$b", wantErr: false},
		{name: "empty rejected", input: "", wantErr: true},
		{name: "whitespace only rejected", input: "   ", wantErr: true},
		{name: "dollar prefix rejected", input: "$set", wantErr: true},
		{name: "dollar prefix after trim rejected", input: "  $inc", wantErr: true},
		{name: "embedded NUL rejected", input: "bad\x00field", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFieldName(tt.input)
			if tt.wantErr {
				assert.ErrorIs(t, err, ErrInvalidFieldName)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsMongoCommandCode(t *testing.T) {
	const target = mongoNamespaceExistsCode // 48
	const other = mongoNamespaceNotFoundCode // 26

	tests := []struct {
		name  string
		err   error
		codes []int
		want  bool
	}{
		{
			name: "nil error",
			err:  nil,
			codes: []int{target},
			want: false,
		},
		{
			name: "plain non-command error",
			err:  errors.New("boom"),
			codes: []int{target},
			want: false,
		},
		{
			name: "command error matching code",
			err:  mongo.CommandError{Code: int32(target), Name: "NamespaceExists"},
			codes: []int{target},
			want: true,
		},
		{
			name: "command error non-matching code",
			err:  mongo.CommandError{Code: int32(other), Name: "NamespaceNotFound"},
			codes: []int{target},
			want: false,
		},
		{
			name: "command error matches one of several codes",
			err:  mongo.CommandError{Code: int32(other)},
			codes: []int{target, other},
			want: true,
		},
		{
			name: "wrapped command error matching code",
			err:  fmt.Errorf("op failed: %w", mongo.CommandError{Code: int32(target)}),
			codes: []int{target},
			want: true,
		},
		{
			name: "write exception matching code",
			err: mongo.WriteException{
				WriteErrors: mongo.WriteErrors{{Code: target, Message: "exists"}},
			},
			codes: []int{target},
			want: true,
		},
		{
			name: "write exception non-matching code",
			err: mongo.WriteException{
				WriteErrors: mongo.WriteErrors{{Code: other}},
			},
			codes: []int{target},
			want: false,
		},
		{
			name: "write exception empty errors",
			err:  mongo.WriteException{},
			codes: []int{target},
			want: false,
		},
		{
			name: "bulk write exception matching code",
			err: mongo.BulkWriteException{
				WriteErrors: []mongo.BulkWriteError{
					{WriteError: mongo.WriteError{Code: target}},
				},
			},
			codes: []int{target},
			want: true,
		},
		{
			name: "bulk write exception non-matching code",
			err: mongo.BulkWriteException{
				WriteErrors: []mongo.BulkWriteError{
					{WriteError: mongo.WriteError{Code: other}},
				},
			},
			codes: []int{target},
			want: false,
		},
		{
			name: "no codes provided",
			err:  mongo.CommandError{Code: int32(target)},
			codes: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isMongoCommandCode(tt.err, tt.codes...))
		})
	}
}
