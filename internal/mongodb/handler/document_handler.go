package handler

import (
	"context"
	"errors"
	"net/http"

	"backend/internal/mongodb/model"
	mongoservice "backend/internal/mongodb/service"
	"backend/internal/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type documentServiceI interface {
	QueryDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.QueryDocumentsRequest) (*model.QueryDocumentsResult, error)
	GetDocumentByID(ctx context.Context, userID, projectID uuid.UUID, collection string, id string) (map[string]interface{}, error)
	GetDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.GetDocumentsRequest) (*model.GetDocumentsResult, error)
	InsertDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.InsertDocumentsRequest) (*model.InsertDocumentResult, error)
	UpdateDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.UpdateDocumentsRequest) (*model.UpdateDocumentsResult, error)
	DeleteDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.DeleteDocumentsRequest) (*model.DeleteDocumentsResult, error)
	CountDocuments(ctx context.Context, userID, projectID uuid.UUID, collection string, req model.CountDocumentsRequest) (*model.CountDocumentsResult, error)
	UpdateDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string, req model.UpdateFieldRequest) error
	AddDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id string, req model.AddDocumentFieldRequest) error
	DeleteDocumentField(ctx context.Context, userID, projectID uuid.UUID, collection, id, field string) error
	DeleteDocument(ctx context.Context, userID, projectID uuid.UUID, collection, id string) error
}

// DocumentHandler handles MongoDB document endpoints.
type DocumentHandler struct {
	documentService documentServiceI
}

func NewDocumentHandler(documentService documentServiceI) *DocumentHandler {
	return &DocumentHandler{documentService: documentService}
}

func (h *DocumentHandler) QueryDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.QueryDocumentsRequest
	// filter/sort are optional so we allow an empty body
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.documentService.QueryDocuments(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, result, "Documents retrieved successfully")
}

func (h *DocumentHandler) GetDocument(c *gin.Context) {
	collection := c.Param("collection")
	id := c.Param("docId")
	if collection == "" || id == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection and document ID are required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	doc, err := h.documentService.GetDocumentByID(c.Request.Context(), userUUID, projectUUID, collection, id)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, doc, "Document retrieved successfully")
}

func (h *DocumentHandler) GetDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection and document ID are required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.GetDocumentsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid query parameters")
		return
	}

	result, err := h.documentService.GetDocuments(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, result, "Document retrieved successfully")
}

func (h *DocumentHandler) InsertDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.InsertDocumentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.documentService.InsertDocuments(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, result, "Documents inserted successfully")
}

func (h *DocumentHandler) UpdateDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.UpdateDocumentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.documentService.UpdateDocuments(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, result, "Documents updated successfully")
}

func (h *DocumentHandler) DeleteDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.DeleteDocumentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.documentService.DeleteDocuments(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, result, "Documents deleted successfully")
}

func (h *DocumentHandler) CountDocuments(c *gin.Context) {
	collection := c.Param("collection")
	if collection == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Collection name is required")
		return
	}

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.CountDocumentsRequest
	_ = c.ShouldBindJSON(&req)

	result, err := h.documentService.CountDocuments(c.Request.Context(), userUUID, projectUUID, collection, req)
	if err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, result, "Documents counted successfully")
}

func (h *DocumentHandler) UpdateDocumentField(c *gin.Context) {
	collection := c.Param("collection")
	id := c.Param("docId")
	field := c.Param("field")

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.UpdateFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if err := h.documentService.UpdateDocumentField(c.Request.Context(), userUUID, projectUUID, collection, id, field, req); err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, nil, "Field updated successfully")
}

func (h *DocumentHandler) AddDocumentField(c *gin.Context) {
	collection := c.Param("collection")
	id := c.Param("docId")

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	var req model.AddDocumentFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if err := h.documentService.AddDocumentField(c.Request.Context(), userUUID, projectUUID, collection, id, req); err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, nil, "Field added successfully")
}

func (h *DocumentHandler) DeleteDocumentField(c *gin.Context) {
	collection := c.Param("collection")
	id := c.Param("docId")
	field := c.Param("field")

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	if err := h.documentService.DeleteDocumentField(c.Request.Context(), userUUID, projectUUID, collection, id, field); err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, nil, "Field deleted successfully")
}

func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
	collection := c.Param("collection")
	id := c.Param("docId")

	userUUID, projectUUID, ok := requireUserAndProject(c)
	if !ok {
		return
	}

	if err := h.documentService.DeleteDocument(c.Request.Context(), userUUID, projectUUID, collection, id); err != nil {
		handleDocumentError(c, err)
		return
	}

	response.Success(c, http.StatusOK, nil, "Document deleted successfully")
}

func handleDocumentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, mongoservice.ErrDocumentNotFound):
		response.Fail(c, http.StatusNotFound, err, "Document not found")
	case errors.Is(err, mongoservice.ErrInvalidDocumentID):
		response.Fail(c, http.StatusBadRequest, err, "Invalid document ID")
	case errors.Is(err, mongoservice.ErrInvalidFilter):
		response.Fail(c, http.StatusBadRequest, err, "Invalid filter")
	case errors.Is(err, mongoservice.ErrInvalidUpdate):
		response.Fail(c, http.StatusBadRequest, err, "Invalid update")
	case errors.Is(err, mongoservice.ErrInvalidCollectionName):
		response.Fail(c, http.StatusBadRequest, err, "Invalid collection name")
	case errors.Is(err, mongoservice.ErrTypeMismatch):
		response.Fail(c, http.StatusConflict, err, "Value type does not match existing field type")
	case errors.Is(err, mongoservice.ErrInvalidFieldType):
		response.Fail(c, http.StatusBadRequest, err, "Invalid field type")
	case errors.Is(err, mongoservice.ErrFieldAlreadyExists):
		response.Fail(c, http.StatusConflict, err, "Field already exists")
	case failMongoInstanceError(c, err):
		return
	default:
		response.Fail(c, http.StatusBadRequest, err, "Operation failed")
	}
}
