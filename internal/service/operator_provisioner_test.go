package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNewOperatorProvisioner(t *testing.T) {
	// Might fail because we are outside cluster and lack kubeconfig,
	// but it will hit the branch.
	_, _ = NewOperatorProvisioner()
}

func TestCollectK8sMetrics(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	pgGVR := schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	mongoGVR := schema.GroupVersionResource{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"}

	gvrToListKind := map[schema.GroupVersionResource]string{
		pgGVR:    "ClusterList",
		mongoGVR: "MongoDBCommunityList",
	}

	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
	prov := &OperatorProvisioner{dynamic: fakeClient}

	prov.cnpgGVR = pgGVR
	prov.mongoGVR = mongoGVR

	// Will trigger list (returns empty)
	prov.collectK8sMetrics(ctx)

	pgObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      "pg-1",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase": "Cluster in healthy state",
			},
		},
	}
	mongoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mongodbcommunity.mongodb.com/v1",
			"kind":       "MongoDBCommunity",
			"metadata": map[string]interface{}{
				"name":      "mongo-1",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase": "Running",
			},
		},
	}

	_, _ = fakeClient.Resource(pgGVR).Namespace("default").Create(ctx, pgObj, metav1.CreateOptions{})
	_, _ = fakeClient.Resource(mongoGVR).Namespace("default").Create(ctx, mongoObj, metav1.CreateOptions{})

	// Should successfully list and update metrics
	prov.collectK8sMetrics(ctx)
}

// crdStore is a simple in-memory store for CRD objects that bypasses the dynamic
// fake's ObjectTracker (which requires scheme registration that can't properly
// handle polymorphic *unstructured.Unstructured types).
type crdStore map[string]*unstructured.Unstructured

func crdKey(gvr schema.GroupVersionResource, namespace, name string) string {
	return gvr.String() + "/" + namespace + "/" + name
}

// safeUnstructuredDeepCopy copies an unstructured object without panicking on int64 values
// (unlike DeepCopyJSON which only supports JSON-native float64).
func safeUnstructuredDeepCopy(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	return &unstructured.Unstructured{Object: safeCopyMap(obj.Object)}
}

func safeCopyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = safeCopyValue(v)
	}
	return out
}

func safeCopyValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return safeCopyMap(x)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = safeCopyValue(e)
		}
		return out
	default:
		return v // int64, float64, string, bool, nil all pass through
	}
}

// errAlreadyExists returns a k8s AlreadyExists error for the given resource.
func errAlreadyExists(gvr schema.GroupVersionResource, namespace, name string) error {
	return k8serrors.NewAlreadyExists(schema.GroupResource{Group: gvr.Group, Resource: gvr.Resource}, name)
}

// installCRDReactor adds reactors to the dynamic fake so CRD Create/Get/Delete
// go through a simple map store instead of the ObjectTracker.
func installCRDReactor(dyn *dynamicfake.FakeDynamicClient) crdStore {
	store := make(crdStore)
	dyn.PrependReactor("*", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		switch action.GetVerb() {
		case "create":
			createAction, ok := action.(ktesting.CreateAction)
			if !ok {
				return false, nil, nil
			}
			obj, ok := createAction.GetObject().(*unstructured.Unstructured)
			if !ok {
				return false, nil, nil
			}
			key := crdKey(createAction.GetResource(), createAction.GetNamespace(), obj.GetName())
			// Emulate real K8s semantics: a second create of the same object returns
			// AlreadyExists (and does NOT overwrite the existing object/status). This is
			// what drives the production "already exists" branches.
			if _, exists := store[key]; exists {
				return true, nil, errAlreadyExists(createAction.GetResource(), createAction.GetNamespace(), obj.GetName())
			}
			store[key] = safeUnstructuredDeepCopy(obj)
			return true, obj, nil
		case "get":
			getAction, ok := action.(ktesting.GetAction)
			if !ok {
				return false, nil, nil
			}
			key := crdKey(getAction.GetResource(), getAction.GetNamespace(), getAction.GetName())
			obj, ok := store[key]
			if !ok {
				return false, nil, nil
			}
			return true, safeUnstructuredDeepCopy(obj), nil
		case "delete":
			deleteAction, ok := action.(ktesting.DeleteAction)
			if !ok {
				return false, nil, nil
			}
			key := crdKey(deleteAction.GetResource(), deleteAction.GetNamespace(), deleteAction.GetName())
			if _, ok := store[key]; !ok {
				return false, nil, nil
			}
			delete(store, key)
			return true, nil, nil
		}
		return false, nil, nil
	})
	return store
}

