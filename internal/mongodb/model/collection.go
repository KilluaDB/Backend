package model

// CreateCollectionRequest represents a request to create a new collection.
type CreateCollectionRequest struct {
	Name string `json:"name" binding:"required"`
}

// AddFieldRequest represents a request to add a field to all documents.
type AddFieldRequest struct {
	Field          string      `json:"field" binding:"required"`
	Default        interface{} `json:"default,omitempty"`
	UpdateExisting *bool       `json:"update_existing,omitempty"`
}

// FieldUpdateResult captures matched/modified document counts from update operations.
type FieldUpdateResult struct {
	Matched  int64 `json:"matched"`
	Modified int64 `json:"modified"`
}
