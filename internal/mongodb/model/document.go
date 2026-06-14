package model

type InsertDocumentsRequest struct {
	Documents []map[string]interface{} `json:"documents" binding:"required,min=1"`
}

type InsertDocumentResult struct {
	InsertedCount int64         `json:"inserted_count"`
	InsertedIDs   []interface{} `json:"inserted_ids"`
}

type UpdateDocumentsRequest struct {
	Filter    map[string]interface{} `json:"filter" binding:"required"`
	Update    map[string]interface{} `json:"update" binding:"required"`
	Upsert    *bool                  `json:"upsert,omitempty"`
	UpdateOne *bool                  `json:"update_one,omitempty"`
}

type UpdateDocumentsResult struct {
	Matched  int64       `json:"matched"`
	Modified int64       `json:"modified"`
	Upserted interface{} `json:"upserted_id,omitempty"`
}

type DeleteDocumentsRequest struct {
	Filter    map[string]interface{} `json:"filter"`
}

type DeleteDocumentsResult struct {
	Deleted int64 `json:"deleted"`
}

type QueryDocumentsRequest struct {
	Filter map[string]interface{} `json:"filter,omitempty"`
	Sort   map[string]interface{} `json:"sort,omitempty"`
	Page   int64                  `json:"page,omitempty"`
	Limit  int64                  `json:"limit,omitempty"`
}

type QueryDocumentsResult struct {
	Documents []map[string]interface{} `json:"documents"`
	Total     int64                    `json:"total"`
	Page      int64                    `json:"page"`
	Limit     int64                    `json:"limit"`
}

type CountDocumentsRequest struct {
	Filter map[string]interface{} `json:"filter,omitempty"`
}

type CountDocumentsResult struct {
	Count int64 `json:"count"`
}

type GetDocumentsRequest struct {
	// Limit restricts the number of documents returned (e.g., 20)
	Limit int64 `json:"limit,omitempty" form:"limit,default=20"`

	// Page or Cursor for pagination offsets
	Page int64 `json:"page,omitempty" form:"page,default=1"`
}

type GetDocumentsResult struct {
	Documents []map[string]interface{} `json:"documents"`
	Total     int64                    `json:"total"`
	Page      int64                    `json:"page"`
	Limit     int64                    `json:"limit"`
}

type UpdateFieldRequest struct {
	Value interface{} `json:"value"`
	Type  string      `json:"type,omitempty"` // "string", "int", "double", "boolean", "date", "null"
}

type AddDocumentFieldRequest struct {
	Field string      `json:"field" binding:"required"`
	Value interface{} `json:"value"`
	Type  string      `json:"type" binding:"required"`
}
