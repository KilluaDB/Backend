package model

type MongoDashboardMetrics struct {
	Database        string  `json:"database"`
	DBSizeBytes     int64   `json:"db_size_bytes"`
	Collections     int64   `json:"collections"`
	TotalDocuments  int64   `json:"total_documents"`
	ActiveConns     int64   `json:"active_connections"`
	AvailableConns  int64   `json:"available_connections"`
	ActiveOps       int64   `json:"active_operations"`
	TotalInserts    int64   `json:"total_inserts"`
	TotalUpdates    int64   `json:"total_updates"`
	TotalDeletes    int64   `json:"total_deletes"`
}