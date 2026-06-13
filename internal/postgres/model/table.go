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
	Schema     string          `json:"schema"` // Omitted or empty → "public" in service
	Table      string          `json:"table" binding:"required"`
	References []ForeignKeyRef `json:"references" binding:"required,min=1"`
}

// AddColumnForeignKey is a flat foreign key entry attached to a single newly-added column.
// The local column is implicit (the column being added), so only the referenced side is specified.
type AddColumnForeignKey struct {
	Schema        string `json:"schema"` // Omitted or empty → "public" in service
	Table         string `json:"table" binding:"required"`
	ForeignColumn string `json:"foreign_column" binding:"required"`
	OnUpdate      string `json:"on_update"`
	OnDelete      string `json:"on_delete"`
}

// CreateTableRequest is the request body for creating a table.
type CreateTableRequest struct {
	Schema      string               `json:"schema"` // Omitted or empty → "public" in service
	Table       string               `json:"table" binding:"required"`
	Columns     []TableColumnDef     `json:"columns" binding:"required"`
	ForeignKeys []TableForeignKeyDef `json:"foreign_keys" binding:"omitempty,dive"`
}

// UpdateTableRequest is the request body for updating a table.
type UpdateTableRequest struct {
	Schema      string              `json:"schema"`
	Table       string              `json:"table"`
	Columns     []TableColumnDef    `json:"columns"`
	ForeignKeys *TableForeignKeyDef `json:"foreign_keys"`
}

// DeleteTableRequest is the request body for deleting a table.
type DeleteTableRequest struct {
	Schema string `json:"schema"` // Omitted or empty → "public" in service
	Table  string `json:"table" binding:"required"`
}

// GetRowsResult is the data payload for GET .../tables/:table/rows (paginated list).
type GetRowsResult struct {
	Rows    []map[string]interface{} `json:"rows"`
	Limit   int                      `json:"limit"`
	Offset  int                      `json:"offset"`
	HasMore bool                     `json:"has_more"`
	Total   *int64                   `json:"total,omitempty"`
}

// TableMetadata is returned by GET .../postgres/tables/:table (column layout, keys; no row data).
type TableMetadata struct {
	Schema      string         `json:"schema"`
	Table       string         `json:"table"`
	Columns     []ColumnDetail `json:"columns"`
	PrimaryKeys []string       `json:"primary_keys"`
	ForeignKeys []ForeignKey   `json:"foreign_keys"`
}

// TableIndexInfo describes one index on a table (from pg_catalog).
type TableIndexInfo struct {
	Name       string   `json:"name"`
	Table      string   `json:"table"`
	Columns    []string `json:"columns"`
	Unique     bool     `json:"unique"`
	Primary    bool     `json:"primary"`
	Method     string   `json:"method"`
	Definition string   `json:"definition"`
	Valid      bool     `json:"valid"`
}

// CreateIndexRequest is the body for POST .../tables/{table}/indexes.
type CreateIndexRequest struct {
	Name    string   `json:"name" binding:"required"`
	Columns []string `json:"columns" binding:"required,min=1,max=32"`
	Unique  bool     `json:"unique"`
	Method  string   `json:"method"` // btree (default), hash, gin, gist, spgist, brin
}
