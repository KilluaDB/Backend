package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func newTestProvisioner(t *testing.T, objs ...runtime.Object) *OperatorProvisioner {
	t.Helper()
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClient(scheme, objs...)
	core := k8sfake.NewSimpleClientset()
	return &OperatorProvisioner{
		postgresNamespace:  "postgres-instances",
		mongoNamespace:     "mongodb-instances",
		dynamic:            dyn,
		core:               core,
		cnpgGVR:            schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"},
		mongoGVR:           schema.GroupVersionResource{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"},
		ingressRouteTCPGVR: schema.GroupVersionResource{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "ingressroutetcps"},
		externalDomain:     "db.example.com",
		postgresExtPort:    5432,
		mongoExtPort:       27017,
	}
}

func TestOperatorProvisioner_ClusterNameAndExternal(t *testing.T) {
	p := newTestProvisioner(t)
	id := uuid.New()
	name := p.ClusterNameForProject(id)
	assert.True(t, strings.HasPrefix(name, "db-"))
	assert.True(t, p.HasExternalAccess())
	assert.Equal(t, 5432, p.ExternalPort("postgresql"))
	assert.Equal(t, 27017, p.ExternalPort("mongodb"))
	host := p.ExternalHostname(id, "postgresql")
	assert.Contains(t, host, "postgres")
	assert.Contains(t, host, "db.example.com")
}

func TestOperatorProvisioner_TierResources(t *testing.T) {
	p := newTestProvisioner(t)
	tests := []struct {
		tier    string
		wantErr bool
	}{
		{"free", false},
		{"basic", false},
		{"premium", false},
		{"unknown", true},
	}
	for _, tt := range tests {
		_, _, _, err := p.TierResources(tt.tier)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestOperatorProvisioner_GetPostgresConnection(t *testing.T) {
	p := newTestProvisioner(t)
	name := "db-testcluster"
	ns := p.postgresNamespace
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-app", Namespace: ns},
		Data: map[string][]byte{
			"username": []byte("app"),
			"password": []byte("secret"),
			"dbname":   []byte("app"),
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	result, err := p.getPostgresConnection(context.Background(), ns, name)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.DSN, "postgresql://"))
}

func TestOperatorProvisioner_CreateInstanceUnsupported(t *testing.T) {
	p := newTestProvisioner(t)
	_, err := p.CreateInstance(context.Background(), uuid.New(), "mysql", "free", "pass")
	assert.Error(t, err)
}

func TestOperatorProvisioner_DeleteInstance(t *testing.T) {
	p := newTestProvisioner(t)
	err := p.DeleteInstance(context.Background(), uuid.New(), "postgresql")
	assert.NoError(t, err)
}