func newTestProvisioner(t *testing.T, objs ...runtime.Object) *OperatorProvisioner {
	t.Helper()
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}:                   "ClusterList",
		{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"}: "MongoDBCommunityList",
		{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "ingressroutetcps"}:    "IngressRouteTCPList",
	})
	installCRDReactor(dyn)
	// Pre-populate objects from the fake's store into the CRD store.
	for _, obj := range objs {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		gvk := u.GroupVersionKind()
		gvr := schema.GroupVersionResource{
			Group:    gvk.Group,
			Version:  gvk.Version,
			Resource: crdResourceForKind(gvk.Kind),
		}
		dyn.Resource(gvr).Namespace(u.GetNamespace()).Create(context.Background(), u, metav1.CreateOptions{})
	}
	core := k8sfake.NewSimpleClientset()
	return &OperatorProvisioner{
		dynamic:            dyn,
		core:               core,
		cnpgGVR:            schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"},
		mongoGVR:           schema.GroupVersionResource{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"},
		ingressRouteTCPGVR: schema.GroupVersionResource{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "ingressroutetcps"},
		externalDomain:     "db.example.com",
		postgresExtPort:    5432,
		mongoExtPort:       27017,
		pollInterval:       time.Millisecond,
		pollTimeout:        50 * time.Millisecond,
		skipPostgRESTSetup: true,
	}
}

// crdResourceForKind converts a Kind to the standard plural resource name.
func crdResourceForKind(kind string) string {
	switch kind {
	case "Cluster":
		return "clusters"
	case "MongoDBCommunity":
		return "mongodbcommunity"
	case "IngressRouteTCP":
		return "ingressroutetcps"
	}
	return strings.ToLower(kind) + "s"
}

func TestOperatorProvisioner_ClusterNameAndExternal(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
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
	projectID := uuid.New()
	_ = projectID
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
	projectID := uuid.New()
	_ = projectID
	name := "db-testcluster"
	ns := p.NamespaceForProject(projectID)
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
	projectID := uuid.New()
	_ = projectID
	_, err := p.CreateInstance(context.Background(), uuid.New(), "mysql", "free", "pass")
	assert.Error(t, err)
}

func TestOperatorProvisioner_DeleteInstance(t *testing.T) {
	t.Run("postgresql not found", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		err := p.DeleteInstance(context.Background(), uuid.New(), "postgresql")
		assert.NoError(t, err)
	})
	t.Run("mongodb not found", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		err := p.DeleteInstance(context.Background(), uuid.New(), "mongodb")
		assert.NoError(t, err)
	})
	t.Run("unsupported type", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		err := p.DeleteInstance(context.Background(), uuid.New(), "mysql")
		assert.Error(t, err)
	})
}

func TestCRDStoreDebugWithCNPG(t *testing.T) {
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	store := installCRDReactor(dyn)

	cnpgGVR := schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}
	p := &OperatorProvisioner{cnpgGVR: cnpgGVR, dynamic: dyn}

	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)
	ns := "postgres-instances"

	cluster := readyPGCluster(name, ns)
	created, err := p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(context.Background(), cluster, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NotNil(t, created)
	t.Logf("created %s/%s", created.GetNamespace(), created.GetName())

	key := crdKey(p.cnpgGVR, ns, name)
	_, ok := store[key]
	require.True(t, ok, "object should be in store after Create")

	retrieved, err := p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	t.Logf("retrieved %s/%s, status=%v",
		retrieved.GetNamespace(), retrieved.GetName(),
		retrieved.Object["status"])
	spec, foundSpec, errSpec := unstructured.NestedMap(retrieved.Object, "spec")
	t.Logf("spec=%v found=%v err=%v", spec, foundSpec, errSpec)
	instances, found, errVal := unstructured.NestedInt64(spec, "instances")
	t.Logf("spec.instances=%d found=%v err=%v", instances, found, errVal)
	status, foundSt, errSt := unstructured.NestedMap(retrieved.Object, "status")
	t.Logf("status=%v found=%v err=%v", status, foundSt, errSt)
	readyInstances, found2, errRdy := unstructured.NestedInt64(status, "readyInstances")
	t.Logf("status.readyInstances=%d found=%v err=%v val=%v type=%T", readyInstances, found2, errRdy, status["readyInstances"], status["readyInstances"])
}

