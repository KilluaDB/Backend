package models

type Column struct {
	Name     string
	DataType string
	Nullable bool
}

type ForeignKey struct {
	ConstraintName string
	FromColumn     string
	ToTable        string
	ToColumn       string
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
