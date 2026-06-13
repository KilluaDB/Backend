package service

import (
	"backend/internal/metrics"
	"backend/internal/utils"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	postgrestImage       = "postgrest/postgrest:v12.2.3" // pinned; never use :latest in a provisioner
	postgrestPort        = 3000
	postgrestDBSchema    = "public"
	postgrestAnonRole    = "anon"
	postgrestAppUserRole = "app_user"
	postgrestAuthRole    = "authenticator"
)

// ingressRouteGVR is the GVR for Traefik HTTP IngressRoute objects.
// Distinct from ingressRouteTCPGVR — do not alias or mutate that one.
var ingressRouteGVR = schema.GroupVersionResource{
	Group:    "traefik.containo.us",
	Version:  "v1alpha1",
	Resource: "ingressroutes",
}

// NamespaceForProject returns the per-project K8s namespace name.
// Format: pg-<32hex> (deterministic from project UUID).
func (p *OperatorProvisioner) NamespaceForProject(projectID uuid.UUID) string {
	return fmt.Sprintf("pg-%s", strings.ReplaceAll(projectID.String(), "-", ""))
}

// postgrestDeploymentName returns the PostgREST deployment name for a project.
func postgrestDeploymentName(projectID uuid.UUID) string {
	return fmt.Sprintf("postgrest-%s", strings.ReplaceAll(projectID.String(), "-", ""))
}

// postgrestSecretName returns the PostgREST config secret name for a project.
func postgrestSecretName(projectID uuid.UUID) string {
	return fmt.Sprintf("postgrest-%s-cfg", strings.ReplaceAll(projectID.String(), "-", ""))
}

// postgrestServiceName returns the PostgREST service name for a project.
func postgrestServiceName(projectID uuid.UUID) string {
	return fmt.Sprintf("postgrest-%s-svc", strings.ReplaceAll(projectID.String(), "-", ""))
}

// PostgRESTURL returns the external URL for a project's PostgREST API.
// Returns empty string if external access is not configured.
// HTTP only — no cert-manager configured for local dev.
func (p *OperatorProvisioner) PostgRESTURL(projectID uuid.UUID) string {
	if p.externalDomain == "" {
		return ""
	}
	id := strings.ReplaceAll(projectID.String(), "-", "")
	return fmt.Sprintf("http://rest-%s.%s", id, p.externalDomain)
}

// SetupPostgRESTRoles connects to the project's database as the CNPG superuser
// and creates the PostgreSQL roles needed by PostgREST:
//   - authenticator: LOGIN, NOINHERIT — PostgREST connects as this role
//   - anon: NOLOGIN — unauthenticated requests (gets NO permissions)
//   - app_user: NOLOGIN — authenticated requests (gets full CRUD on public schema)
//
// Must connect as superuser because only superusers can CREATE ROLE.
// Returns the generated authenticator password.
func (p *OperatorProvisioner) SetupPostgRESTRoles(ctx context.Context, namespace, clusterName string) (authenticatorPassword string, err error) {
	if p.skipPostgRESTSetup {
		return "test-auth-pw", nil
	}
	authenticatorPassword, err = utils.GeneratePasswordBase64(32)
	if err != nil {
		return "", fmt.Errorf("generate authenticator password: %w", err)
	}

	// Read the CNPG superuser secret (<cluster>-superuser) to get postgres credentials.
	// The CNPG operator creates this secret asynchronously after the cluster is marked ready,
	// so we poll until it appears (up to 2 minutes).
	superSecretName := clusterName + "-superuser"
	var superSecret *corev1.Secret
	if err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		s, getErr := p.core.CoreV1().Secrets(namespace).Get(ctx, superSecretName, metav1.GetOptions{})
		if getErr != nil {
			if errors.IsNotFound(getErr) {
				return false, nil // keep polling
			}
			return false, getErr // unexpected error, stop
		}
		superSecret = s
		return true, nil
	}); err != nil {
		return "", fmt.Errorf("waiting for superuser secret %s: %w", superSecretName, err)
	}

	superUser := string(superSecret.Data["username"])
	if superUser == "" {
		superUser = "postgres"
	}
	superPass := string(superSecret.Data["password"])
	if superPass == "" {
		return "", fmt.Errorf("superuser secret %s has no password", superSecretName)
	}

	// Build DSN using the superuser credentials.
	pgHost := fmt.Sprintf("%s-rw.%s.svc.cluster.local", clusterName, namespace)
	userInfo := url.UserPassword(superUser, superPass)
	superDSN := fmt.Sprintf("postgresql://%s@%s:5432/%s?sslmode=require", userInfo.String(), pgHost, postgresAppDBName)

	pool, err := pgxpool.New(ctx, superDSN)
	if err != nil {
		return "", fmt.Errorf("connect to project DB as superuser: %w", err)
	}
	defer pool.Close()

	// Use a single connection so all statements run in the same session.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire connection for role setup: %w", err)
	}
	defer conn.Release()

	// Wrap in an explicit transaction so a failure in any statement rolls back
	// all preceding statements — avoids half-applied role state.
	//
	// NOTE: authenticatorPassword is base64-generated (URL-safe alphabet only),
	// so interpolation is safe here. If this function is ever called with an
	// externally-supplied password, switch to pgx parameterized queries via
	// pgx/v5's QuoteLiteral helper to prevent SQL injection.
	roleSQL := fmt.Sprintf(`
BEGIN;

-- PostgREST authenticator role (the role PostgREST uses to connect).
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[1]s') THEN
    CREATE ROLE %[1]s NOINHERIT LOGIN PASSWORD '%[2]s';
  ELSE
    ALTER ROLE %[1]s WITH PASSWORD '%[2]s';
  END IF;
END $$;

-- Anonymous role (unauthenticated requests) — NO permissions.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[3]s') THEN
    CREATE ROLE %[3]s NOLOGIN;
  END IF;
END $$;

-- Authenticated role — full CRUD on public schema.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[4]s') THEN
    CREATE ROLE %[4]s NOLOGIN;
  END IF;
END $$;

-- Allow authenticator to switch into anon and app_user.
GRANT %[3]s TO %[1]s;
GRANT %[4]s TO %[1]s;

-- app_user permissions: full CRUD on public schema.
GRANT USAGE ON SCHEMA public TO %[4]s;
GRANT ALL ON ALL TABLES IN SCHEMA public TO %[4]s;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO %[4]s;
ALTER DEFAULT PRIVILEGES FOR ROLE %[5]s IN SCHEMA public GRANT ALL ON TABLES TO %[4]s;
ALTER DEFAULT PRIVILEGES FOR ROLE %[5]s IN SCHEMA public GRANT ALL ON SEQUENCES TO %[4]s;

COMMIT;
`,
		postgrestAuthRole,     // %[1]s
		authenticatorPassword, // %[2]s
		postgrestAnonRole,     // %[3]s
		postgrestAppUserRole,  // %[4]s
		postgresAppDBOwner,    // %[5]s
	)

	if _, err := conn.Exec(ctx, roleSQL); err != nil {
		return "", fmt.Errorf("create PostgREST roles: %w", err)
	}

	slog.Info("PostgREST roles created/updated in project database", "namespace", namespace)
	return authenticatorPassword, nil
}