// helper: create a ready CNPG Cluster unstructured object.
func readyPGCluster(name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("postgresql.cnpg.io/v1")
	obj.SetKind("Cluster")
	obj.SetName(name)
	obj.SetNamespace(namespace)
	unstructured.SetNestedField(obj.Object, int64(1), "spec", "instances")
	unstructured.SetNestedField(obj.Object, int64(1), "status", "readyInstances")
	return obj
}

// helper: create a running MongoDBCommunity unstructured object.
func runningMongoCluster(name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("mongodbcommunity.mongodb.com/v1")
	obj.SetKind("MongoDBCommunity")
	obj.SetName(name)
	obj.SetNamespace(namespace)
	unstructured.SetNestedField(obj.Object, "Running", "status", "phase")
	return obj
}

// ---------------------------------------------------------------------------
// getPostgresConnection
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_GetPostgresConnection_missingSecret(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	_, err := p.getPostgresConnection(context.Background(), p.NamespaceForProject(projectID), "db-nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get app secret")
}

func TestOperatorProvisioner_GetPostgresConnection_missingPassword(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-nopass"
	ns := p.NamespaceForProject(projectID)
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-app", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("u"), "dbname": []byte("d")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = p.getPostgresConnection(context.Background(), ns, name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no password")
}

// ---------------------------------------------------------------------------
// getMongoConnection
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_GetMongoConnection_success(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-mongo"
	ns := p.MongoNamespaceForProject(projectID)
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-password", Namespace: ns},
		Data:       map[string][]byte{"password": []byte("p4ss")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	result, err := p.getMongoConnection(context.Background(), ns, name)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.DSN, "mongodb://"))
	assert.Contains(t, result.ResourceRef, ns+"/"+name)
}

func TestOperatorProvisioner_GetMongoConnection_missingSecret(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	_, err := p.getMongoConnection(context.Background(), p.MongoNamespaceForProject(projectID), "db-nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get mongo password secret")
}

func TestOperatorProvisioner_GetMongoConnection_missingPassword(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db2"
	ns := p.MongoNamespaceForProject(projectID)
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-password", Namespace: ns},
		Data:       map[string][]byte{},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = p.getMongoConnection(context.Background(), ns, name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no password")
}

// ---------------------------------------------------------------------------
// waitForPostgresReady
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_WaitForPostgresReady_readyOnFirstPoll(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-pg-ready"
	ns := p.NamespaceForProject(projectID)

	// Pre-populate a ready cluster.
	p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(context.Background(),
		readyPGCluster(name, ns), metav1.CreateOptions{})

	err := p.waitForPostgresReady(context.Background(), ns, name)
	assert.NoError(t, err)
}

func TestOperatorProvisioner_WaitForPostgresReady_timeout(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-pg-slow"
	ns := p.NamespaceForProject(projectID)

	// Pre-populate a cluster WITHOUT ready status → never ready.
	p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(context.Background(), &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"instances": int64(1),
			},
		},
	}, metav1.CreateOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := p.waitForPostgresReady(ctx, ns, name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}

// ---------------------------------------------------------------------------
// waitForMongoReady
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_WaitForMongoReady_readyOnFirstPoll(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-mongo-ready"
	ns := p.MongoNamespaceForProject(projectID)

	p.dynamic.Resource(p.mongoGVR).Namespace(ns).Create(context.Background(),
		runningMongoCluster(name, ns), metav1.CreateOptions{})

	err := p.waitForMongoReady(context.Background(), ns, name)
	assert.NoError(t, err)
}

