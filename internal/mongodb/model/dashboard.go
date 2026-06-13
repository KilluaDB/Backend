package model

type OperationStats struct {
	Inserts int64 `json:"inserts"`
	Updates int64 `json:"updates"`
	Deletes int64 `json:"deletes"`
}

type MongoDashboardMetrics struct {
	Database       string         `json:"database"`
	DBSizeBytes    int64          `json:"db_size_bytes"`
	Collections    int64          `json:"collections"`
	TotalDocuments int64          `json:"total_documents"`
	Last30Days     OperationStats `json:"last_30_days"`
}
