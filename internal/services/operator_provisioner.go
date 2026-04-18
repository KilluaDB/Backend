package services

import (
	"backend/internal/utils"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// TODO(sharding): Namespace sharding for scale
//
// Current design puts all clusters in a single namespace (postgres-instances,
// mongodb-instances). This works up to ~10,000 clusters but degrades beyond
// that due to Kubernetes API server list/watch cost, CNPG operator reconcile
// load, and etcd hotspots.
//
// When needed, shard by first byte of project UUID:
//
//	shard = projectID[0] % numShards
//	namespace = "postgres-instances-{shard}"
//
// This is deterministic — no shard mapping needs to be stored. UUID v4 first
// byte is uniformly random so shards fill evenly. Start with 16 shards
// (~6,250 clusters/shard at 100k total), design for 256.
//
// Changes required:
//   - Replace postgresNamespace/mongoNamespace fields with prefix + numShards
//   - Add postgresNamespaceForProject(projectID) / mongoNamespaceForProject(projectID)
//   - Pre-create shard namespaces in cluster setup script
//   - Configure CNPG operator to watch all shard namespaces via label selector
//   - For migration: check shard namespace first, fall back to legacy namespace
//     until all existing clusters are recreated
//
// Do not decrease numShards after deployment — it reshuffles project→namespace
// mapping and loses cluster references.

// ProvisionResult holds connection info and resource ref after successful provisioning.
type ProvisionResult struct {
	DSN         string // postgresql://user:pass@host:5432/app?sslmode=disable
	ResourceRef string // namespace/name for deletion
}

const (
	postgresAppDBName  = "app"
	postgresAppDBOwner = "app"
)

// OperatorProvisioner creates and deletes DB instances via Kubernetes operators (CloudNativePG, MongoDB).
type OperatorProvisioner struct {
	postgresNamespace string
	mongoNamespace    string
	dynamic           dynamic.Interface
	core              kubernetes.Interface
	cnpgGVR           schema.GroupVersionResource
	mongoGVR          schema.GroupVersionResource
}

// NewOperatorProvisioner creates a provisioner using in-cluster config (when running in K8s)
// or kubeconfig (KUBECONFIG env or ~/.kube/config) when running locally.
// Postgres and MongoDB instances use separate namespaces (env: DB_INSTANCES_NAMESPACE_POSTGRES, DB_INSTANCES_NAMESPACE_MONGO).
func NewOperatorProvisioner() (*OperatorProvisioner, error) {
	postgresNS := os.Getenv("DB_INSTANCES_NAMESPACE_POSTGRES")
	if postgresNS == "" {
		postgresNS = "postgres-instances"
	}
	mongoNS := os.Getenv("DB_INSTANCES_NAMESPACE_MONGO")
	if mongoNS == "" {
		mongoNS = "mongodb-instances"
	}

	config, err := rest.InClusterConfig()
	if err != nil { // not running in-cluster, fall back to local kubeconfig
		// Not in cluster: use kubeconfig (env or default path)
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home := os.Getenv("HOME")
			if home == "" {
				home = os.Getenv("USERPROFILE")
			}
			if home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}
		if kubeconfig == "" {
			return nil, fmt.Errorf("kubernetes config: not in cluster and no kubeconfig (set KUBECONFIG or use ~/.kube/config): %w", err)
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("kubernetes config: %w", err)
		}
	}

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}

	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("core client: %w", err)
	}

	return &OperatorProvisioner{
		postgresNamespace: postgresNS,
		mongoNamespace:    mongoNS,
		dynamic:           dyn,
		core:              core,
		cnpgGVR:           schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"},
		mongoGVR:          schema.GroupVersionResource{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"},
	}, nil
}

