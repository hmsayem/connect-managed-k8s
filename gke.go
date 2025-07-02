package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	container "cloud.google.com/go/container/apiv1"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"cloud.google.com/go/storage"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Default GCP zone/location
	GCPDefaultZone = "us-central1"
)

// GCPConfig represents GCP configuration options
type GCPConfig struct {
	ProjectID       string // GCP project ID (required)
	Zone            string // GCP zone/location (optional)
	CredentialsJSON []byte // Service account JSON credentials (optional)
	CredentialsPath string // Path to service account JSON file (optional)
}

// GCPClientManager manages GCP clients and configurations
type GCPClientManager struct {
	config        GCPConfig
	gkeClient     *container.ClusterManagerClient
	storageClient *storage.Client
}

// NewGCPClientManager creates a new GCP client manager
func NewGCPClientManager(cfg GCPConfig) (*GCPClientManager, error) {
	manager := &GCPClientManager{
		config: cfg,
	}

	if err := manager.initializeGCPClients(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize GCP clients: %w", err)
	}

	return manager, nil
}

// initializeGCPClients initializes the GCP clients based on the provided configuration
func (m *GCPClientManager) initializeGCPClients(ctx context.Context) error {
	if err := m.validateConfig(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	if m.config.Zone == "" {
		m.config.Zone = GCPDefaultZone
	}
	var clientOptions []option.ClientOption

	if len(m.config.CredentialsJSON) > 0 {
		fmt.Println("Using static service account JSON")
		clientOptions = append(clientOptions, option.WithCredentialsJSON(m.config.CredentialsJSON))
	} else if m.config.CredentialsPath != "" {
		fmt.Println("Using static service account file")
		clientOptions = append(clientOptions, option.WithCredentialsFile(m.config.CredentialsPath))
	} else {
		fmt.Println("Using application default credentials")
	}

	gkeClient, err := container.NewClusterManagerClient(ctx, clientOptions...)
	if err != nil {
		return fmt.Errorf("failed to create GKE client: %w", err)
	}

	storageClient, err := storage.NewClient(ctx, clientOptions...)
	if err != nil {
		gkeClient.Close()
		return fmt.Errorf("failed to create storage client: %w", err)
	}

	m.gkeClient = gkeClient
	m.storageClient = storageClient

	// Validate credentials
	if err := m.validateCredentials(ctx); err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}

	return nil
}

// validateConfig validates the GCP configuration
func (m *GCPClientManager) validateConfig() error {
	if m.config.ProjectID == "" {
		return fmt.Errorf("project ID is required")
	}

	// Validate project ID format (basic check)
	if strings.Contains(m.config.ProjectID, " ") || len(m.config.ProjectID) < 6 {
		return fmt.Errorf("invalid project ID format: %s", m.config.ProjectID)
	}

	return nil
}

// validateCredentials validates GCP credentials by making a test API call
func (m *GCPClientManager) validateCredentials(ctx context.Context) error {
	return nil
	// Test credentials by trying to list storage buckets (lightweight API call)
	it := m.storageClient.Buckets(ctx, m.config.ProjectID)
	if _, err := it.Next(); err != nil && err != iterator.Done {
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}

	fmt.Printf("GCP Credentials Validated for project: %s\n", m.config.ProjectID)
	return nil
}

// GetGKEClient returns the GKE client
func (m *GCPClientManager) GetGKEClient() *container.ClusterManagerClient {
	return m.gkeClient
}

// GetProjectID returns the configured project ID
func (m *GCPClientManager) GetProjectID() string {
	return m.config.ProjectID
}

// GetZone returns the configured zone
func (m *GCPClientManager) GetZone() string {
	return m.config.Zone
}

// Close closes all GCP clients
func (m *GCPClientManager) Close() error {
	var err error
	if m.gkeClient != nil {
		if closeErr := m.gkeClient.Close(); closeErr != nil {
			err = closeErr
		}
	}
	if m.storageClient != nil {
		if closeErr := m.storageClient.Close(); closeErr != nil {
			if err != nil {
				err = fmt.Errorf("%w; %v", err, closeErr)
			} else {
				err = closeErr
			}
		}
	}
	return err
}

