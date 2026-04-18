package service

import (
	"backend/internal/postgres/infra"
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	"backend/internal/utils"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for schema operations so handlers can return proper HTTP status.
var (
	ErrInvalidSchema = errors.New("invalid schema name")
)

const (
	maxJunctionTableColumns = 6
	minJunctionTableFKs     = 2
)

type SchemaService struct {
	instanceConn infra.InstanceConnectionService
}

// NewSchemaService creates a new SchemaService
func NewSchemaService(instanceConn infra.InstanceConnectionService) *SchemaService {
	return &SchemaService{
		instanceConn: instanceConn,
	}
}

// isValidSchemaName checks PostgreSQL schema name (similar to identifier rules).
func isValidSchemaName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_$]*$`, name)
	return matched
}

// VisualizeSchema generates a Mermaid ER diagram for a project's database schema
func (s *SchemaService) VisualizeSchema(userID uuid.UUID, projectID uuid.UUID, schema string) (string, error) {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "public"
	}
	if !isValidSchemaName(schema) {
		return "", ErrInvalidSchema
	}

	ctx := context.Background()
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return "", err
	}

	schemaRepo := repository.NewSchemaRepository(pool)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	mermaidDiagram, err := GenerateSchemaVisualization(ctx2, schemaRepo, schema)
	if err != nil {
		return "", fmt.Errorf("failed to generate schema visualization: %w", err)
	}
	return mermaidDiagram, nil
}

// ListSchemas returns schema names available in the project's database.
func (s *SchemaService) ListSchemas(ctx context.Context, userID, projectID uuid.UUID) ([]string, error) {
	pool, err := s.instanceConn.GetPool(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	repo := repository.NewSchemaRepository(pool)
	return repo.ListSchemas(ctx)
}

func parseTables(ctx context.Context, schemaRepo *repository.SchemaRepository, schema string) ([]model.Table, error) {
	tableNames, err := schemaRepo.GetTables(ctx, schema)
	if err != nil {
		return nil, err
	}

	tables := make([]model.Table, 0, len(tableNames))

	for _, tableName := range tableNames {
		table := model.Table{Name: tableName}

		// Get columns
		columns, err := schemaRepo.GetColumns(ctx, schema, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get columns for %s: %w", tableName, err)
		}
		table.Columns = columns

		// Get primary keys
		pks, err := schemaRepo.GetPrimaryKeys(ctx, schema, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get primary keys for %s: %w", tableName, err)
		}
		table.PrimaryKeys = pks

		// Get foreign keys
		fks, err := schemaRepo.GetForeignKeys(ctx, schema, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get foreign keys for %s: %w", tableName, err)
		}
		table.ForeignKeys = fks

		tables = append(tables, table)
	}

	return tables, nil
}

func buildRelationshipsWithDetection(ctx context.Context, schemaRepo *repository.SchemaRepository, schema string, tables []model.Table) ([]model.Relationship, error) {
	var relationships []model.Relationship
	junctionTables := detectJunctionTables(tables)

	// Collect all table-column pairs that need unique constraint checking
	var tableColumns []repository.TableColumn

	// First pass: collect all foreign keys that need checking (excluding junction tables)
	for _, table := range tables {
		if !junctionTables[table.Name] {
			for _, fk := range table.ForeignKeys {
				tableColumns = append(tableColumns, repository.TableColumn{
					Table:  table.Name,
					Column: fk.FromColumn,
				})
			}
		}
	}

	// Batch query all unique constraints
	uniqueMap, err := schemaRepo.GetUniqueConstraintsBatch(ctx, schema, tableColumns)
	if err != nil {
		return nil, fmt.Errorf("failed to get unique constraints: %w", err)
	}

	// Second pass: build relationships
	for _, table := range tables {
		// Skip junction tables - they'll be handled as many-to-many
		if junctionTables[table.Name] {
			// Create many-to-many relationships
			if len(table.ForeignKeys) >= minJunctionTableFKs {
				// Handle multiple foreign keys in junction table
				for i := 0; i < len(table.ForeignKeys); i++ {
					for j := i + 1; j < len(table.ForeignKeys); j++ {
						rel := model.Relationship{
							FromTable: table.ForeignKeys[i].ToTable,
							ToTable:   table.ForeignKeys[j].ToTable,
							Type:      "}o--o{",
						}
						relationships = append(relationships, rel)
					}
				}
			}
			continue
		}

		for _, fk := range table.ForeignKeys {
			// Check if this column has a unique constraint
			key := fmt.Sprintf("%s:%s", table.Name, fk.FromColumn)
			isUnique := uniqueMap[key]

			relType := "||--o{" // Default: one-to-many
			if isUnique {
				relType = "||--||" // One-to-one
			}

			rel := model.Relationship{
				FromTable: table.Name,
				ToTable:   fk.ToTable,
				Type:      relType,
			}
			relationships = append(relationships, rel)
		}
	}

	return relationships, nil
}

func detectJunctionTables(tables []model.Table) map[string]bool {
	junctionTables := make(map[string]bool)
	for _, table := range tables {
		// More flexible detection: at least 2 FKs, and all FKs are part of PK
		if len(table.ForeignKeys) >= minJunctionTableFKs &&
			len(table.PrimaryKeys) >= minJunctionTableFKs &&
			len(table.Columns) <= maxJunctionTableColumns {

			// Check if all foreign keys are in the primary key
			allFKsInPK := true
			for _, fk := range table.ForeignKeys {
				if !utils.Contains(table.PrimaryKeys, fk.FromColumn) {
					allFKsInPK = false
					break
				}
			}
			fkCountInPK := 0
			for _, pk := range table.PrimaryKeys {
				for _, fk := range table.ForeignKeys {
					if pk == fk.FromColumn {
						fkCountInPK++
						break
					}
				}
			}
			if allFKsInPK && fkCountInPK >= minJunctionTableFKs {
				junctionTables[table.Name] = true
			}
		}
	}
	return junctionTables
}

func generateMermaid(tables []model.Table, relationships []model.Relationship) string {
	var sb strings.Builder

	sb.WriteString("erDiagram\n")

	if len(relationships) > 0 {
		// Use a map to deduplicate relationships
		seen := make(map[string]bool)
		for _, rel := range relationships {
			// Create a unique key for the relationship
			key := fmt.Sprintf("%s:%s:%s", rel.FromTable, rel.Type, rel.ToTable)
			if seen[key] {
				continue // Skip duplicate relationships
			}
			seen[key] = true

			// Mermaid ER diagram syntax requires a label (even if empty)
			// Use empty string as label to effectively hide it
			sb.WriteString(fmt.Sprintf("    %s %s %s : \"\"\n",
				strings.ToUpper(rel.FromTable),
				rel.Type,
				strings.ToUpper(rel.ToTable)))
		}
		sb.WriteString("\n")
	}

	// Write table definitions
	for _, table := range tables {
		sb.WriteString(fmt.Sprintf("    %s {\n", strings.ToUpper(table.Name)))

		for _, col := range table.Columns {
			dataType := simplifyDataType(col.DataType)
			annotations := ""

			// Add PK annotation
			if utils.Contains(table.PrimaryKeys, col.Name) {
				annotations = " PK"
			}

			// Add FK annotation
			if isForeignKey(table.ForeignKeys, col.Name) {
				annotations += " FK"
			}

			sb.WriteString(fmt.Sprintf("        %s %s%s\n",
				dataType,
				col.Name,
				annotations))
		}

		sb.WriteString("    }\n\n")
	}

	return sb.String()
}

func simplifyDataType(dataType string) string {
	dt := strings.ToLower(dataType)

	switch {
	case dt == "integer":
		return "int"
	case dt == "bigint":
		return "bigint"
	case dt == "smallint":
		return "smallint"
	case strings.HasPrefix(dt, "character varying"):
		return "varchar"
	case strings.HasPrefix(dt, "character"):
		return "char"
	case dt == "text":
		return "text"
	case strings.HasPrefix(dt, "timestamp without time zone"):
		return "timestamp"
	case strings.HasPrefix(dt, "timestamp with time zone"):
		return "timestamptz"
	case strings.HasPrefix(dt, "time without time zone"):
		return "time"
	case dt == "date":
		return "date"
	case dt == "boolean":
		return "boolean"
	case strings.HasPrefix(dt, "numeric"):
		return "numeric"
	case strings.HasPrefix(dt, "decimal"):
		return "decimal"
	case dt == "real":
		return "real"
	case dt == "double precision":
		return "double"
	case dt == "json":
		return "json"
	case dt == "jsonb":
		return "jsonb"
	case dt == "uuid":
		return "uuid"
	case dt == "bytea":
		return "bytea"
	case strings.HasPrefix(dt, "array"):
		return "array"
	default:
		return dataType
	}
}

func isForeignKey(fks []model.ForeignKey, colName string) bool {
	for _, fk := range fks {
		if fk.FromColumn == colName {
			return true
		}
	}
	return false
}

// GenerateSchemaVisualization builds a Mermaid ER diagram from a schema repository and schema name.
func GenerateSchemaVisualization(ctx context.Context, schemaRepo *repository.SchemaRepository, schema string) (string, error) {
	// Parse tables
	tables, err := parseTables(ctx, schemaRepo, schema)
	if err != nil {
		return "", fmt.Errorf("failed to parse tables: %w", err)
	}

	// Build relationships
	relationships, err := buildRelationshipsWithDetection(ctx, schemaRepo, schema, tables)
	if err != nil {
		return "", fmt.Errorf("failed to build relationships: %w", err)
	}

	mermaidDiagram := generateMermaid(tables, relationships)
	return mermaidDiagram, nil
}
