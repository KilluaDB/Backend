package services

import (
	"database/sql"
	"errors"
	"fmt"
	_ "log"
	"my_project/internal/repositories"
	"my_project/internal/utils"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type TableService struct {
	projectRepo *repositories.ProjectRepository
	instanceRepo *repositories.DatabaseInstanceRepository
	credentialsRepo *repositories.DatabaseCredentialRepository
	executeRepo *repositories.QueryHistoryRepository
	tableRepo *repositories.TableRepository
}

func NewTableService(
	projectRepo *repositories.ProjectRepository,
	instanceRepo *repositories.DatabaseInstanceRepository,
	credentialsRepo *repositories.DatabaseCredentialRepository,
	executeRepo *repositories.QueryHistoryRepository,
	tableRepo *repositories.TableRepository,
) *TableService {
	return &TableService {
		projectRepo: projectRepo,
		instanceRepo: instanceRepo,
		credentialsRepo: credentialsRepo,
		executeRepo: executeRepo,
		tableRepo: tableRepo,
	}
}

type Column struct {
	Name			string	`json:"name" binding:"required"`
	Type 			string	`json:"type" binding:"required"`
	Default		*string	`json:"default"`
	Primary		bool		`json:"primary"`
	IsUnique 	bool		`json:"is_unique"`
	IsIdentity	bool		`json:"is_identity"`
	Nullable		bool		`json:"nullable"`
}

type ForeignKeyRef struct {
	LocalColumn		string	`json:"local_column" binding:"required"`
	ForeignColumn	string	`json:"foreign_column" binding:"required"`
	OnUpdate			string	`json:"on_update" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION"`
	OnDelete			string	`json:"on_delete" binding:"omitempty, oneof=CASCADE RESTRICT NO ACTION SET NULL SET DEFAULT"`
}

type ForeignKey struct {
	Schema 		string				`json:"schema" binding:"required"`
	Table 		string				`json:"table" binding:"required"`
	References	[]ForeignKeyRef	`json:"references" binding:"required, min=1"`
}

type CreateTableRequest struct {
	Schema 		string			`json:"schema" binding:"required"`
	Table 		string			`json:"table" binding:"required"`
	Columns 		[]Column			`json:"columns" binding:"required"`
	ForeignKeys *ForeignKey		`json:"foreign_keys"`
}

type UpdateTableRequest struct {
	Schema 		string			`json:"schema"`
	Table 		string			`json:"table"`
	Columns 		[]Column			`json:"columns"`
	ForeignKeys *ForeignKey		`json:"foreign_keys"`
}

type DeleteTableRequest struct {
	Schema 		string			`json:"schema" binding:"required"`
	Table 		string			`json:"table" binding:"required"`
}