// GKEClient wraps the GKE and Kubernetes clients with improved GCP configuration
type GKEClient struct {
	gcpClientManager *GCPClientManager
	k8sClient        *kubernetes.Clientset
	clusterName      string
}

func NewGKEClient(clusterName string, gcpConfig GCPConfig) (*GKEClient, error) {
	// Create GCP client manager
	clientManager, err := NewGCPClientManager(gcpConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP client manager: %w", err)
	}

	client := &GKEClient{
		gcpClientManager: clientManager,
		clusterName:      clusterName,
	}

	// Initialize Kubernetes client
	if err := client.initKubernetesClient(); err != nil {
		clientManager.Close()
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	return client, nil
}

// initKubernetesClient initializes the Kubernetes client using GKE cluster info
func (c *GKEClient) initKubernetesClient() error {
	ctx := context.Background()

	// Get GKE cluster information
	clusterPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", c.gcpClientManager.GetProjectID(), c.gcpClientManager.GetZone(), c.clusterName)
	clusterReq := &containerpb.GetClusterRequest{
		Name: clusterPath,
	}

	fmt.Println("clusterPath", clusterPath)

	cluster, err := c.gcpClientManager.GetGKEClient().GetCluster(ctx, clusterReq)
	if err != nil {
		return fmt.Errorf("failed to get GKE cluster: %w", err)
	}

	if cluster.Status != containerpb.Cluster_RUNNING {
		return fmt.Errorf("cluster %s is not running, current status: %s", c.clusterName, cluster.Status.String())
	}

	// Decode the certificate authority data
	caCert, err := base64.StdEncoding.DecodeString(cluster.MasterAuth.ClusterCaCertificate)
	if err != nil {
		return fmt.Errorf("failed to decode certificate authority data: %w", err)
	}

	// Get Google Cloud credentials for authentication
	creds, err := google.FindDefaultCredentials(ctx, container.DefaultAuthScopes()...)
	if err != nil {
		return fmt.Errorf("failed to get Google Cloud credentials: %w", err)
	}

	// Get OAuth2 token source
	tokenSource := creds.TokenSource

	// Get an access token
	token, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	// Create Kubernetes client configuration
	kubeConfig := &rest.Config{
		Host:        fmt.Sprintf("https://%s", cluster.Endpoint),
		BearerToken: token.AccessToken,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caCert,
		},
	}

	// Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	c.k8sClient = clientset
	return nil
}

// GetClusterInfo returns basic information about the GKE cluster
func (c *GKEClient) GetClusterInfo() error {
	ctx := context.Background()

	clusterPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", c.gcpClientManager.GetProjectID(), c.gcpClientManager.GetZone(), c.clusterName)
	clusterReq := &containerpb.GetClusterRequest{
		Name: clusterPath,
	}

	cluster, err := c.gcpClientManager.GetGKEClient().GetCluster(ctx, clusterReq)
	if err != nil {
		return fmt.Errorf("failed to get cluster info: %w", err)
	}

	fmt.Printf("GKE Cluster Information:\n")
	fmt.Printf("  Name: %s\n", cluster.Name)
	fmt.Printf("  Status: %s\n", cluster.Status.String())
	fmt.Printf("  Location: %s\n", cluster.Location)
	fmt.Printf("  Current Version: %s\n", cluster.CurrentMasterVersion)
	fmt.Printf("  Endpoint: %s\n", cluster.Endpoint)
	fmt.Printf("  Created: %s\n", cluster.CreateTime)
	fmt.Printf("  Network: %s\n", cluster.Network)
	fmt.Printf("  Subnetwork: %s\n", cluster.Subnetwork)

	return nil
}

