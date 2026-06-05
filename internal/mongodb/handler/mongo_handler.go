package handler

// MongoHandler groups HTTP handlers for MongoDB project APIs.
type MongoHandler struct {
	Collection 	*CollectionHandler
	Document   	*DocumentHandler
	Dashboard	*MongoDashboardHandler
}

func NewMongoHandler(collection *CollectionHandler, document *DocumentHandler, dashboard	*MongoDashboardHandler) *MongoHandler {
	return &MongoHandler{
		Collection: collection,
		Document: 	document,
		Dashboard: 	dashboard,
	}
}
