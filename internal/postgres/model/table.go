package model

// TableOpResult is returned by CreateTable and DeleteTable for API response.
type TableOpResult struct {
	RowsAffected int64 `json:"rows_affected"`
}

// TableColumnDef is a column definition for create/update table requests.
type TableColumnDef struct {
	Name       string  `json:"name" binding:"required"`
	Type       string  `json:"type" binding:"required"`
	Default    *string `json:"default"`
	Primary    bool    `json:"primary"`
	IsUnique   bool    `json:"is_unique"`
	IsIdentity bool    `json:"is_identity"`
	Nullable   bool    `json:"nullable"`
}

// ForeignKeyRef is a single local/foreign column pair for a foreign key.
type ForeignKeyRef struct {
	LocalColumn   string `json:"local_column" binding:"required"`
	ForeignColumn string `json:"foreign_column" binding:"required"`
	OnUpdate      string `json:"on_update" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION"`
	OnDelete      string `json:"on_delete" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION SET NULL SET DEFAULT"`
}

// TableForeignKeyDef is foreign key definition for create/update table requests.
type TableForeignKeyDef struct {
	Schema     string          `json:"schema" binding:"required"`
	Table      string          `json:"table" binding:"required"`
	References []ForeignKeyRef `json:"references" binding:"required,min=1"`
}

// CreateTableRequest is the request body for creating a table.
type CreateTableRequest struct {
	Schema      string               `json:"schema" binding:"required"`
	Table       string               `json:"table" binding:"required"`
	Columns     []TableColumnDef     `json:"columns" binding:"required"`
	ForeignKeys *TableForeignKeyDef  `json:"foreign_keys"`
}

// UpdateTableRequest is the request body for updating a table.
type UpdateTableRequest struct {
	Schema      string               `json:"schema"`
	Table       string               `json:"table"`
	Columns     []TableColumnDef     `json:"columns"`
	ForeignKeys *TableForeignKeyDef  `json:"foreign_keys"`
}

// DeleteTableRequest is the request body for deleting a table.
type DeleteTableRequest struct {
	Schema string `json:"schema" binding:"required"`
	Table  string `json:"table" binding:"required"`
}