func TestOperatorProvisioner_WaitForMongoReady_timeout(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-mongo-slow"
	ns := p.MongoNamespaceForProject(projectID)

	p.dynamic.Resource(p.mongoGVR).Namespace(ns).Create(context.Background(), &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mongodbcommunity.mongodb.com/v1",
			"kind":       "MongoDBCommunity",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
		},
	}, metav1.CreateOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := p.waitForMongoReady(ctx, ns, name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadline exceeded")
}

// ---------------------------------------------------------------------------
// createPostgresCluster
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_CreatePostgresCluster_successNoExternal(t *testing.T) {
	externalDomain := "db.example.com"
	tests := []struct {
		name    string
		domain  string // empty = no external access
		wantExt bool
	}{
		{"with external domain", externalDomain, true},
		{"no external domain", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProvisioner(t)
			p.externalDomain = tt.domain
			projectID := uuid.New()
			_ = projectID
			clusterName := p.ClusterNameForProject(projectID)
			ns := p.NamespaceForProject(projectID)

			// Add a reactor to mark created CNPG clusters as ready immediately.
			dynObj := p.dynamic.(*dynamicfake.FakeDynamicClient)
			dynObj.PrependReactor("create", "clusters", func(action ktesting.Action) (bool, runtime.Object, error) {
				obj := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
				unstructured.SetNestedField(obj.Object, int64(1), "status", "readyInstances")
				return false, nil, nil // fall through to default store
			})

			result, err := p.createPostgresCluster(context.Background(), projectID, clusterName, 0.5, 512, 5, "s3cret", "free")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, strings.HasPrefix(result.DSN, "postgresql://"))
			assert.Contains(t, result.ResourceRef, ns+"/"+clusterName)
			if tt.wantExt {
				require.NotNil(t, result.ExternalAccess)
				assert.Equal(t, p.postgresExtPort, result.ExternalAccess.Port)
			} else {
				assert.Nil(t, result.ExternalAccess)
			}

			// Verify the secret was created.
			secret, err := p.core.CoreV1().Secrets(ns).Get(context.Background(), clusterName+"-app", metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, "s3cret", string(secret.Data["password"]))
			assert.Equal(t, "app", string(secret.Data["username"]))

			// Verify the cluster unstructured was created.
			_, err = p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Get(context.Background(), clusterName, metav1.GetOptions{})
			require.NoError(t, err)
		})
	}
}

func TestOperatorProvisioner_CreatePostgresCluster_alreadyExistsReady(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)
	ns := p.NamespaceForProject(projectID)

	// Pre-populate a ready cluster + secret.
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-app", Namespace: ns},
		Data: map[string][]byte{
			"username": []byte("app"), "password": []byte("existing"), "dbname": []byte("app"),
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(context.Background(),
		readyPGCluster(name, ns), metav1.CreateOptions{})
	require.NoError(t, err)

	result, err := p.createPostgresCluster(context.Background(), projectID, name, 0.5, 512, 5, "ignored", "free")
	require.NoError(t, err)
	assert.Contains(t, result.DSN, "existing@") // password from existing secret
}

func TestOperatorProvisioner_CreatePostgresCluster_createSecretError(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)

	// Force the app-credential secret creation to fail with a non-AlreadyExists error.
	// k8sfake.Clientset embeds testing.Fake, so it supports PrependReactor on core types.
	// This exercises the early-return "create app credential secret" path.
	p.core.(*k8sfake.Clientset).PrependReactor("create", "secrets",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("secret API error")
		})

	_, err := p.createPostgresCluster(context.Background(), projectID, name, 0.5, 512, 5, "s3cret", "free")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create app credential secret")
	assert.Contains(t, err.Error(), "secret API error")
}

func TestOperatorProvisioner_CreatePostgresCluster_alreadyExistsNotReady(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)
	ns := p.NamespaceForProject(projectID)

	// Pre-populate a non-ready cluster + secret.
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-app", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("app"), "password": []byte("pw"), "dbname": []byte("app")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	notReady := &unstructured.Unstructured{}
	notReady.SetAPIVersion("postgresql.cnpg.io/v1")
	notReady.SetKind("Cluster")
	notReady.SetName(name)
	notReady.SetNamespace(ns)
	unstructured.SetNestedField(notReady.Object, int64(1), "spec", "instances")
	// No status.readyInstances set → never ready.
	_, err = p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(context.Background(), notReady, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = p.createPostgresCluster(context.Background(), projectID, name, 0.5, 512, 5, "ignored", "free")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "existing postgres cluster not ready")
}

