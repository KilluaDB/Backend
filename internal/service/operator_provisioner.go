package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/metrics"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

// ExternalAccess holds the external hostname and port for direct DB access via Traefik TCP SNI.
type ExternalAccess struct {
	Hostname string // e.g. db-abc123def456.postgres.db.example.com
	Port     int    // 5432 or 27017
}

// ProvisionResult holds connection info and resource ref after successful provisioning.
type ProvisionResult struct {
	DSN            string          // postgresql://user:pass@host:5432/app?sslmode=require
	ResourceRef    string          // namespace/name for deletion
	ExternalAccess *ExternalAccess // nil if EXTERNAL_DB_DOMAIN not set
	PostgRESTURL   string          // e.g. https://rest-abc123.api.example.com (empty for MongoDB)
	JWTSecret      string          // PostgREST JWT secret (empty for MongoDB)
	APIKey         string          // pre-signed JWT with role=app_user (empty for MongoDB)
}

const (
	postgresAppDBName  = "app"
	postgresAppDBOwner = "app"
)

// OperatorProvisioner creates and deletes DB instances via Kubernetes operators (CloudNativePG, MongoDB).
type OperatorProvisioner struct {
	dynamic            dynamic.Interface
	core               kubernetes.Interface
	cnpgGVR            schema.GroupVersionResource
	mongoGVR           schema.GroupVersionResource
	ingressRouteTCPGVR schema.GroupVersionResource
	externalDomain     string // EXTERNAL_DB_DOMAIN env; empty = no external access
	postgresExtPort    int    // POSTGRES_EXTERNAL_PORT (default 5432)
	mongoExtPort       int    // MONGO_EXTERNAL_PORT (default 27017)
	pollInterval      time.Duration // test-seam: poll wait interval (default 5s)
	pollTimeout       time.Duration // test-seam: poll wait timeout (default 10m)
}

// NewOperatorProvisioner creates a provisioner using in-cluster config (when running in K8s)
// or kubeconfig (KUBECONFIG env or ~/.kube/config) when running locally.
func NewOperatorProvisioner() (*OperatorProvisioner, error) {
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

	postgresExtPort := 5432
	mongoExtPort := 27017
	if v := os.Getenv("POSTGRES_EXTERNAL_PORT"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &postgresExtPort); n != 1 || err != nil {
			postgresExtPort = 5432
		}
	}
	if v := os.Getenv("MONGO_EXTERNAL_PORT"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &mongoExtPort); n != 1 || err != nil {
			mongoExtPort = 27017
		}
	}

	return &OperatorProvisioner{
		dynamic:            dyn,
		core:               core,
		cnpgGVR:            schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"},
		mongoGVR:           schema.GroupVersionResource{Group: "mongodbcommunity.mongodb.com", Version: "v1", Resource: "mongodbcommunity"},
		ingressRouteTCPGVR: schema.GroupVersionResource{Group: "traefik.containo.us", Version: "v1alpha1", Resource: "ingressroutetcps"},
		externalDomain:     os.Getenv("EXTERNAL_DB_DOMAIN"),
		postgresExtPort:    postgresExtPort,
		mongoExtPort:       mongoExtPort,
		pollInterval:         5 * time.Second,
		pollTimeout:          10 * time.Minute,
	}, nil
}

