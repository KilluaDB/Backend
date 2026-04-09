package model

type Column struct {
	Name     string
	DataType string
	Nullable bool
}

type ForeignKey struct {
	ConstraintName string `json:"constraint_name"`
	FromColumn     string `json:"from_column"`
	ToSchema       string `json:"to_schema"`
	ToTable        string `json:"to_table"`
	ToColumn       string `json:"to_column"`
	UpdateRule     string `json:"update_rule"`
	DeleteRule     string `json:"delete_rule"`
}

// ColumnDetail is introspection data for PATCH table column sync.
type ColumnDetail struct {
	Name          string  `json:"name"`
	DataType      string  `json:"data_type"`
	UdtName       string  `json:"udt_name"`
	CharMaxLength *int    `json:"char_max_length,omitempty"`
	IsNullable    bool    `json:"is_nullable"`
	ColumnDefault *string `json:"column_default,omitempty"`
	IsIdentity    bool    `json:"is_identity"`
}

type Table struct {
	Name        string
	Columns     []Column
	PrimaryKeys []string
	ForeignKeys []ForeignKey
}

type Relationship struct {
	FromTable string
	ToTable   string
	Type      string // "||--o{", "||--||", etc.
}
