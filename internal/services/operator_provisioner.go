package services

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/utils"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// ProvisionResult holds connection info and resource ref after successful provisioning.
type ProvisionResult struct {
	Host        string
	Port        int
	User        string
	Password    string
	Database    string
	ResourceRef string // "namespace/name" for deletion
}

// OperatorProvisioner creates and deletes DB instances via Kubernetes operators (CloudNativePG, MongoDB).
type OperatorProvisioner struct {
	namespace  string
	dynamic    dynamic.Interface
	core       kubernetes.Interface
	cnpgGVR    schema.GroupVersionResource
	mongoGVR   schema.GroupVersionResource
	tierConfig func(string) (cpu float64, memoryMB float64, storageGB int)
	inCluster  bool
}

// NewOperatorProvisioner creates a provisioner using in-cluster config (when running in K8s)
// or kubeconfig (KUBECONFIG env or ~/.kube/config) when running locally.
func NewOperatorProvisioner() (*OperatorProvisioner, error) {
	namespace := os.Getenv("DB_INSTANCES_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	config, err := rest.InClusterConfig()
	inCluster := err == nil
	if err != nil {
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

	tierConfig := getTierConfig()

	return &OperatorProvisioner{
		namespace:  namespace,
		dynamic:    dyn,
		core:       core,
		cnpgGVR:    schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"},
		mongoGVR:   schema.GroupVersionResource{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"},
		tierConfig: tierConfig,
		inCluster:  inCluster,
	}, nil
}

// ClusterNameForProject returns the K8s resource name used for a project's DB (same as CreateInstance).
func (p *OperatorProvisioner) ClusterNameForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("db-%s", strings.ReplaceAll(projectID.String(), "-", "")[:24])
}

// ResourceRefForProject returns "namespace/name" for the project's cluster (for discovery/heal).
func (p *OperatorProvisioner) ResourceRefForProject(projectID uuid.UUID) string {
	return p.namespace + "/" + p.ClusterNameForProject(projectID)
}

// GetTierResources returns the CPU (cores), memory (MB), and storage (GB) for a tier.
// These are the same values used when creating instances via the operator.
func (p *OperatorProvisioner) GetTierResources(tier string) (cpu float64, memoryMB float64, storageGB int) {
	return p.tierConfig(tier)
}

// CreateInstance provisions a DB instance (PostgreSQL or MongoDB) via the appropriate operator.
func (p *OperatorProvisioner) CreateInstance(ctx context.Context, projectID uuid.UUID, dbType string, tier string) (*ProvisionResult, error) {
	// Normalize dbType: "postgres" -> postgresql for backward compatibility
	dbKind := dbType
	if dbType == "postgres" {
		dbKind = "postgresql"
	}

	name := p.ClusterNameForProject(projectID)
	cpu, memoryMB, storageGB := p.tierConfig(tier)

	switch dbKind {
	case "postgresql":
		return p.createPostgresCluster(ctx, name, cpu, memoryMB, storageGB)
	case "mongodb":
		return p.createMongoDB(ctx, name, projectID.String(), cpu, memoryMB, storageGB) // database param used for naming only
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

func (p *OperatorProvisioner) createPostgresCluster(ctx context.Context, name string, cpu, memoryMB float64, storageGB int) (*ProvisionResult, error) {
	size := fmt.Sprintf("%dGi", storageGB)
	if size == "0Gi" {
		size = "1Gi"
	}

	// Create admin user secret used by CNPG initdb bootstrap (single user per instance).
	adminSecretName := name + "-admin"
	adminUsername := "admin"
	adminPassword, err := utils.GeneratePasswordBase64(48)
	if err != nil {
		return nil, fmt.Errorf("generate admin user password: %w", err)
	}
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      adminSecretName,
			Namespace: p.namespace,
		},
		Type: corev1.SecretTypeBasicAuth,
		StringData: map[string]string{
			"username": adminUsername,
			"password": adminPassword,
		},
	}
	_, createErr := p.core.CoreV1().Secrets(p.namespace).Create(ctx, adminSecret, metav1.CreateOptions{})
	if createErr != nil {
		if !errors.IsAlreadyExists(createErr) {
			return nil, fmt.Errorf("create admin user secret %s: %w", adminSecretName, createErr)
		}
		// Secret exists (e.g. from a previous failed run); update with a fresh password for this cluster.
		existing, getErr := p.core.CoreV1().Secrets(p.namespace).Get(ctx, adminSecretName, metav1.GetOptions{})
		if getErr != nil {
			return nil, fmt.Errorf("get admin user secret %s: %w", adminSecretName, getErr)
		}
		existing.Data = map[string][]byte{
			"username": []byte(adminUsername),
			"password": []byte(adminPassword),
		}
		if _, err := p.core.CoreV1().Secrets(p.namespace).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("update admin user secret %s: %w", adminSecretName, err)
		}
	}

	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": p.namespace,
			},
			"spec": map[string]interface{}{
				"instances":             1,
				"enableSuperuserAccess": true, // superuser for operator; app uses single admin user
				"bootstrap": map[string]interface{}{
					"initdb": map[string]interface{}{
						// Single admin user; database "app" for connection compatibility.
						"database": "app",
						"owner":    adminUsername,
						"secret": map[string]interface{}{
							"name": adminSecretName,
						},
					},
				},
				"storage": map[string]interface{}{
					"size": size,
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

	_, err = p.dynamic.Resource(p.cnpgGVR).Namespace(p.namespace).Create(ctx, cluster, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Use existing cluster: read secret and return connection info
			return p.getPostgresConnectionFromCluster(ctx, name, "app")
		}
		return nil, fmt.Errorf("create postgresql cluster: %w", err)
	}

	log.Printf("Created PostgreSQL cluster %s/%s, waiting for ready", p.namespace, name)
	if err := p.waitForPostgresReady(ctx, name); err != nil {
		_ = p.dynamic.Resource(p.cnpgGVR).Namespace(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("postgresql cluster not ready: %w", err)
	}

	return p.getPostgresConnectionFromCluster(ctx, name, "app")
}

func (p *OperatorProvisioner) waitForPostgresReady(ctx context.Context, name string) error {
	secretName := name + "-superuser"
	// Wait for superuser secret (created by CNPG when enableSuperuserAccess=true). Bounded by parent context (e.g. 6m from CreateInstance); 10m is local upper bound.
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		secret, err := p.core.CoreV1().Secrets(p.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		// Secret exists; ensure it has a password (operator may create it before filling)
		passBytes := secret.Data["password"]
		if len(passBytes) == 0 {
			return false, nil
		}
		// When running inside the cluster, also wait until the DB actually accepts the credentials.
		// This avoids returning early in cases where the secret is present but the password isn't applied yet.
		if p.inCluster {
			user := string(secret.Data["username"])
			if user == "" {
				user = "postgres"
			}
			password := string(passBytes)
			host := fmt.Sprintf("%s-rw.%s.svc.cluster.local", name, p.namespace)
			userInfo := url.UserPassword(user, password)
			dsn := fmt.Sprintf("postgresql://%s@%s:5432/postgres?sslmode=disable", userInfo.String(), host)

			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			conn, connErr := pgx.Connect(pingCtx, dsn)
			if connErr != nil {
				return false, nil
			}
			_ = conn.Close(pingCtx)
		}
		return true, nil
	})
}

func (p *OperatorProvisioner) getPostgresConnectionFromCluster(ctx context.Context, name, database string) (*ProvisionResult, error) {
	// Prefer admin secret (new clusters); fall back to app-user for backward compatibility.
	adminSecretName := name + "-admin"
	appUserSecretName := name + "-app-user"
	secret, err := p.core.CoreV1().Secrets(p.namespace).Get(ctx, adminSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			secret, err = p.core.CoreV1().Secrets(p.namespace).Get(ctx, appUserSecretName, metav1.GetOptions{})
		}
		if err != nil {
			return nil, fmt.Errorf("get postgresql user secret (%s or %s): %w", adminSecretName, appUserSecretName, err)
		}
	}

	user := string(secret.Data["username"])
	if len(user) == 0 {
		user = "admin"
	}
	password := string(secret.Data["password"])
	if len(password) == 0 {
		return nil, fmt.Errorf("secret has no password")
	}

	// In-cluster host: <cluster>-rw.<namespace>.svc.cluster.local
	host := fmt.Sprintf("%s-rw.%s.svc.cluster.local", name, p.namespace)
	resourceRef := p.namespace + "/" + name

	dbName := "app"
	return &ProvisionResult{
		Host:        host,
		Port:        5432,
		User:        user,
		Password:    password,
		Database:    dbName,
		ResourceRef: resourceRef,
	}, nil
}

