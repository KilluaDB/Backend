package handler

// MongoHandler groups HTTP handlers for MongoDB project APIs.
type MongoHandler struct {
	Collection *CollectionHandler
}

func NewMongoHandler(collection *CollectionHandler) *MongoHandler {
	return &MongoHandler{
		Collection: collection,
	}
}