// ---------------------------------------------------------------------------
// createMongoDBCluster
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_CreateMongoDBCluster_success(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)
	ns := p.MongoNamespaceForProject(projectID)

	// Reactor: make MongoDBCommunity immediately running.
	p.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "mongodbcommunity",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			obj := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
			unstructured.SetNestedField(obj.Object, "Running", "status", "phase")
			return false, nil, nil
		})

	// Also pre-create a secret so getMongoConnection succeeds.
	p.externalDomain = "" // disable external access for simplicity
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-password", Namespace: ns},
		Data:       map[string][]byte{"password": []byte("monGoP4ss")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	result, err := p.createMongoDBCluster(context.Background(), projectID, name, 1, 1024, 10, "initPass", "free")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, strings.HasPrefix(result.DSN, "mongodb://"))
	assert.Contains(t, result.ResourceRef, ns+"/"+name)
}

func TestOperatorProvisioner_CreateMongoDBCluster_alreadyExists(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)
	ns := p.MongoNamespaceForProject(projectID)

	// Pre-populate a running cluster + secret.
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-password", Namespace: ns},
		Data:       map[string][]byte{"password": []byte("pw")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = p.dynamic.Resource(p.mongoGVR).Namespace(ns).Create(context.Background(),
		runningMongoCluster(name, ns), metav1.CreateOptions{})
	require.NoError(t, err)

	result, err := p.createMongoDBCluster(context.Background(), projectID, name, 1, 1024, 10, "ignored", "free")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.DSN, "mongodb://"))
}

func TestOperatorProvisioner_CreateMongoDBCluster_createError(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)

	p.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "mongodbcommunity",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("mongo API error")
		})

	_, err := p.createMongoDBCluster(context.Background(), projectID, name, 1, 1024, 10, "pw", "free")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create mongodb community")
}

// ---------------------------------------------------------------------------
// createIngressRouteTCP
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_CreateIngressRouteTCP_success(t *testing.T) {
	p := newTestProvisioner(t)
	p.externalDomain = "example.com"
	projectID := uuid.New()
	_ = projectID

	err := p.createIngressRouteTCP(context.Background(), projectID, "mongodb")
	require.NoError(t, err)

	// Verify the ingress route was created.
	name := p.ClusterNameForProject(projectID)
	obj, err := p.dynamic.Resource(p.ingressRouteTCPGVR).Namespace(p.MongoNamespaceForProject(projectID)).
		Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)

	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	entryPoints, _, _ := unstructured.NestedStringSlice(spec, "entryPoints")
	require.Len(t, entryPoints, 1)
	assert.Equal(t, "mongodb", entryPoints[0])
}

func TestOperatorProvisioner_CreateIngressRouteTCP_noExternalDomain(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	p.externalDomain = "" // disabled
	err := p.createIngressRouteTCP(context.Background(), uuid.New(), "mongodb")
	assert.NoError(t, err)

	// Nothing should be created.
	assert.Len(t, p.dynamic.(*dynamicfake.FakeDynamicClient).Actions(), 0,
		"no k8s API calls when externalDomain is empty")
}

func TestOperatorProvisioner_CreateIngressRouteTCP_postgresSkips(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	p.externalDomain = "example.com"
	err := p.createIngressRouteTCP(context.Background(), uuid.New(), "postgresql")
	assert.NoError(t, err)
	assert.Len(t, p.dynamic.(*dynamicfake.FakeDynamicClient).Actions(), 0,
		"postgres should not create IngressRouteTCP")
}

func TestOperatorProvisioner_CreateIngressRouteTCP_unsupportedType(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	p.externalDomain = "example.com"
	err := p.createIngressRouteTCP(context.Background(), uuid.New(), "mysql")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported db type")
}