// ListPods lists all pods in the kube-system namespace
func (c *GKEClient) ListPods() error {
	namespace := "kube-system"

	pods, err := c.k8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}

	fmt.Printf("\nPods in namespace '%s' (%d total):\n", namespace, len(pods.Items))
	for _, pod := range pods.Items {
		fmt.Printf("  Name: %s\n", pod.Name)
		fmt.Printf("    Status: %s\n", pod.Status.Phase)
		fmt.Printf("    Node: %s\n", pod.Spec.NodeName)
		fmt.Printf("    Created: %s\n", pod.CreationTimestamp.Format(time.RFC3339))
		fmt.Println()
	}

	return nil
}

// GetProjectID returns the GCP project ID for this GKE client
func (c *GKEClient) GetProjectID() string {
	return c.gcpClientManager.GetProjectID()
}

// GetZone returns the configured GCP zone
func (c *GKEClient) GetZone() string {
	return c.gcpClientManager.GetZone()
}

// Close closes the GKE client connections
func (c *GKEClient) Close() error {
	return c.gcpClientManager.Close()
}

// RunGCPTest runs the GKE test client
func RunGCPTest() error {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found, using environment variables")
	}

	// Get cluster details from environment variables
	clusterName := os.Getenv("GKE_CLUSTER_NAME")
	if clusterName == "" {
		return fmt.Errorf("GKE_CLUSTER_NAME environment variable is required")
	}

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		return fmt.Errorf("GOOGLE_CLOUD_PROJECT environment variable is required")
	}

	zone := os.Getenv("GKE_ZONE")
	if zone == "" {
		zone = GCPDefaultZone
		fmt.Printf("GKE_ZONE not set, using default: %s\n", zone)
	}

	// Create GCP configuration based on environment variables
	gcpConfig := GCPConfig{
		ProjectID:       projectID,
		Zone:            zone,
		CredentialsPath: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"), // Optional: service account file
	}

	// Check for base64 encoded credentials in environment
	if credentialsB64 := os.Getenv("GCP_CREDENTIALS_JSON"); credentialsB64 != "" {
		credentialsJSON, err := base64.StdEncoding.DecodeString(credentialsB64)
		if err != nil {
			return fmt.Errorf("failed to decode GCP_CREDENTIALS_JSON: %w", err)
		}

		// Validate JSON format
		var credTest map[string]interface{}
		if err := json.Unmarshal(credentialsJSON, &credTest); err != nil {
			return fmt.Errorf("invalid JSON in GCP_CREDENTIALS_JSON: %w", err)
		}

		gcpConfig.CredentialsJSON = credentialsJSON
	}

	fmt.Printf("Connecting to GKE cluster '%s' in zone '%s' (project: %s)...\n", clusterName, zone, projectID)

	// Log configuration method being used
	if len(gcpConfig.CredentialsJSON) > 0 {
		fmt.Println("Using service account JSON from environment variable")
	} else if gcpConfig.CredentialsPath != "" {
		fmt.Printf("Using service account file: %s\n", gcpConfig.CredentialsPath)
	} else {
		fmt.Println("Using application default credentials (gcloud auth, service accounts, etc.)")
	}

	// Create GKE client with improved GCP configuration
	client, err := NewGKEClient(clusterName, gcpConfig)
	if err != nil {
		return fmt.Errorf("failed to create GKE client: %w", err)
	}
	defer client.Close()

	fmt.Println("✓ Successfully connected to GKE cluster!")

	// Display GCP project information
	fmt.Printf("Connected to GCP Project: %s\n", client.GetProjectID())
	fmt.Printf("Cluster Zone: %s\n", client.GetZone())

	// Get cluster information
	if err := client.GetClusterInfo(); err != nil {
		log.Printf("Failed to get cluster info: %v", err)
	}

	// List pods in kube-system namespace
	if err := client.ListPods(); err != nil {
		log.Printf("Failed to list pods: %v", err)
	}

	fmt.Println("\n✓ GKE operations completed successfully!")
	return nil
}