// CreateInstance provisions a DB instance (PostgreSQL or MongoDB) via the appropriate operator.
func (p *OperatorProvisioner) CreateInstance(ctx context.Context, projectID uuid.UUID, dbType string, tier string) (*ProvisionResult, error) {
	dbKind := dbType
	if dbType == "postgres" {
		dbKind = "postgresql"
	}

	name := p.ClusterNameForProject(projectID)
	cpu, memoryMB, storageGB, err := p.tierResources(tier)
	if err != nil {
		return nil, err
	}
	switch dbKind {
	case "postgresql":
		return p.createPostgresCluster(ctx, name, cpu, memoryMB, storageGB)
	case "mongodb":
		return p.createMongoDBCluster(ctx, name, cpu, memoryMB, storageGB)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// GetConnection returns live connection info for an existing database instance.
// Credentials are read from the operator-managed secret at call time and never stored.
// name is derived deterministically from projectID so no resource ref needs to be persisted.
func (p *OperatorProvisioner) GetConnection(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error) {
	name := p.ClusterNameForProject(projectID)
	switch dbType {
	case "postgres", "postgresql":
		return p.getPostgresConnection(ctx, p.postgresNamespace, name)
	case "mongodb":
		return p.getMongoConnection(ctx, p.mongoNamespace, name)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// DeleteInstance deletes the K8s resource (Cluster or MongoDBCommunity) identified by resourceRef (namespace/name).
func (p *OperatorProvisioner) DeleteInstance(ctx context.Context, projectID uuid.UUID, dbType string) error {
	name := p.ClusterNameForProject(projectID)
	switch dbType {
	case "postgres", "postgresql":
		err := p.dynamic.Resource(p.cnpgGVR).Namespace(p.postgresNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete postgres cluster: %w", err)
		}
		return nil
	case "mongodb":
		err := p.dynamic.Resource(p.mongoGVR).Namespace(p.mongoNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("delete mongodb: %w", err)
		}
		_ = p.core.CoreV1().Secrets(p.mongoNamespace).Delete(ctx, name+"-admin-password", metav1.DeleteOptions{})
		return nil
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// ClusterNameForProject returns the K8s resource name used for a project's DB (same as CreateInstance).
func (p *OperatorProvisioner) ClusterNameForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("db-%s", strings.ReplaceAll(projectID.String(), "-", ""))
}

func (p *OperatorProvisioner) createPostgresCluster(ctx context.Context, name string, cpu, memoryMB float64, storageGB int) (*ProvisionResult, error) {
	ns := p.postgresNamespace
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"instances": 1,
				"bootstrap": map[string]interface{}{
					"initdb": map[string]interface{}{
						"database": postgresAppDBName,
						"owner":    postgresAppDBOwner,
					},
				},
				"postgresql": map[string]interface{}{
					"parameters": map[string]interface{}{
						"pg_stat_statements.max":   "10000",
						"pg_stat_statements.track": "all",
					},
				},
				"storage": map[string]interface{}{
					"size": fmt.Sprintf("%dGi", storageGB),
				},
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    fmt.Sprintf("%.1f", cpu),
						"memory": fmt.Sprintf("%.0fMi", memoryMB),
					},
					"limits": map[string]interface{}{
						"cpu":    fmt.Sprintf("%.1f", cpu),
						"memory": fmt.Sprintf("%.0fMi", memoryMB),
					},
				},
			},
		},
	}
	_, err := p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(ctx, cluster, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			if err := p.waitForPostgresReady(ctx, ns, name); err != nil {
				return nil, fmt.Errorf("existing postgres cluster not ready: %w", err)
			}
			return p.getPostgresConnection(ctx, ns, name)
		}
	}

	log.Printf("Created PostgreSQL cluster %s/%s, waiting for ready", ns, name)
	if err := p.waitForPostgresReady(ctx, ns, name); err != nil {
		_ = p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("postgres cluster not ready: %w", err)
	}

	return p.getPostgresConnection(ctx, ns, name)
}
func (p *OperatorProvisioner) waitForPostgresReady(ctx context.Context, namespace, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		cluster, err := p.dynamic.Resource(p.cnpgGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		spec, _, _ := unstructured.NestedMap(cluster.Object, "spec")
		instances, _, _ := unstructured.NestedInt64(spec, "instances")
		status, _, _ := unstructured.NestedMap(cluster.Object, "status")
		readyInstances, _, _ := unstructured.NestedInt64(status, "readyInstances")
		return readyInstances >= instances && readyInstances > 0, nil
	})
}
func (p *OperatorProvisioner) getPostgresConnection(ctx context.Context, namespace, name string) (*ProvisionResult, error) {
	host := fmt.Sprintf("%s-rw.%s.svc.cluster.local", name, namespace)
	appSecretName := name + "-app"
	secret, err := p.core.CoreV1().Secrets(namespace).Get(ctx, appSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get app secret %s: %w", appSecretName, err)
	}
	username := string(secret.Data["username"])
	if username == "" {
		username = postgresAppDBOwner
	}
	password := string(secret.Data["password"])
	if password == "" {
		return nil, fmt.Errorf("app secret %s has no password", appSecretName)
	}
	database := string(secret.Data["dbname"])
	if database == "" {
		database = postgresAppDBName
	}
	userInfo := url.UserPassword(username, password)
	return &ProvisionResult{
		DSN:         fmt.Sprintf("postgresql://%s@%s:5432/%s?sslmode=require", userInfo.String(), host, database),
		ResourceRef: namespace + "/" + name,
	}, nil
}