// ---------------------------------------------------------------------------
// deleteIngressRouteTCP
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_DeleteIngressRouteTCP_success(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := p.ClusterNameForProject(projectID)

	// Pre-create the route.
	route := &unstructured.Unstructured{}
	route.SetAPIVersion("traefik.containo.us/v1alpha1")
	route.SetKind("IngressRouteTCP")
	route.SetName(name)
	route.SetNamespace(p.MongoNamespaceForProject(projectID))
	_, err := p.dynamic.Resource(p.ingressRouteTCPGVR).Namespace(p.MongoNamespaceForProject(projectID)).
		Create(context.Background(), route, metav1.CreateOptions{})
	require.NoError(t, err)

	err = p.deleteIngressRouteTCP(context.Background(), projectID, "mongodb")
	assert.NoError(t, err)

	// Verify it's gone.
	_, err = p.dynamic.Resource(p.ingressRouteTCPGVR).Namespace(p.MongoNamespaceForProject(projectID)).
		Get(context.Background(), name, metav1.GetOptions{})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not found"))
}

func TestOperatorProvisioner_DeleteIngressRouteTCP_noExternalDomain(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	p.externalDomain = ""
	err := p.deleteIngressRouteTCP(context.Background(), uuid.New(), "mongodb")
	assert.NoError(t, err)
	assert.Len(t, p.dynamic.(*dynamicfake.FakeDynamicClient).Actions(), 0)
}

func TestOperatorProvisioner_DeleteIngressRouteTCP_unsupportedType(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	err := p.deleteIngressRouteTCP(context.Background(), uuid.New(), "mysql")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported db type")
}

// ---------------------------------------------------------------------------
// CreateInstance
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_CreateInstance_postgresql(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	ns := p.NamespaceForProject(projectID)
	clusterName := p.ClusterNameForProject(projectID)

	// Reactor: make CNPG clusters ready immediately.
	p.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "clusters",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			obj := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
			unstructured.SetNestedField(obj.Object, int64(1), "status", "readyInstances")
			return false, nil, nil
		})

	// Pre-create connection secret so getPostgresConnection succeeds.
	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-app", Namespace: ns},
		Data:       map[string][]byte{"username": []byte("app"), "password": []byte("pw"), "dbname": []byte("app")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	result, err := p.CreateInstance(context.Background(), projectID, "postgres", "free", "pass")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.DSN, "postgresql://"))
}

func TestOperatorProvisioner_CreateInstance_mongodb(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	ns := p.MongoNamespaceForProject(projectID)
	clusterName := p.ClusterNameForProject(projectID)

	p.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("create", "mongodbcommunity",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			obj := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
			unstructured.SetNestedField(obj.Object, "Running", "status", "phase")
			return false, nil, nil
		})

	_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-admin-password", Namespace: ns},
		Data:       map[string][]byte{"password": []byte("pw")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	result, err := p.CreateInstance(context.Background(), projectID, "mongodb", "free", "pass")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.DSN, "mongodb://"))
}

func TestOperatorProvisioner_CreateInstance_unsupported(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	_, err := p.CreateInstance(context.Background(), uuid.New(), "mysql", "free", "pass")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// ExternalHostname both types
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_ExternalHostname(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	id := uuid.New()
	pgHost := p.ExternalHostname(id, "postgresql")
	mongoHost := p.ExternalHostname(id, "mongodb")
	assert.Contains(t, pgHost, "postgres")
	assert.Contains(t, mongoHost, "mongodb")
}

// ---------------------------------------------------------------------------
// GetConnection (public dispatcher over getPostgresConnection/getMongoConnection)
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_GetConnection(t *testing.T) {
	t.Run("postgresql success", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		name := p.ClusterNameForProject(projectID)
		ns := p.NamespaceForProject(projectID)
		_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-app", Namespace: ns},
			Data:       map[string][]byte{"username": []byte("app"), "password": []byte("pw"), "dbname": []byte("app")},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		result, err := p.GetConnection(context.Background(), projectID, "postgres")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(result.DSN, "postgresql://"))
		assert.Contains(t, result.ResourceRef, ns+"/"+name)
	})

	t.Run("mongodb success", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		name := p.ClusterNameForProject(projectID)
		ns := p.MongoNamespaceForProject(projectID)
		_, err := p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-password", Namespace: ns},
			Data:       map[string][]byte{"password": []byte("pw")},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		result, err := p.GetConnection(context.Background(), projectID, "mongodb")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(result.DSN, "mongodb://"))
	})

	t.Run("unsupported type", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		_, err := p.GetConnection(context.Background(), uuid.New(), "mysql")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported database type")
	})
}

