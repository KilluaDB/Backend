package service

import (
	"backend/internal/postgres/infra"
	"backend/internal/postgres/model"
	"backend/internal/postgres/repository"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSchemaInstanceConn struct {
	pool       *pgxpool.Pool
	err        error
	instanceID uuid.UUID
}

var _ infra.InstanceConnectionService = mockSchemaInstanceConn{}

func (m mockSchemaInstanceConn) GetPool(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pool, nil
}

func (m mockSchemaInstanceConn) GetPoolWithMeta(ctx context.Context, userID, projectID uuid.UUID) (*pgxpool.Pool, uuid.UUID, error) {
	if m.err != nil {
		return nil, uuid.Nil, m.err
	}
	instanceID := m.instanceID
	if instanceID == uuid.Nil {
		instanceID = uuid.New()
	}
	return m.pool, instanceID, nil
}

func (m mockSchemaInstanceConn) GetInstanceID(ctx context.Context, userID, projectID uuid.UUID) (uuid.UUID, error) {
	if m.err != nil {
		return uuid.Nil, m.err
	}
	if m.instanceID == uuid.Nil {
		return uuid.New(), nil
	}
	return m.instanceID, nil
}

type mockSchemaDDLRunnerSource struct {
	runner schemaDDLRunner
	err    error
}

func (m mockSchemaDDLRunnerSource) DDLRunner(ctx context.Context, userID, projectID uuid.UUID) (schemaDDLRunner, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.runner, nil
}

func newSchemaServiceMockPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func newSchemaServiceRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client
}

func pendingDDLKey(projectID uuid.UUID) string {
	return fmt.Sprintf("pending_schema:%s", projectID.String())
}

func tableFixture(name string, columns []model.Column, primaryKeys []string, foreignKeys []model.ForeignKey) model.Table {
	return model.Table{
		Name:        name,
		Columns:     columns,
		PrimaryKeys: primaryKeys,
		ForeignKeys: foreignKeys,
	}
}

func col(name, dataType string) model.Column {
	return model.Column{Name: name, DataType: dataType}
}

func fk(fromColumn, toTable, toColumn string) model.ForeignKey {
	return model.ForeignKey{
		ConstraintName: fmt.Sprintf("fk_%s_%s", toTable, fromColumn),
		FromColumn:     fromColumn,
		ToSchema:       "public",
		ToTable:        toTable,
		ToColumn:       toColumn,
		UpdateRule:     "NO ACTION",
		DeleteRule:     "NO ACTION",
	}
}

