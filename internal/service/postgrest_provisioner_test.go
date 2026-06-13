package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPostgREST_NamesAndURL(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	
	ns := p.NamespaceForProject(projectID)
	assert.True(t, strings.HasPrefix(ns, "pg-"))
	
	dep := postgrestDeploymentName(projectID)
	assert.True(t, strings.HasPrefix(dep, "postgrest-"))

	sec := postgrestSecretName(projectID)
	assert.True(t, strings.HasPrefix(sec, "postgrest-") && strings.HasSuffix(sec, "-cfg"))

	svc := postgrestServiceName(projectID)
	assert.True(t, strings.HasPrefix(svc, "postgrest-") && strings.HasSuffix(svc, "-svc"))

	url := p.PostgRESTURL(projectID)
	assert.Contains(t, url, p.externalDomain)

	p.externalDomain = ""
	assert.Equal(t, "", p.PostgRESTURL(projectID))
}

func TestPostgREST_SetupPostgRESTRoles_Skip(t *testing.T) {
	p := newTestProvisioner(t)
	p.skipPostgRESTSetup = true
	pw, err := p.SetupPostgRESTRoles(context.Background(), "ns", "cluster")
	assert.NoError(t, err)
	assert.Equal(t, "test-auth-pw", pw)
}

func TestPostgREST_SetupPostgRESTRoles_Timeout(t *testing.T) {
	p := newTestProvisioner(t)
	p.skipPostgRESTSetup = false
	// without secret, it should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := p.SetupPostgRESTRoles(ctx, "ns", "cluster")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "waiting for superuser secret")
}

func TestPostgREST_SetupPostgRESTRoles_SecretNoPassword(t *testing.T) {
	p := newTestProvisioner(t)
	p.skipPostgRESTSetup = false
	ns := "ns"
	cluster := "cluster"
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: cluster + "-superuser", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("postgres")}, // no password
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = p.SetupPostgRESTRoles(context.Background(), ns, cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no password")
}

func TestPostgREST_CreatePostgRESTResources(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	ns := "ns"
	p.externalDomain = "example.com"

	jwtSecret, apiKey, err := p.CreatePostgRESTResources(context.Background(), projectID, ns, "pg-host", "auth-pw")
	require.NoError(t, err)
	assert.NotEmpty(t, jwtSecret)
	assert.NotEmpty(t, apiKey)

	// Check Secret
	sec, err := p.core.CoreV1().Secrets(ns).Get(context.Background(), postgrestSecretName(projectID), metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, string(sec.Data["jwt-secret"]), jwtSecret)
	assert.Equal(t, string(sec.Data["api-key"]), apiKey)
	assert.Contains(t, string(sec.Data["db-uri"]), "pg-host")

	// Check Deployment
	dep, err := p.core.AppsV1().Deployments(ns).Get(context.Background(), postgrestDeploymentName(projectID), metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(1), *dep.Spec.Replicas)

	// Check Service
	svc, err := p.core.CoreV1().Services(ns).Get(context.Background(), postgrestServiceName(projectID), metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(3000), svc.Spec.Ports[0].Port)
}

func TestPostgREST_GetPostgRESTCredentials(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	ns := p.NamespaceForProject(projectID)

	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: postgrestSecretName(projectID), Namespace: ns},
		Data: map[string][]byte{
			"jwt-secret": []byte("my-jwt-secret"),
			"api-key":    []byte("my-api-key"),
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	jwtSecret, apiKey, err := p.GetPostgRESTCredentials(context.Background(), projectID)
	require.NoError(t, err)
	assert.Equal(t, "my-jwt-secret", jwtSecret)
	assert.Equal(t, "my-api-key", apiKey)
}

func TestPostgREST_SignPostgRESTAPIKey(t *testing.T) {
	secret := "secret-123"
	token, err := signPostgRESTAPIKey(secret)
	require.NoError(t, err)

	parsedToken, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err)
	assert.True(t, parsedToken.Valid)
	
	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	require.True(t, ok)
	assert.Equal(t, "app_user", claims["role"])
}

func TestPostgREST_createPostgRESTIngressRoute(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	ns := "ns"
	svcName := "my-svc"
	p.externalDomain = "example.com"

	err := p.createPostgRESTIngressRoute(context.Background(), projectID, ns, svcName)
	require.NoError(t, err)

	id := strings.ReplaceAll(projectID.String(), "-", "")
	routeName := "postgrest-" + id + "-http"

	// Fetch route from dynamic client
	obj, err := p.dynamic.Resource(ingressRouteGVR).Namespace(ns).Get(context.Background(), routeName, metav1.GetOptions{})
	require.NoError(t, err)

	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	routes, _, _ := unstructured.NestedSlice(spec, "routes")
	require.Len(t, routes, 1)

	routeMap, ok := routes[0].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, routeMap["match"], "rest-"+id+".example.com")
}

func TestPostgREST_mustParseQuantity(t *testing.T) {
	q := mustParseQuantity("100m")
	assert.Equal(t, "100m", q.String())

	assert.Panics(t, func() {
		mustParseQuantity("invalid-quantity")
	})
}
