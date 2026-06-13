package metrics

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type InstanceCollector struct {
	db         Queryer
	activeDesc *prometheus.Desc
}

func NewInstanceCollector(db Queryer) *InstanceCollector {
	return &InstanceCollector{
		db: db,
		activeDesc: prometheus.NewDesc(
			"killuadb_active_instances",
			"Number of active database instances by type.",
			[]string{"type"}, nil,
		),
	}
}

func (c *InstanceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.activeDesc
}

func (c *InstanceCollector) Collect(ch chan<- prometheus.Metric) {
	rows, err := c.db.Query(context.Background(),
		`SELECT db_type, COUNT(*) FROM projects WHERE status = 'running' GROUP BY db_type`)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.activeDesc, err)
		return
	}

	defer rows.Close()
	counts := map[string]int64{
		"postgresql": 0,
		"mongodb":    0,
	}

	for rows.Next() {
		var dbType string
		var count int64
		if err := rows.Scan(&dbType, &count); err != nil {
			continue
		}
		counts[dbType] = count
	}

	for dbType, count := range counts {
		ch <- prometheus.MustNewConstMetric(c.activeDesc, prometheus.GaugeValue, float64(count), dbType)
	}
}