// CreatePostgRESTResources creates all K8s resources for PostgREST in the project namespace:
//  1. Secret with PGRST_DB_URI, PGRST_JWT_SECRET, and pre-signed api-key
//  2. Deployment running postgrest at a pinned version
//  3. ClusterIP Service exposing port 3000
//  4. Traefik HTTP IngressRoute (if external domain is configured)
//
// Returns the jwtSecret and pre-signed apiKey.
// On partial failure (e.g. Service create fails after Secret+Deployment succeed),
// resources are left in place — re-running is idempotent via IsAlreadyExists checks.
func (p *OperatorProvisioner) CreatePostgRESTResources(
	ctx context.Context,
	projectID uuid.UUID,
	namespace string,
	pgHost string,
	authenticatorPassword string,
) (jwtSecret string, apiKey string, err error) {

	// Generate a 64-byte random JWT secret.
	jwtSecret, err = utils.GeneratePasswordBase64(64)
	if err != nil {
		return "", "", fmt.Errorf("generate JWT secret: %w", err)
	}

	// Sign a service-role API key (no expiration).
	apiKey, err = signPostgRESTAPIKey(jwtSecret)
	if err != nil {
		return "", "", fmt.Errorf("sign API key: %w", err)
	}

	// Build the PostgREST connection URI using the authenticator role.
	// sslmode=require matches the superuser DSN — CNPG enforces TLS by default.
	userInfo := url.UserPassword(postgrestAuthRole, authenticatorPassword)
	dbURI := fmt.Sprintf("postgresql://%s@%s:5432/%s?sslmode=require", userInfo.String(), pgHost, postgresAppDBName)

	secretName := postgrestSecretName(projectID)
	deployName := postgrestDeploymentName(projectID)
	svcName := postgrestServiceName(projectID)

	// 1. Create config secret.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    p.projectLabels(projectID),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"db-uri":     []byte(dbURI),
			"jwt-secret": []byte(jwtSecret),
			"api-key":    []byte(apiKey),
		},
	}
	if _, err := p.core.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return "", "", fmt.Errorf("create PostgREST secret: %w", err)
	}

	// 2. Create deployment.
	replicas := int32(1)
	labels := map[string]string{
		"app":        "postgrest",
		"project-id": projectID.String(),
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: namespace,
			Labels:    p.projectLabels(projectID),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgrest",
							Image: postgrestImage,
							Ports: []corev1.ContainerPort{
								{ContainerPort: int32(postgrestPort), Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{
									Name: "PGRST_DB_URI",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
											Key:                  "db-uri",
										},
									},
								},
								{
									Name: "PGRST_JWT_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
											Key:                  "jwt-secret",
										},
									},
								},
								{Name: "PGRST_DB_SCHEMAS", Value: postgrestDBSchema},
								{Name: "PGRST_DB_ANON_ROLE", Value: postgrestAnonRole},
								{Name: "PGRST_SERVER_PORT", Value: fmt.Sprintf("%d", postgrestPort)},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("50m"),
									corev1.ResourceMemory: mustParseQuantity("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("200m"),
									corev1.ResourceMemory: mustParseQuantity("128Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := p.core.AppsV1().Deployments(namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return "", "", fmt.Errorf("create PostgREST deployment: %w", err)
	}

	// 3. Create service.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
			Labels:    p.projectLabels(projectID),
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Port:       int32(postgrestPort),
					TargetPort: intstr.FromInt(postgrestPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if _, err := p.core.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return "", "", fmt.Errorf("create PostgREST service: %w", err)
	}

	// 4. Create Traefik HTTP IngressRoute (if external domain is set).
	// HTTPS IngressRoute is intentionally omitted: no cert-manager is configured,
	// and an empty tls:{} block causes Traefik to serve its internal self-signed
	// cert which fails x509 verification on all clients.
	if p.externalDomain != "" {
		if err := p.createPostgRESTIngressRoute(ctx, projectID, namespace, svcName); err != nil {
			slog.Warn("Failed to create PostgREST IngressRoute", "project_id", projectID, "error", err)
			metrics.SubResourceErrorsTotal.WithLabelValues("postgresql", "postgrest_ingress").Inc()
		}
	}

	slog.Info("PostgREST resources created", "namespace", namespace, "project_id", projectID)
	return jwtSecret, apiKey, nil
}

