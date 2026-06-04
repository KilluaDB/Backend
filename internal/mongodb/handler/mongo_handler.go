package handler

// MongoHandler groups HTTP handlers for MongoDB project APIs.
type MongoHandler struct {
	Collection *CollectionHandler
	Document   *DocumentHandler
}

func NewMongoHandler(collection *CollectionHandler, document *DocumentHandler) *MongoHandler {
	return &MongoHandler{
		Collection: collection,
		Document: 	document,
	}
}
