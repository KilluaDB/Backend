package metrics

import (
	"errors"
	"strings"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceCollector_Describe(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	collector := NewInstanceCollector(mockPool)

	ch := make(chan *prometheus.Desc, 1)
	collector.Describe(ch)

	desc := <-ch
	assert.Contains(t, desc.String(), "killuadb_active_instances")
}

func TestInstanceCollector_Collect_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	collector := NewInstanceCollector(mockPool)

	rows := pgxmock.NewRows([]string{"db_type", "count"}).
		AddRow("postgresql", int64(3)).
		AddRow("mongodb", int64(2))

	mockPool.ExpectQuery("SELECT db_type, COUNT(.*) FROM projects WHERE status = 'running' GROUP BY db_type").
		WillReturnRows(rows)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	expected := `
		# HELP killuadb_active_instances Number of active database instances by type.
		# TYPE killuadb_active_instances gauge
		killuadb_active_instances{type="mongodb"} 2
		killuadb_active_instances{type="postgresql"} 3
	`

	err = testutil.GatherAndCompare(registry, strings.NewReader(expected), "killuadb_active_instances")
	assert.NoError(t, err)
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

func TestInstanceCollector_Collect_Error(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	collector := NewInstanceCollector(mockPool)

	mockPool.ExpectQuery("SELECT db_type, COUNT(.*)").
		WillReturnError(errors.New("db connection error"))

	// Since prometheus framework will panic on Gather if an invalid metric is sent (because Collect returns it),
	// we test Collect directly instead of via GatherAndCompare.
	ch := make(chan prometheus.Metric, 1)

	// Collect won't panic, it sends InvalidMetric to the channel
	collector.Collect(ch)

	m := <-ch
	assert.Contains(t, m.Desc().String(), "killuadb_active_instances")
	assert.NoError(t, mockPool.ExpectationsWereMet())
}
