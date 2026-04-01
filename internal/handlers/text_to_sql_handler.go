package handlers

import (
	"my_project/internal/services"
)

type TextToSQLHandler struct {
	textToSQLService *services.TextToSQLService
	queryService     *services.QueryService
}

func NewTextToSQLHandler(
	textToSQLService *services.TextToSQLService,
	queryService *services.QueryService,
) *TextToSQLHandler {
	return &TextToSQLHandler{
		textToSQLService: textToSQLService,
		queryService:     queryService,
	}
}