// CreateInstance provisions a DB instance (PostgreSQL or MongoDB) via the appropriate operator.
func (p *OperatorProvisioner) CreateInstance(ctx context.Context, projectID uuid.UUID, dbType string, tier string, password string) (*ProvisionResult, error) {
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
		return p.createPostgresCluster(ctx, projectID, name, cpu, memoryMB, storageGB, password, tier)
	case "mongodb":
		return p.createMongoDBCluster(ctx, projectID, name, cpu, memoryMB, storageGB, password, tier)
	default:
		metrics.ProvisioningErrorsTotal.WithLabelValues(dbType).Inc()
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// GetConnection returns live connection info for an existing database instance.
// Credentials are read from the operator-managed secret at call time and never stored.
// name is derived deterministically from projectID so no resource ref needs to be persisted.
func (p *OperatorProvisioner) GetConnection(ctx context.Context, projectID uuid.UUID, dbType string) (*ProvisionResult, error) {
	start := time.Now()
	name := p.ClusterNameForProject(projectID)

	var res *ProvisionResult
	var err error

	switch dbType {
	case "postgres", "postgresql":
		ns := p.NamespaceForProject(projectID)
		res, err = p.getPostgresConnection(ctx, ns, name)
	case "mongodb":
		ns := p.MongoNamespaceForProject(projectID)
		res, err = p.getMongoConnection(ctx, ns, name)
	default:
		err = fmt.Errorf("unsupported database type: %s", dbType)
	}

	if err != nil {
		metrics.GetConnectionErrorsTotal.WithLabelValues(dbType).Inc()
		metrics.GetConnectionDuration.WithLabelValues(dbType, "error").Observe(time.Since(start).Seconds())
		slog.Error("Failed to get connection", "project_id", projectID, "db_type", dbType, "error", err)
		return nil, err
	}
	metrics.GetConnectionDuration.WithLabelValues(dbType, "success").Observe(time.Since(start).Seconds())
	return res, nil
}

func (p *OperatorProvisioner) DeleteInstance(ctx context.Context, projectID uuid.UUID, dbType string) error {
	start := time.Now()
	if err := p.deleteIngressRouteTCP(ctx, projectID, dbType); err != nil {
		slog.Error("Failed to delete IngressRouteTCP", "project_id", projectID, "db_type", dbType, "error", err)
		metrics.SubResourceErrorsTotal.WithLabelValues(dbType, "ingress_route_tcp").Inc()
	}
	switch dbType {
	case "postgres", "postgresql":
		ns := p.NamespaceForProject(projectID)
		err := p.core.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			metrics.ProvisioningDuration.WithLabelValues("postgres", "delete", "success").Observe(time.Since(start).Seconds())
			return nil
		}
		if err != nil {
			metrics.ProvisioningDuration.WithLabelValues("postgres", "delete", "error").Observe(time.Since(start).Seconds())
			slog.Error("Failed to delete project namespace", "namespace", ns, "project_id", projectID, "db_type", dbType, "error", err)
			return fmt.Errorf("delete project namespace %s: %w", ns, err)
		}
		slog.Info("Deleted namespace (cascade)", "namespace", ns, "project_id", projectID, "db_type", "postgresql")
		metrics.InstancesDeletedTotal.WithLabelValues("postgresql").Inc()
		metrics.InstancesCurrent.WithLabelValues("postgresql").Dec()
		metrics.ProvisioningDuration.WithLabelValues("postgres", "delete", "success").Observe(time.Since(start).Seconds())
		return nil
	case "mongodb":
		ns := p.MongoNamespaceForProject(projectID)
		err := p.core.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			metrics.ProvisioningDuration.WithLabelValues("mongodb", "delete", "success").Observe(time.Since(start).Seconds())
			return nil
		}
		if err != nil {
			metrics.ProvisioningDuration.WithLabelValues("mongodb", "delete", "error").Observe(time.Since(start).Seconds())
			slog.Error("Failed to delete project namespace", "namespace", ns, "project_id", projectID, "db_type", dbType, "error", err)
			return fmt.Errorf("delete project namespace %s: %w", ns, err)
		}
		slog.Info("Deleted namespace (cascade)", "namespace", ns, "project_id", projectID, "db_type", "mongodb")
		metrics.InstancesDeletedTotal.WithLabelValues("mongodb").Inc()
		metrics.InstancesCurrent.WithLabelValues("mongodb").Dec()
		metrics.ProvisioningDuration.WithLabelValues("mongodb", "delete", "success").Observe(time.Since(start).Seconds())
		return nil
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// ClusterNameForProject returns the K8s resource name used for a project's DB (same as CreateInstance).
func (p *OperatorProvisioner) ClusterNameForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("db-%s", strings.ReplaceAll(projectID.String(), "-", ""))
}