func (p *OperatorProvisioner) createMongoDBCluster(ctx context.Context, name string, cpu, memoryMB float64, storageGB int) (*ProvisionResult, error) {
	ns := p.mongoNamespace
	password, err := utils.GeneratePasswordBase64(48)
	if err != nil {
		return nil, fmt.Errorf("generate cluster admin password: %w", err)
	}

	passSecretName := name + "-admin-password"
	passSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      passSecretName,
			Namespace: ns,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(password),
		},
	}

	_, err = p.core.CoreV1().Secrets(ns).Create(ctx, passSecret, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create cluster password secret: %w", err)
	}
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mongodbcommunity.mongodb.com/v1",
			"kind":       "MongoDBCommunity",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"members": 1,
				"type":    "ReplicaSet",
				"version": "7.0.0",
				"security": map[string]interface{}{
					"authentication": map[string]interface{}{
						"modes": []interface{}{"SCRAM"},
					},
					"tls": map[string]interface{}{
						"enabled": false,
					},
				},
				"users": []interface{}{
					map[string]interface{}{
						"name": "admin",
						"db":   "admin",
						"passwordSecretRef": map[string]interface{}{
							"name": passSecretName,
						},
						"roles": []interface{}{
							map[string]interface{}{"name": "clusterAdmin", "db": "admin"},
							map[string]interface{}{"name": "readWriteAnyDatabase", "db": "admin"},
						},
						"scramCredentialsSecretName": name + "-admin-scram",
					},
				},
				"statefulSet": map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name": "mongod",
										"resources": map[string]interface{}{
											"limits": map[string]interface{}{
												"cpu":    fmt.Sprintf("%.1f", cpu),
												"memory": fmt.Sprintf("%.0fMi", memoryMB),
											},
										},
									},
								},
							},
						},
						"volumeClaimTemplates": []interface{}{
							map[string]interface{}{
								"metadata": map[string]interface{}{"name": "data-volume"},
								"spec": map[string]interface{}{
									"accessModes": []interface{}{"ReadWriteOnce"},
									"resources": map[string]interface{}{
										"requests": map[string]interface{}{"storage": fmt.Sprintf("%dGi", storageGB)},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = p.dynamic.Resource(p.mongoGVR).Namespace(ns).Create(ctx, cluster, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return p.getMongoConnection(ctx, ns, name)
		}
		return nil, fmt.Errorf("create mongodb community: %w", err)
	}

	log.Printf("Created MongoDBCommunity %s/%s, waiting for ready", ns, name)
	if err := p.waitForMongoReady(ctx, ns, name); err != nil {
		_ = p.dynamic.Resource(p.mongoGVR).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{})
		_ = p.core.CoreV1().Secrets(ns).Delete(ctx, passSecretName, metav1.DeleteOptions{})
		return nil, fmt.Errorf("mongodb not ready: %w", err)
	}

	return p.getMongoConnection(ctx, ns, name)
}
func (p *OperatorProvisioner) waitForMongoReady(ctx context.Context, namespace, name string) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		obj, err := p.dynamic.Resource(p.mongoGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		status, _, _ := unstructured.NestedMap(obj.Object, "status")
		if status == nil {
			return false, nil
		}
		phase, _ := status["phase"].(string)
		msg, _ := status["message"].(string)
		if phase != "" && phase != "Running" && phase != "running" && msg != "" {
			log.Printf("[MongoDB %s/%s] phase=%q message=%q", namespace, name, phase, msg)
		}
		return phase == "Running" || phase == "running", nil
	})
}
func (p *OperatorProvisioner) getMongoConnection(ctx context.Context, namespace, name string) (*ProvisionResult, error) {
	secretName := name + "-admin-password"
	secret, err := p.core.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get mongo password secret %s: %w", secretName, err)
	}
	password := string(secret.Data["password"])
	if password == "" {
		return nil, fmt.Errorf("mongo password secret %s has no password", secretName)
	}
	host := fmt.Sprintf("%s-svc.%s.svc.cluster.local", name, namespace)
	userInfo := url.UserPassword("admin", password)
	return &ProvisionResult{
		DSN:         fmt.Sprintf("mongodb://%s@%s:27017/admin?authSource=admin", userInfo.String(), host),
		ResourceRef: namespace + "/" + name,
	}, nil
}
func (p *OperatorProvisioner) tierResources(tier string) (float64, float64, int, error) {
	switch tier {
	case "free":
		return 0.5, 512, 5, nil
	case "basic":
		return 1.0, 1024, 10, nil
	case "premium":
		return 2.0, 2048, 20, nil
	default:
		return 0, 0, 0, fmt.Errorf("unknown tier: %s", tier)
	}
}