func expectSchemaIntrospection(mock pgxmock.PgxPoolIface, schema string, tables []model.Table) {
	mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
		WithArgs(schema).
		WillReturnRows(tableNameRows(tables))

	for _, table := range tables {
		mock.ExpectQuery(`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`).
			WithArgs(schema, table.Name).
			WillReturnRows(columnRows(table.Columns))

		mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
			WithArgs(schema, table.Name).
			WillReturnRows(primaryKeyRows(table.PrimaryKeys))

		mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FOREIGN KEY`).
			WithArgs(schema, table.Name).
			WillReturnRows(foreignKeyRows(table.ForeignKeys))
	}
}

func expectUniqueConstraintQuery(mock pgxmock.PgxPoolIface, args []interface{}, uniquePairs ...[2]string) {
	rows := pgxmock.NewRows([]string{"table_name", "column_name"})
	for _, pair := range uniquePairs {
		rows.AddRow(pair[0], pair[1])
	}
	mock.ExpectQuery(`(?s)SELECT DISTINCT tc\.table_name.*UNIQUE`).
		WithArgs(args...).
		WillReturnRows(rows)
}

func tableNameRows(tables []model.Table) *pgxmock.Rows {
	rows := pgxmock.NewRows([]string{"table_name"})
	for _, table := range tables {
		rows.AddRow(table.Name)
	}
	return rows
}

func columnRows(columns []model.Column) *pgxmock.Rows {
	rows := pgxmock.NewRows([]string{"column_name", "data_type", "is_nullable"})
	for _, column := range columns {
		nullable := "NO"
		if column.Nullable {
			nullable = "YES"
		}
		rows.AddRow(column.Name, column.DataType, nullable)
	}
	return rows
}

func primaryKeyRows(primaryKeys []string) *pgxmock.Rows {
	rows := pgxmock.NewRows([]string{"column_name"})
	for _, primaryKey := range primaryKeys {
		rows.AddRow(primaryKey)
	}
	return rows
}

func foreignKeyRows(foreignKeys []model.ForeignKey) *pgxmock.Rows {
	rows := pgxmock.NewRows([]string{
		"constraint_name", "column_name", "foreign_table_schema", "foreign_table_name",
		"foreign_column_name", "update_rule", "delete_rule",
	})
	for _, foreignKey := range foreignKeys {
		rows.AddRow(
			foreignKey.ConstraintName,
			foreignKey.FromColumn,
			foreignKey.ToSchema,
			foreignKey.ToTable,
			foreignKey.ToColumn,
			foreignKey.UpdateRule,
			foreignKey.DeleteRule,
		)
	}
	return rows
}

func TestIsValidSchemaName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "public", want: true},
		{name: "my_schema", want: true},
		{name: "Schema1", want: true},
		{name: "a", want: true},
		{name: strings.Repeat("a", 63), want: true},
		{name: "", want: false},
		{name: strings.Repeat("a", 64), want: false},
		{name: "bad-name", want: false},
		{name: "has space", want: false},
		{name: "9starts_with_digit", want: false},
		{name: "has\x00null", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidSchemaName(tt.name))
		})
	}
}

func TestDetectJunctionTables(t *testing.T) {
	standardJunction := tableFixture(
		"user_roles",
		[]model.Column{col("user_id", "uuid"), col("role_id", "uuid"), col("created_at", "timestamp")},
		[]string{"user_id", "role_id"},
		[]model.ForeignKey{fk("user_id", "users", "id"), fk("role_id", "roles", "id")},
	)

	tests := []struct {
		name       string
		tables     []model.Table
		wantTables []string
	}{
		{
			name:       "standard junction with two FK columns in PK and up to six columns",
			tables:     []model.Table{standardJunction},
			wantTables: []string{"user_roles"},
		},
		{
			name: "not junction with only one FK",
			tables: []model.Table{tableFixture(
				"orders",
				[]model.Column{col("id", "uuid"), col("user_id", "uuid")},
				[]string{"id"},
				[]model.ForeignKey{fk("user_id", "users", "id")},
			)},
		},
		{
			name: "not junction when FKs are not in PK",
			tables: []model.Table{tableFixture(
				"user_roles",
				[]model.Column{col("id", "uuid"), col("user_id", "uuid"), col("role_id", "uuid")},
				[]string{"id"},
				[]model.ForeignKey{fk("user_id", "users", "id"), fk("role_id", "roles", "id")},
			)},
		},
		{
			name: "not junction with more than six columns",
			tables: []model.Table{tableFixture(
				"user_roles",
				[]model.Column{
					col("user_id", "uuid"), col("role_id", "uuid"), col("created_at", "timestamp"),
					col("updated_at", "timestamp"), col("deleted_at", "timestamp"), col("metadata", "jsonb"), col("notes", "text"),
				},
				[]string{"user_id", "role_id"},
				[]model.ForeignKey{fk("user_id", "users", "id"), fk("role_id", "roles", "id")},
			)},
		},
		{
			name: "multiple junction tables",
			tables: []model.Table{
				standardJunction,
				tableFixture(
					"post_tags",
					[]model.Column{col("post_id", "uuid"), col("tag_id", "uuid")},
					[]string{"post_id", "tag_id"},
					[]model.ForeignKey{fk("post_id", "posts", "id"), fk("tag_id", "tags", "id")},
				),
			},
			wantTables: []string{"user_roles", "post_tags"},
		},
		{
			name:   "empty table list",
			tables: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectJunctionTables(tt.tables)
			for _, wantTable := range tt.wantTables {
				assert.True(t, got[wantTable], "expected %s to be detected as a junction table", wantTable)
			}
			assert.Len(t, got, len(tt.wantTables))
		})
	}
}

func TestBuildRelationshipsWithDetectionViaGenerateSchemaVisualization(t *testing.T) {
	tests := []struct {
		name            string
		tables          []model.Table
		uniqueArgs      []interface{}
		uniquePairs     [][2]string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "one-to-many when FK column is not unique",
			tables: []model.Table{
				tableFixture("users", []model.Column{col("id", "uuid")}, []string{"id"}, nil),
				tableFixture(
					"orders",
					[]model.Column{col("id", "uuid"), col("user_id", "uuid")},
					[]string{"id"},
					[]model.ForeignKey{fk("user_id", "users", "id")},
				),
			},
			uniqueArgs:   []interface{}{"public", "orders", "user_id", "public"},
			wantContains: []string{`ORDERS ||--o{ USERS : ""`},
		},
		{
			name: "one-to-one when FK column is unique",
			tables: []model.Table{
				tableFixture("users", []model.Column{col("id", "uuid")}, []string{"id"}, nil),
				tableFixture(
					"profiles",
					[]model.Column{col("id", "uuid"), col("user_id", "uuid")},
					[]string{"id"},
					[]model.ForeignKey{fk("user_id", "users", "id")},
				),
			},
			uniqueArgs:   []interface{}{"public", "profiles", "user_id", "public"},
			uniquePairs:  [][2]string{{"profiles", "user_id"}},
			wantContains: []string{`PROFILES ||--|| USERS : ""`},
		},
		{
			name: "many-to-many through detected junction table",
			tables: []model.Table{
				tableFixture("users", []model.Column{col("id", "uuid")}, []string{"id"}, nil),
				tableFixture("roles", []model.Column{col("id", "uuid")}, []string{"id"}, nil),
				tableFixture(
					"user_roles",
					[]model.Column{col("user_id", "uuid"), col("role_id", "uuid")},
					[]string{"user_id", "role_id"},
					[]model.ForeignKey{fk("user_id", "users", "id"), fk("role_id", "roles", "id")},
				),
			},
			wantContains:    []string{`USERS }o--o{ ROLES : ""`},
			wantNotContains: []string{`USER_ROLES ||--o{ USERS`, `USER_ROLES ||--o{ ROLES`},
		},
		{
			name:         "no tables yields empty diagram",
			tables:       nil,
			wantContains: []string{"erDiagram\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newSchemaServiceMockPool(t)
			expectSchemaIntrospection(mock, "public", tt.tables)
			if len(tt.uniqueArgs) > 0 {
				expectUniqueConstraintQuery(mock, tt.uniqueArgs, tt.uniquePairs...)
			}

			repo := repository.NewSchemaRepository(mock)
			got, err := GenerateSchemaVisualization(context.Background(), repo, "public")
			require.NoError(t, err)
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, got, notWant)
			}
			if len(tt.tables) == 0 {
				assert.Equal(t, "erDiagram\n", got)
			}
		})
	}
}

func TestGenerateMermaid(t *testing.T) {
	t.Run("prefix uppercase tables annotations and deduplicated relationships", func(t *testing.T) {
		tables := []model.Table{
			tableFixture(
				"orders",
				[]model.Column{col("id", "integer"), col("user_id", "uuid"), col("status", "text")},
				[]string{"id"},
				[]model.ForeignKey{fk("user_id", "users", "id")},
			),
			tableFixture("users", []model.Column{col("id", "uuid")}, []string{"id"}, nil),
		}
		relationships := []model.Relationship{
			{FromTable: "orders", ToTable: "users", Type: "||--o{"},
			{FromTable: "orders", ToTable: "users", Type: "||--o{"},
		}

		got := generateMermaid(tables, relationships)

		assert.True(t, strings.HasPrefix(got, "erDiagram\n"))
		assert.Contains(t, got, "    ORDERS {\n")
		assert.Contains(t, got, "    USERS {\n")
		assert.Contains(t, got, "        int id PK\n")
		assert.Contains(t, got, "        uuid user_id FK\n")
		assert.Equal(t, 1, strings.Count(got, `ORDERS ||--o{ USERS : ""`))
	})

	t.Run("empty tables and relationships", func(t *testing.T) {
		assert.Equal(t, "erDiagram\n", generateMermaid(nil, nil))
	})
}

func TestGenerateSchemaVisualization(t *testing.T) {
	t.Run("success with two related tables", func(t *testing.T) {
		mock := newSchemaServiceMockPool(t)
		tables := []model.Table{
			tableFixture("users", []model.Column{col("id", "uuid")}, []string{"id"}, nil),
			tableFixture(
				"orders",
				[]model.Column{col("id", "uuid"), col("user_id", "uuid")},
				[]string{"id"},
				[]model.ForeignKey{fk("user_id", "users", "id")},
			),
		}
		expectSchemaIntrospection(mock, "public", tables)
		expectUniqueConstraintQuery(mock, []interface{}{"public", "orders", "user_id", "public"})

		repo := repository.NewSchemaRepository(mock)
		got, err := GenerateSchemaVisualization(context.Background(), repo, "public")

		require.NoError(t, err)
		assert.Contains(t, got, "USERS")
		assert.Contains(t, got, "ORDERS")
		assert.Contains(t, got, `ORDERS ||--o{ USERS : ""`)
	})

	t.Run("GetTables error propagates", func(t *testing.T) {
		mock := newSchemaServiceMockPool(t)
		mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
			WithArgs("public").
			WillReturnError(errors.New("get tables failed"))

		repo := repository.NewSchemaRepository(mock)
		_, err := GenerateSchemaVisualization(context.Background(), repo, "public")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "get tables failed")
	})

	t.Run("GetForeignKeys error propagates", func(t *testing.T) {
		mock := newSchemaServiceMockPool(t)
		tables := []model.Table{
			tableFixture("orders", []model.Column{col("id", "uuid"), col("user_id", "uuid")}, []string{"id"}, nil),
		}
		mock.ExpectQuery(`(?s)SELECT table_name.*information_schema\.tables`).
			WithArgs("public").
			WillReturnRows(tableNameRows(tables))
		mock.ExpectQuery(`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`).
			WithArgs("public", "orders").
			WillReturnRows(columnRows(tables[0].Columns))
		mock.ExpectQuery(`(?s)SELECT kcu\.column_name.*PRIMARY KEY`).
			WithArgs("public", "orders").
			WillReturnRows(primaryKeyRows(tables[0].PrimaryKeys))
		mock.ExpectQuery(`(?s)SELECT.*constraint_name.*FOREIGN KEY`).
			WithArgs("public", "orders").
			WillReturnError(errors.New("get foreign keys failed"))

		repo := repository.NewSchemaRepository(mock)
		_, err := GenerateSchemaVisualization(context.Background(), repo, "public")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "get foreign keys failed")
	})
}

func TestSchemaService_CachePendingDDL(t *testing.T) {
	ctx := context.Background()

	t.Run("normal DDL string is stored under project key", func(t *testing.T) {
		_, client := newSchemaServiceRedis(t)
		projectID := uuid.New()
		ddl := "CREATE TABLE users (id uuid);"
		svc := NewSchemaService(mockSchemaInstanceConn{}, client)

		require.NoError(t, svc.CachePendingDDL(ctx, projectID, ddl))

		got, err := client.Get(ctx, pendingDDLKey(projectID)).Result()
		require.NoError(t, err)
		assert.Equal(t, ddl, got)
	})

	t.Run("empty or whitespace DDL is a no-op", func(t *testing.T) {
		mr, client := newSchemaServiceRedis(t)
		projectID := uuid.New()
		svc := NewSchemaService(mockSchemaInstanceConn{}, client)

		require.NoError(t, svc.CachePendingDDL(ctx, projectID, " \n\t "))

		assert.False(t, mr.Exists(pendingDDLKey(projectID)))
	})

	t.Run("redis unavailable returns error", func(t *testing.T) {
		mr, client := newSchemaServiceRedis(t)
		mr.Close()
		svc := NewSchemaService(mockSchemaInstanceConn{}, client)

		err := svc.CachePendingDDL(ctx, uuid.New(), "CREATE TABLE users (id uuid);")

		require.Error(t, err)
	})
}

func TestSchemaService_ApplyDDL(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	t.Run("key exists and exec succeeds deletes key", func(t *testing.T) {
		mr, client := newSchemaServiceRedis(t)
		projectID := uuid.New()
		key := pendingDDLKey(projectID)
		ddl := "CREATE TABLE users (id uuid);"
		mr.Set(key, ddl)

		mockPool := newSchemaServiceMockPool(t)
		mockPool.ExpectExec(regexp.QuoteMeta(ddl)).
			WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))

		svc := NewSchemaService(mockSchemaInstanceConn{}, client)
		svc.ddlSource = mockSchemaDDLRunnerSource{runner: mockPool}

		require.NoError(t, svc.ApplyDDL(ctx, userID, projectID))
		assert.False(t, mr.Exists(key))
	})

	t.Run("key missing returns ErrNoPendingDDL", func(t *testing.T) {
		_, client := newSchemaServiceRedis(t)
		projectID := uuid.New()
		mockPool := newSchemaServiceMockPool(t)
		svc := NewSchemaService(mockSchemaInstanceConn{}, client)
		svc.ddlSource = mockSchemaDDLRunnerSource{runner: mockPool}

		err := svc.ApplyDDL(ctx, userID, projectID)

		assert.ErrorIs(t, err, ErrNoPendingDDL)
	})

	t.Run("redis generic error propagates", func(t *testing.T) {
		mr, client := newSchemaServiceRedis(t)
		mr.Close()
		mockPool := newSchemaServiceMockPool(t)
		svc := NewSchemaService(mockSchemaInstanceConn{}, client)
		svc.ddlSource = mockSchemaDDLRunnerSource{runner: mockPool}

		err := svc.ApplyDDL(ctx, userID, uuid.New())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to retrieve pending DDL")
	})

	t.Run("exec fails returns wrapped error and keeps key", func(t *testing.T) {
		mr, client := newSchemaServiceRedis(t)
		projectID := uuid.New()
		key := pendingDDLKey(projectID)
		ddl := "CREATE TABLE users (id uuid);"
		mr.Set(key, ddl)

		mockPool := newSchemaServiceMockPool(t)
		mockPool.ExpectExec(regexp.QuoteMeta(ddl)).
			WillReturnError(errors.New("exec failed"))

		svc := NewSchemaService(mockSchemaInstanceConn{}, client)
		svc.ddlSource = mockSchemaDDLRunnerSource{runner: mockPool}

		err := svc.ApplyDDL(ctx, userID, projectID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to apply DDL")
		assert.Contains(t, err.Error(), "exec failed")
		assert.True(t, mr.Exists(key))
	})

	t.Run("GetPool fails before Redis interaction", func(t *testing.T) {
		mr, client := newSchemaServiceRedis(t)
		mr.Close()
		projectID := uuid.New()
		svc := NewSchemaService(mockSchemaInstanceConn{err: errors.New("pool failed")}, client)

		err := svc.ApplyDDL(ctx, userID, projectID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get connection pool")
		assert.Contains(t, err.Error(), "pool failed")
		assert.NotContains(t, err.Error(), "failed to retrieve pending DDL")
	})
}

func TestSchemaServiceTestRegexesCompile(t *testing.T) {
	patterns := []string{
		`(?s)SELECT table_name.*information_schema\.tables`,
		`(?s)SELECT column_name, data_type, is_nullable.*information_schema\.columns`,
		`(?s)SELECT kcu\.column_name.*PRIMARY KEY`,
		`(?s)SELECT.*constraint_name.*FOREIGN KEY`,
		`(?s)SELECT DISTINCT tc\.table_name.*UNIQUE`,
	}
	for _, pattern := range patterns {
		_, err := regexp.Compile(pattern)
		require.NoError(t, err)
	}
}