// MongoNamespaceForProject returns the per-project K8s namespace name for MongoDB.
// Format: mongo-<32hex> (deterministic from project UUID).
func (p *OperatorProvisioner) MongoNamespaceForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("mongo-%s", strings.ReplaceAll(projectID.String(), "-", ""))
}

// mongoProjectLabels returns standard labels for MongoDB project-scoped resources.
func (p *OperatorProvisioner) mongoProjectLabels(projectID uuid.UUID) map[string]string {
	return map[string]string{
		"managed-by": "killuadb",
		"project-id": projectID.String(),
		"db-type":    "mongodb",
	}
}

// mongoProjectLabelsMap returns labels as map[string]interface{} for unstructured MongoDB resources.
func (p *OperatorProvisioner) mongoProjectLabelsMap(projectID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"managed-by": "killuadb",
		"project-id": projectID.String(),
		"db-type":    "mongodb",
	}
}

func (p *OperatorProvisioner) createPostgresCluster(ctx context.Context, projectID uuid.UUID, name string, cpu, memoryMB float64, storageGB int, password, tier string) (*ProvisionResult, error) {
	start := time.Now()
	ns := p.NamespaceForProject(projectID)

	// 1. Create per-project namespace.
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ns,
			Labels: p.projectLabels(projectID),
		},
	}
	if _, err := p.core.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return p.provisioningError("postgresql", tier, start, fmt.Errorf("create project namespace %s: %w", ns, err))
	}
	slog.Info("Created namespace", "namespace", ns, "project_id", projectID, "db_type", "postgresql")

	// 2. Create the app user secret with the provided password.
	appSecretName := name + "-app"
	appSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appSecretName,
			Namespace: ns,
			Labels:    p.projectLabels(projectID),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username": []byte(postgresAppDBOwner),
			"password": []byte(password),
			"dbname":   []byte(postgresAppDBName),
		},
	}

	_, err := p.core.CoreV1().Secrets(ns).Create(ctx, appSecret, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return p.provisioningError("postgresql", tier, start, fmt.Errorf("create app credential secret: %w", err))
	}

	// 3. Create CNPG Cluster in the per-project namespace.
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgresql.cnpg.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
				"labels":    p.projectLabelsMap(projectID),
			},
			"spec": map[string]interface{}{
				"imageName":             "ghcr.io/cloudnative-pg/postgresql:16.3",
				"imagePullPolicy":       "IfNotPresent",
				"instances":             1,
				"enableSuperuserAccess": true,
				"bootstrap": map[string]interface{}{
					"initdb": map[string]interface{}{
						"database": postgresAppDBName,
						"owner":    postgresAppDBOwner,
						"secret": map[string]interface{}{
							"name": appSecretName,
						},
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
	_, err = p.dynamic.Resource(p.cnpgGVR).Namespace(ns).Create(ctx, cluster, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			if err := p.waitForPostgresReady(ctx, ns, name); err != nil {
				return p.provisioningError("postgresql", tier, start, fmt.Errorf("existing postgres cluster not ready: %w", err))
			}
			metrics.ProvisioningDuration.WithLabelValues("postgres", "create", "success").Observe(time.Since(start).Seconds())
			return p.getPostgresConnection(ctx, ns, name)
		}
	}

	slog.Info("Created PostgreSQL cluster, waiting for ready", "namespace", ns, "name", name, "project_id", projectID, "db_type", "postgresql")
	if err := p.waitForPostgresReady(ctx, ns, name); err != nil {
		// Clean up: delete the entire namespace on failure.
		_ = p.core.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
		return p.provisioningError("postgresql", tier, start, fmt.Errorf("postgres cluster not ready: %w", err))
	}

	// 4. Get connection info.
	result, err := p.getPostgresConnection(ctx, ns, name)
	if err != nil {
		return p.provisioningError("postgresql", tier, start, err)
	}

	// 5. Setup PostgREST database roles (connects as superuser).
	authPassword, roleErr := p.SetupPostgRESTRoles(ctx, ns, name)
	if roleErr != nil {
		slog.Warn("Failed to setup PostgREST roles", "project_id", projectID, "error", roleErr)
		metrics.SubResourceErrorsTotal.WithLabelValues("postgresql", "postgrest_roles").Inc()
	} else {
		// 6. Deploy PostgREST (Deployment + Service + Secret + IngressRoute).
		pgHost := fmt.Sprintf("%s-rw.%s.svc.cluster.local", name, ns)
		jwtSecret, apiKey, pgrestErr := p.CreatePostgRESTResources(ctx, projectID, ns, pgHost, authPassword)
		if pgrestErr != nil {
			slog.Warn("Failed to deploy PostgREST", "project_id", projectID, "error", pgrestErr)
			metrics.SubResourceErrorsTotal.WithLabelValues("postgresql", "postgrest_deploy").Inc()
		} else {
			result.PostgRESTURL = p.PostgRESTURL(projectID)
			result.JWTSecret = jwtSecret
			result.APIKey = apiKey
		}
	}

	// 7. External access (Traefik TCP for direct Postgres connections).
	if p.externalDomain != "" {
		if routeErr := p.createIngressRouteTCP(ctx, projectID, "postgresql"); routeErr != nil {
			slog.Warn("Failed to create IngressRouteTCP", "name", name, "project_id", projectID, "error", routeErr)
			metrics.SubResourceErrorsTotal.WithLabelValues("postgresql", "ingress_route_tcp").Inc()
		} else {
			result.ExternalAccess = &ExternalAccess{
				Hostname: p.ExternalHostname(projectID, "postgresql"),
				Port:     p.postgresExtPort,
			}
		}
	}

	// Record provisioning success.
	metrics.InstancesCreatedTotal.WithLabelValues("postgresql", tier).Inc()
	metrics.InstancesCurrent.WithLabelValues("postgresql").Inc()
	metrics.ProvisioningDuration.WithLabelValues("postgres", "create", "success").Observe(time.Since(start).Seconds())

	return result, nil
}

func (p *OperatorProvisioner) provisioningError(dbType, tier string, start time.Time, err error) (*ProvisionResult, error) {
	metrics.ProvisioningErrorsTotal.WithLabelValues(dbType).Inc()
	dbLabel := dbType
	if dbLabel == "postgresql" {
		dbLabel = "postgres"
	}
	metrics.ProvisioningDuration.WithLabelValues(dbLabel, "create", "error").Observe(time.Since(start).Seconds())
	return nil, err
}

func (p *OperatorProvisioner) waitForPostgresReady(ctx context.Context, namespace, name string) error {
	waitStart := time.Now()
	iterations := 0
	err := wait.PollUntilContextTimeout(ctx, p.pollInterval, p.pollTimeout, true, func(ctx context.Context) (bool, error) {
		iterations++
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

		if iterations%6 == 0 || readyInstances > 0 {
			slog.Info("Postgres wait progress", "namespace", namespace, "name", name, "ready_instances", readyInstances, "target", instances)
		}

		return readyInstances >= instances && readyInstances > 0, nil
	})

	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.WaitReadyDuration.WithLabelValues("postgresql", status).Observe(time.Since(waitStart).Seconds())
	return err
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

func (p *OperatorProvisioner) createMongoDBCluster(ctx context.Context, projectID uuid.UUID, name string, cpu, memoryMB float64, storageGB int, password, tier string) (*ProvisionResult, error) {
	start := time.Now()
	ns := p.MongoNamespaceForProject(projectID)

	// 1. Create per-project namespace.
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ns,
			Labels: p.mongoProjectLabels(projectID),
		},
	}
	if _, err := p.core.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return p.provisioningError("mongodb", tier, start, fmt.Errorf("create project namespace %s: %w", ns, err))
	}
	slog.Info("Created namespace", "namespace", ns, "project_id", projectID, "db_type", "mongodb")

	// 2. Create service account required by MongoDB operator.
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mongodb-database",
			Namespace: ns,
		},
	}
	if _, err := p.core.CoreV1().ServiceAccounts(ns).Create(ctx, sa, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return p.provisioningError("mongodb", tier, start, fmt.Errorf("create mongodb-database serviceaccount: %w", err))
	}

	// 3. Bind mongodb-database SA to backend-db-manager ClusterRole.
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mongodb-database",
			Namespace: ns,
		},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "mongodb-database", Namespace: ns}},
		RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "backend-db-manager"},
	}
	if _, err := p.core.RbacV1().RoleBindings(ns).Create(ctx, roleBinding, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return p.provisioningError("mongodb", tier, start, fmt.Errorf("create mongodb-database rolebinding: %w", err))
	}

	// 4. Create the admin password secret.
	passSecretName := name + "-admin-password"
	passSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      passSecretName,
			Namespace: ns,
			Labels:    p.mongoProjectLabels(projectID),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte(password),
		},
	}
	_, err := p.core.CoreV1().Secrets(ns).Create(ctx, passSecret, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return p.provisioningError("mongodb", tier, start, fmt.Errorf("create cluster password secret: %w", err))
	}

	// 5. Create MongoDBCommunity CR in the per-project namespace.
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mongodbcommunity.mongodb.com/v1",
			"kind":       "MongoDBCommunity",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
				"labels":    p.mongoProjectLabelsMap(projectID),
			},
			"spec": map[string]interface{}{
				"members": 1,
				"type":    "ReplicaSet",
				"version": "7.0.0",
				"additionalMongodConfig": map[string]interface{}{
					"net.port": 27017,
				},
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
										"name":            "mongod",
										"imagePullPolicy": "IfNotPresent",
										"resources": map[string]interface{}{
											"limits": map[string]interface{}{
												"cpu":    fmt.Sprintf("%.1f", cpu),
												"memory": fmt.Sprintf("%.0fMi", memoryMB),
											},
										},
									},
									map[string]interface{}{
										"name":            "mongodb-agent",
										"imagePullPolicy": "IfNotPresent",
									},
									map[string]interface{}{
										"name":            "mongodb-exporter",
										"image":           "percona/mongodb_exporter:0.40",
										"imagePullPolicy": "IfNotPresent",
										"ports": []interface{}{
											map[string]interface{}{
												"name":          "metrics",
												"containerPort": 9216,
											},
										},
										"env": []interface{}{
											map[string]interface{}{
												"name":  "MONGODB_URI",
												"value": "mongodb://localhost:27017",
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
			metrics.ProvisioningDuration.WithLabelValues("mongodb", "create", "success").Observe(time.Since(start).Seconds())
			return p.getMongoConnection(ctx, ns, name)
		}
		return p.provisioningError("mongodb", tier, start, fmt.Errorf("create mongodb community: %w", err))
	}

	slog.Info("Created MongoDBCommunity, waiting for ready", "namespace", ns, "name", name, "project_id", projectID, "db_type", "mongodb")
	if err := p.waitForMongoReady(ctx, ns, name); err != nil {
		_ = p.core.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
		return p.provisioningError("mongodb", tier, start, fmt.Errorf("mongodb not ready: %w", err))
	}

	result, err := p.getMongoConnection(ctx, ns, name)
	if err != nil {
		return p.provisioningError("mongodb", tier, start, err)
	}
	if p.externalDomain != "" {
		if routeErr := p.createIngressRouteTCP(ctx, projectID, "mongodb"); routeErr != nil {
			slog.Warn("Failed to create IngressRouteTCP", "name", name, "project_id", projectID, "error", routeErr)
			metrics.SubResourceErrorsTotal.WithLabelValues("mongodb", "ingress_route_tcp").Inc()
		} else {
			result.ExternalAccess = &ExternalAccess{
				Hostname: p.ExternalHostname(projectID, "mongodb"),
				Port:     p.mongoExtPort,
			}
		}
	}

	metrics.InstancesCreatedTotal.WithLabelValues("mongodb", tier).Inc()
	metrics.InstancesCurrent.WithLabelValues("mongodb").Inc()
	metrics.ProvisioningDuration.WithLabelValues("mongodb", "create", "success").Observe(time.Since(start).Seconds())

	return result, nil
}
func (p *OperatorProvisioner) waitForMongoReady(ctx context.Context, namespace, name string) error {
	waitStart := time.Now()
	iterations := 0
	err := wait.PollUntilContextTimeout(ctx, p.pollInterval, p.pollTimeout, true, func(ctx context.Context) (bool, error) {
		iterations++
		obj, err := p.dynamic.Resource(p.mongoGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		status, _, _ := unstructured.NestedMap(obj.Object, "status")
		if status == nil {
			return false, nil
		}
		phase, _ := status["phase"].(string)
		msg, _ := status["message"].(string)
		if phase != "" && phase != "Running" && phase != "running" && msg != "" {
			if iterations%6 == 0 {
				slog.Info("MongoDB wait progress", "namespace", namespace, "name", name, "phase", phase, "message", msg)
			}
		}
		return phase == "Running" || phase == "running", nil
	})

	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.WaitReadyDuration.WithLabelValues("mongodb", status).Observe(time.Since(waitStart).Seconds())
	return err
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
		DSN:         fmt.Sprintf("mongodb://%s@%s:27017/app?authSource=admin", userInfo.String(), host),
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

// HasExternalAccess reports whether Traefik TCP SNI routing is configured.
func (p *OperatorProvisioner) HasExternalAccess() bool {
	return p.externalDomain != ""
}

// ExternalHostname returns the deterministic external hostname for a project's DB.
// Uses the full 32-char UUID (no dashes) so pgproxy can derive the CNPG service name
// without any storage or K8s API lookup.
// Format: db-{32hexchars}.postgres.{domain} or ...mongodb...
func (p *OperatorProvisioner) ExternalHostname(projectID uuid.UUID, dbType string) string {
	nodashes := strings.ReplaceAll(projectID.String(), "-", "")
	sub := "postgres"
	if dbType == "mongodb" {
		sub = "mongodb"
	}
	return fmt.Sprintf("db-%s.%s.%s", nodashes, sub, p.externalDomain)
}

// ExternalPort returns the external port for the given DB type.
func (p *OperatorProvisioner) ExternalPort(dbType string) int {
	if dbType == "mongodb" {
		return p.mongoExtPort
	}
	return p.postgresExtPort
}

func (p *OperatorProvisioner) createIngressRouteTCP(ctx context.Context, projectID uuid.UUID, dbType string) error {
	if p.externalDomain == "" {
		return nil
	}
	// PostgreSQL is handled by the pgproxy catch-all (HostSNI(*)).
	// Only MongoDB needs per-project IngressRouteTCP (TLS passthrough works for MongoDB).
	switch dbType {
	case "postgres", "postgresql":
		return nil
	}

	name := p.ClusterNameForProject(projectID)
	hostname := p.ExternalHostname(projectID, dbType)

	var ns, entryPoint, svcName string
	var port int64
	switch dbType {
	case "mongodb":
		ns = p.MongoNamespaceForProject(projectID)
		entryPoint = "mongodb"
		svcName = name + "-svc"
		port = 27017
	default:
		return fmt.Errorf("unsupported db type: %s", dbType)
	}

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.containo.us/v1alpha1",
			"kind":       "IngressRouteTCP",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"entryPoints": []interface{}{entryPoint},
				"routes": []interface{}{
					map[string]interface{}{
						"match": fmt.Sprintf("HostSNI(`%s`)", hostname),
						"services": []interface{}{
							map[string]interface{}{
								"name": svcName,
								"port": port,
							},
						},
					},
				},
				"tls": map[string]interface{}{
					"passthrough": true,
				},
			},
		},
	}

	_, err := p.dynamic.Resource(p.ingressRouteTCPGVR).Namespace(ns).Create(ctx, route, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create IngressRouteTCP: %w", err)
	}
	return nil
}