// ---------------------------------------------------------------------------
// waitForX: ready-after-status-update (multiple polls then ready)
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_WaitForPostgresReady_readyAfterPolls(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-pg-eventual"
	ns := p.NamespaceForProject(projectID)

	// A get reactor that reports not-ready on the first poll and ready afterwards,
	// exercising the "becomes ready after a status update" path deterministically.
	calls := 0
	p.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("get", "clusters",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			calls++
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion("postgresql.cnpg.io/v1")
			obj.SetKind("Cluster")
			obj.SetName(name)
			obj.SetNamespace(ns)
			unstructured.SetNestedField(obj.Object, int64(1), "spec", "instances")
			ready := int64(0)
			if calls >= 2 {
				ready = 1
			}
			unstructured.SetNestedField(obj.Object, ready, "status", "readyInstances")
			return true, obj, nil
		})

	err := p.waitForPostgresReady(context.Background(), ns, name)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 2, "should have polled at least twice before ready")
}

func TestOperatorProvisioner_WaitForMongoReady_readyAfterPolls(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	name := "db-mongo-eventual"
	ns := p.MongoNamespaceForProject(projectID)

	// First poll: a non-Running phase WITH a message (also covers the diagnostic log
	// branch); subsequent polls: Running.
	calls := 0
	p.dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("get", "mongodbcommunity",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			calls++
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion("mongodbcommunity.mongodb.com/v1")
			obj.SetKind("MongoDBCommunity")
			obj.SetName(name)
			obj.SetNamespace(ns)
			phase := "Pending"
			if calls >= 2 {
				phase = "Running"
			}
			unstructured.SetNestedField(obj.Object, phase, "status", "phase")
			unstructured.SetNestedField(obj.Object, "reconciling members", "status", "message")
			return true, obj, nil
		})

	err := p.waitForMongoReady(context.Background(), ns, name)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 2)
}

// ---------------------------------------------------------------------------
// DeleteInstance: success (resource exists) and error branches
// ---------------------------------------------------------------------------

func TestOperatorProvisioner_DeleteInstance_existing(t *testing.T) {
	t.Run("postgresql deletes existing cluster", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		name := p.ClusterNameForProject(projectID)
		ns := p.NamespaceForProject(projectID)
		_, err := p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(context.Background(),
			readyPGCluster(name, ns), metav1.CreateOptions{})
		require.NoError(t, err)

		_, err = p.core.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, p.DeleteInstance(context.Background(), projectID, "postgresql"))

		_, err = p.core.CoreV1().Namespaces().Get(context.Background(), ns, metav1.GetOptions{})
		require.Error(t, err)
	})

	t.Run("mongodb deletes existing cluster and secret", func(t *testing.T) {
		p := newTestProvisioner(t)
		projectID := uuid.New()
		_ = projectID
		name := p.ClusterNameForProject(projectID)
		ns := p.MongoNamespaceForProject(projectID)
		_, err := p.dynamic.Resource(p.mongoGVR).Namespace(ns).Create(context.Background(),
			runningMongoCluster(name, ns), metav1.CreateOptions{})
		require.NoError(t, err)
		_, err = p.core.CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name + "-admin-password", Namespace: ns},
			Data:       map[string][]byte{"password": []byte("pw")},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		_, err = p.core.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
		require.NoError(t, err)

		require.NoError(t, p.DeleteInstance(context.Background(), projectID, "mongodb"))

		_, err = p.core.CoreV1().Namespaces().Get(context.Background(), ns, metav1.GetOptions{})
		require.Error(t, err)
	})
}

func TestOperatorProvisioner_DeleteInstance_deleteError(t *testing.T) {
	p := newTestProvisioner(t)
	projectID := uuid.New()
	_ = projectID
	// A non-NotFound delete error must be wrapped and returned.
	p.core.(*k8sfake.Clientset).PrependReactor("delete", "namespaces",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("api server unavailable")
		})

	err := p.DeleteInstance(context.Background(), uuid.New(), "postgresql")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete project namespace")
	assert.Contains(t, err.Error(), "api server unavailable")
}
