package servicetest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestProvisioner(t *testing.T) {
	p := &Provisioner{External: true}
	ctx := context.Background()
	projectID := uuid.New()

	_, err := p.CreateInstance(ctx, projectID, "postgres", "free", "pass")
	assert.NoError(t, err)

	_, err = p.GetConnection(ctx, projectID, "postgres")
	assert.NoError(t, err)

	err = p.DeleteInstance(ctx, projectID, "postgres")
	assert.NoError(t, err)

	name := p.ClusterNameForProject(projectID)
	assert.NotEmpty(t, name)

	ext := p.HasExternalAccess()
	assert.True(t, ext)

	host := p.ExternalHostname(projectID, "postgres")
	assert.NotEmpty(t, host)

	port := p.ExternalPort("postgres")
	assert.Equal(t, 5432, port)
	assert.Equal(t, 27017, p.ExternalPort("mongodb"))

	cpu, mem, conns, err := p.TierResources("free")
	assert.NoError(t, err)
	assert.Greater(t, cpu, 0.0)
	assert.Greater(t, mem, 0.0)
	assert.Greater(t, conns, 0)
	_, _, _, err = p.TierResources("unknown")
	assert.Error(t, err)

	url := p.PostgRESTURL(projectID)
	assert.NotEmpty(t, url)

	user, pass, err := p.GetPostgRESTCredentials(ctx, projectID)
	assert.NoError(t, err)
	assert.NotEmpty(t, user)
	assert.NotEmpty(t, pass)
}