func (p *OperatorProvisioner) deleteIngressRouteTCP(ctx context.Context, projectID uuid.UUID, dbType string) error {
	if p.externalDomain == "" {
		return nil
	}
	// PostgreSQL uses pgproxy catch-all — no per-project IngressRouteTCP.
	if dbType == "postgres" || dbType == "postgresql" {
		return nil
	}
	name := p.ClusterNameForProject(projectID)
	ns := p.MongoNamespaceForProject(projectID)
	err := p.dynamic.Resource(p.ingressRouteTCPGVR).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete IngressRouteTCP: %w", err)
	}
	return nil
}

// PeriodicMetricsCollector runs every interval to refresh K8s-level instance count metrics.
// Must be called as a goroutine.
func (p *OperatorProvisioner) PeriodicMetricsCollector(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	p.collectK8sMetrics(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.collectK8sMetrics(ctx)
		}
	}
}

func (p *OperatorProvisioner) collectK8sMetrics(ctx context.Context) {
	pgList, err := p.dynamic.Resource(p.cnpgGVR).List(ctx, metav1.ListOptions{})
	if err == nil {
		perNS := map[string]int{}
		for _, item := range pgList.Items {
			ns := item.GetNamespace()
			perNS[ns]++
		}
		for ns, count := range perNS {
			metrics.K8sInstanceCount.WithLabelValues("postgresql", ns).Set(float64(count))
		}
	} else {
		slog.Error("Failed to list CNPG clusters for metrics", "error", err)
		metrics.MetricsCollectionErrorsTotal.WithLabelValues("postgresql").Inc()
	}

	mongoList, err := p.dynamic.Resource(p.mongoGVR).List(ctx, metav1.ListOptions{})
	if err == nil {
		perNS := map[string]int{}
		for _, item := range mongoList.Items {
			ns := item.GetNamespace()
			perNS[ns]++
		}
		for ns, count := range perNS {
			metrics.K8sInstanceCount.WithLabelValues("mongodb", ns).Set(float64(count))
		}
	} else {
		slog.Error("Failed to list MongoDBCommunity CRDs for metrics", "error", err)
		metrics.MetricsCollectionErrorsTotal.WithLabelValues("mongodb").Inc()
	}

	p.updateInstanceCountFromK8s(pgList, mongoList)
}

func (p *OperatorProvisioner) updateInstanceCountFromK8s(pgList, mongoList *unstructured.UnstructuredList) {
	var pgCount, mongoCount float64
	if pgList != nil {
		for _, item := range pgList.Items {
			status, _, _ := unstructured.NestedMap(item.Object, "status")
			if status == nil {
				continue
			}
			phase, _ := status["phase"].(string)
			if phase == "Cluster in healthy state" || phase == "" || phase == "Ready" {
				pgCount++
			}
		}
	}
	if mongoList != nil {
		for _, item := range mongoList.Items {
			status, _, _ := unstructured.NestedMap(item.Object, "status")
			if status == nil {
				continue
			}
			phase, _ := status["phase"].(string)
			if phase == "Running" || phase == "running" || phase == "" {
				mongoCount++
			}
		}
	}
	metrics.InstancesCurrent.WithLabelValues("postgresql").Set(pgCount)
	metrics.InstancesCurrent.WithLabelValues("mongodb").Set(mongoCount)
}