func (p *OperatorProvisioner) createMongoDB(ctx context.Context, name, database string, cpu, memoryMB float64, storageGB int) (*ProvisionResult, error) {
	passSecretName := name + "-admin-password"
	password := uuid.New().String()[:20]

	passSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      passSecretName,
			Namespace: p.namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(password),
		},
	}
	_, err := p.core.CoreV1().Secrets(p.namespace).Create(ctx, passSecret, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create mongo password secret: %w", err)
	}

	size := fmt.Sprintf("%dGi", storageGB)
	if size == "0Gi" {
		size = "5Gi"
	}

	mongo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mongodbcommunity.mongodb.com/v1",
			"kind":       "MongoDBCommunity",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": p.namespace,
			},
			"spec": map[string]interface{}{
				"members": 1,
				"type":    "ReplicaSet",
				"version": "7.0.0",
				"security": map[string]interface{}{
					"authentication": map[string]interface{}{
						"modes": []interface{}{"SCRAM"},
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
										"requests": map[string]interface{}{"storage": size},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err = p.dynamic.Resource(p.mongoGVR).Namespace(p.namespace).Create(ctx, mongo, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return p.getMongoConnection(ctx, name, password)
		}
		return nil, fmt.Errorf("create mongodb community: %w", err)
	}

	log.Printf("Created MongoDBCommunity %s/%s, waiting for ready", p.namespace, name)
	if err := p.waitForMongoReady(ctx, name); err != nil {
		_ = p.dynamic.Resource(p.mongoGVR).Namespace(p.namespace).Delete(ctx, name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("mongodb not ready: %w", err)
	}

	return p.getMongoConnection(ctx, name, password)
}

func (p *OperatorProvisioner) waitForMongoReady(ctx context.Context, name string) error {
	// MongoDB Community Operator creates a service; wait for pods or use status
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		obj, err := p.dynamic.Resource(p.mongoGVR).Namespace(p.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		status, _, _ := unstructured.NestedMap(obj.Object, "status")
		if status == nil {
			return false, nil
		}
		phase, _ := status["phase"].(string)
		return phase == "Running" || phase == "running", nil
	})
}

func (p *OperatorProvisioner) getMongoConnection(ctx context.Context, name, password string) (*ProvisionResult, error) {
	host := fmt.Sprintf("%s-svc.%s.svc.cluster.local", name, p.namespace)
	resourceRef := p.namespace + "/" + name
	return &ProvisionResult{
		Host:        host,
		Port:        27017,
		User:        "admin",
		Password:    password,
		Database:    "admin",
		ResourceRef: resourceRef,
	}, nil
}

// GetConnectionByResourceRef returns connection info for an existing cluster by resourceRef (namespace/name).
// Resource ref is derived from project_id via ResourceRefForProject when discovering DB in K8s.
func (p *OperatorProvisioner) GetConnectionByResourceRef(ctx context.Context, resourceRef string, dbType string) (*ProvisionResult, error) {
	parts := strings.SplitN(resourceRef, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid resource ref: expected namespace/name, got %q", resourceRef)
	}
	namespace, name := parts[0], parts[1]
	if p.namespace != namespace {
		// Provisioner is scoped to one namespace; ref from another namespace not supported
		return nil, fmt.Errorf("resource ref namespace %q does not match provisioner namespace %q", namespace, p.namespace)
	}
	switch dbType {
	case "postgres", "postgresql":
		return p.getPostgresConnectionFromCluster(ctx, name, "app")
	case "mongodb":
		passSecretName := name + "-admin-password"
		secret, err := p.core.CoreV1().Secrets(p.namespace).Get(ctx, passSecretName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get mongo password secret %s: %w", passSecretName, err)
		}
		password := string(secret.Data["password"])
		if password == "" {
			return nil, fmt.Errorf("secret %s has no password", passSecretName)
		}
		return p.getMongoConnection(ctx, name, password)
	default:
		return nil, fmt.Errorf("unsupported database type for get connection: %s", dbType)
	}
}

// DeleteInstance deletes the K8s resource (Cluster or MongoDBCommunity) identified by resourceRef (namespace/name).
func (p *OperatorProvisioner) DeleteInstance(ctx context.Context, resourceRef string) error {
	parts := strings.SplitN(resourceRef, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid resource ref: expected namespace/name, got %q", resourceRef)
	}
	namespace, name := parts[0], parts[1]

	errCNPG := p.dynamic.Resource(p.cnpgGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if errCNPG == nil || errors.IsNotFound(errCNPG) {
		return nil
	}
	errMongo := p.dynamic.Resource(p.mongoGVR).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if errMongo == nil || errors.IsNotFound(errMongo) {
		return nil
	}
	return fmt.Errorf("delete postgresql: %v; delete mongodb: %w", errCNPG, errMongo)
}

func getTierConfig() func(string) (float64, float64, int) {
	return func(tier string) (cpu float64, memoryMB float64, storageGB int) {
		switch tier {
		case "free":
			return 0.5, 512, 5
		case "basic":
			return 1.0, 1024, 10
		case "premium":
			return 2.0, 2048, 20
		default:
			return 0.5, 512, 5
		}
	}
}