// GetPostgRESTCredentials reads the PostgREST JWT secret and API key from K8s.
func (p *OperatorProvisioner) GetPostgRESTCredentials(ctx context.Context, projectID uuid.UUID) (jwtSecret, apiKey string, err error) {
	ns := p.NamespaceForProject(projectID)
	secretName := postgrestSecretName(projectID)

	secret, err := p.core.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("get PostgREST secret: %w", err)
	}

	return string(secret.Data["jwt-secret"]), string(secret.Data["api-key"]), nil
}

// createPostgRESTIngressRoute creates an HTTP-only Traefik IngressRoute for PostgREST.
// HTTPS is deliberately excluded — use cert-manager + a certResolver when TLS is needed.
// Route: Host(`rest-<projectID>.<externalDomain>`) → postgrest-<id>-svc:3000
func (p *OperatorProvisioner) createPostgRESTIngressRoute(ctx context.Context, projectID uuid.UUID, namespace, svcName string) error {
	id := strings.ReplaceAll(projectID.String(), "-", "")
	hostname := fmt.Sprintf("rest-%s.%s", id, p.externalDomain)

	httpRoute := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "traefik.containo.us/v1alpha1",
			"kind":       "IngressRoute",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("postgrest-%s-http", id),
				"namespace": namespace,
				"labels":    p.projectLabelsMap(projectID),
			},
			"spec": map[string]interface{}{
				"entryPoints": []interface{}{"web"},
				"routes": []interface{}{
					map[string]interface{}{
						"match": fmt.Sprintf("Host(`%s`)", hostname),
						"kind":  "Rule",
						"services": []interface{}{
							map[string]interface{}{
								"name": svcName,
								"port": int64(postgrestPort),
							},
						},
					},
				},
			},
		},
	}

	_, err := p.dynamic.Resource(ingressRouteGVR).Namespace(namespace).Create(ctx, httpRoute, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("create HTTP PostgREST IngressRoute: %w", err)
	}

	return nil
}

// signPostgRESTAPIKey signs a JWT with role=app_user and no expiration.
// iat (issued-at) is included for standard compliance and middleware compatibility.
// This is the pre-signed "service key" users can use immediately.
func signPostgRESTAPIKey(jwtSecret string) (string, error) {
	claims := jwt.MapClaims{
		"role": postgrestAppUserRole,
		"iat":  time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

// projectLabels returns standard labels for project-scoped K8s resources.
func (p *OperatorProvisioner) projectLabels(projectID uuid.UUID) map[string]string {
	return map[string]string{
		"managed-by": "killuadb",
		"project-id": projectID.String(),
		"db-type":    "postgresql",
	}
}

// projectLabelsMap returns labels as map[string]interface{} for unstructured resources.
func (p *OperatorProvisioner) projectLabelsMap(projectID uuid.UUID) map[string]interface{} {
	return map[string]interface{}{
		"managed-by": "killuadb",
		"project-id": projectID.String(),
		"db-type":    "postgresql",
	}
}

// mustParseQuantity parses a K8s resource quantity string or panics.
// All callers use compile-time string literals — a panic here is a programming
// error caught at startup, not a runtime condition.
func mustParseQuantity(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		panic(fmt.Sprintf("invalid resource quantity %q: %v", s, err))
	}
	return q
}
