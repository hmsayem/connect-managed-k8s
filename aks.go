package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// AKSClient wraps the AKS and Kubernetes clients
type AKSClient struct {
	aksClient      *armcontainerservice.ManagedClustersClient
	k8sClient      *kubernetes.Clientset
	clusterName    string
	resourceGroup  string
	subscriptionID string
	credential     azcore.TokenCredential
}

// NewAKSClient creates a new AKS client
func NewAKSClient(clusterName, resourceGroup, subscriptionID string) (*AKSClient, error) {
	// Create Azure credential
	cred, err := createAzureCredential()
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Create AKS client
	aksClient, err := armcontainerservice.NewManagedClustersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create AKS client: %w", err)
	}

	client := &AKSClient{
		aksClient:      aksClient,
		clusterName:    clusterName,
		resourceGroup:  resourceGroup,
		subscriptionID: subscriptionID,
		credential:     cred,
	}

	// Initialize Kubernetes client
	if err := client.initKubernetesClient(); err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	return client, nil
}

// createAzureCredential creates Azure credentials using various authentication methods
func createAzureCredential() (azcore.TokenCredential, error) {
	// Try different credential types in order of preference

	// 1. Try Service Principal (if environment variables are set)
	clientID := os.Getenv("AZURE_CLIENT_ID")
	clientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	tenantID := os.Getenv("AZURE_TENANT_ID")

	if clientID != "" && clientSecret != "" && tenantID != "" {
		fmt.Println("Using Azure Service Principal authentication")
		cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create service principal credential: %w", err)
		}
		return cred, nil
	}

	// 2. Try Managed Identity (when running in Azure)
	if os.Getenv("AZURE_USE_MSI") == "true" {
		fmt.Println("Using Azure Managed Identity authentication")
		cred, err := azidentity.NewManagedIdentityCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create managed identity credential: %w", err)
		}
		return cred, nil
	}

	// 3. Try Azure CLI credentials (default)
	fmt.Println("Using Azure CLI authentication")
	cred, err := azidentity.NewAzureCLICredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure CLI credential: %w", err)
	}

	return cred, nil
}

// initKubernetesClientWithAzureAD initializes the Kubernetes client using Azure AD authentication
func (c *AKSClient) initKubernetesClientWithAzureAD(cluster armcontainerservice.ManagedClustersClientGetResponse) error {
	if cluster.Properties == nil || cluster.Properties.Fqdn == nil {
		return fmt.Errorf("cluster FQDN is not available")
	}

	// Get Azure AD token for Kubernetes API
	token, err := c.getAzureADToken()
	if err != nil {
		return fmt.Errorf("failed to get Azure AD token: %w", err)
	}

	// Get CA certificate data from cluster
	caCertData, err := c.getClusterCACertificate()
	if err != nil {
		return fmt.Errorf("failed to get CA certificate: %w", err)
	}

	// Create Kubernetes client configuration with Azure AD token and CA certificate
	kubeConfig := &rest.Config{
		Host:        fmt.Sprintf("https://%s", *cluster.Properties.Fqdn),
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:   caCertData,
			Insecure: false, // Use secure TLS verification with CA certificate
		},
	}

	// Create Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	c.k8sClient = clientset
	fmt.Println("Successfully connected using Azure AD token authentication (secure)")
	return nil
}

// getAzureADToken gets an Azure AD token for Kubernetes API access
func (c *AKSClient) getAzureADToken() (string, error) {
	// Use the same credential that we used for the AKS client
	ctx := context.Background()

	// Get token for Kubernetes API (using the standard AKS server application scope)
	token, err := c.credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"6dae42f8-4368-4678-94ff-3960e28e3630/.default"}, // Azure Kubernetes Service scope
	})
	if err != nil {
		return "", fmt.Errorf("failed to get Azure AD token: %w", err)
	}

	return token.Token, nil
}

// getClusterCACertificate extracts the CA certificate from the AKS cluster
func (c *AKSClient) getClusterCACertificate() ([]byte, error) {
	// If admin credentials fail, try user credentials
	userCredResult, err := c.aksClient.ListClusterUserCredentials(context.Background(), c.resourceGroup, c.clusterName, nil)
	if err == nil && len(userCredResult.Kubeconfigs) > 0 && userCredResult.Kubeconfigs[0].Value != nil {
		caCert, err := c.extractCACertFromKubeconfig(userCredResult.Kubeconfigs[0].Value)
		if err == nil {
			return caCert, nil
		}
	}
	return nil, fmt.Errorf("no CA certificate found in cluster credentials")
}

// extractCACertFromKubeconfig extracts CA certificate data from kubeconfig
func (c *AKSClient) extractCACertFromKubeconfig(kubeconfigData []byte) ([]byte, error) {
	// Extract CA data from kubeconfig using clientcmd
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	// Find the first cluster and extract its CA data
	for _, cluster := range config.Clusters {
		if len(cluster.CertificateAuthorityData) > 0 {
			return cluster.CertificateAuthorityData, nil
		}
	}

	return nil, fmt.Errorf("no CA certificate found in kubeconfig")
}

// initKubernetesClient initializes the Kubernetes client using AKS cluster info
func (c *AKSClient) initKubernetesClient() error {
	ctx := context.Background()

	// Get AKS cluster information
	cluster, err := c.aksClient.Get(ctx, c.resourceGroup, c.clusterName, nil)
	if err != nil {
		return fmt.Errorf("failed to get AKS cluster: %w", err)
	}

	if cluster.Properties == nil {
		return fmt.Errorf("cluster properties are nil")
	}

	// Check cluster status
	if cluster.Properties.PowerState == nil || cluster.Properties.PowerState.Code == nil {
		return fmt.Errorf("cluster power state is unknown")
	}

	if *cluster.Properties.PowerState.Code != armcontainerservice.CodeRunning {
		return fmt.Errorf("cluster %s is not running, current status: %s", c.clusterName, *cluster.Properties.PowerState.Code)
	}

	fmt.Println("Using Azure AD token-based authentication...")
	return c.initKubernetesClientWithAzureAD(cluster)

}

// GetClusterInfo returns basic information about the AKS cluster
func (c *AKSClient) GetClusterInfo() error {
	ctx := context.Background()

	cluster, err := c.aksClient.Get(ctx, c.resourceGroup, c.clusterName, nil)
	if err != nil {
		return fmt.Errorf("failed to get cluster info: %w", err)
	}

	props := cluster.Properties
	if props == nil {
		return fmt.Errorf("cluster properties are nil")
	}

	fmt.Printf("AKS Cluster Information:\n")
	fmt.Printf("  Name: %s\n", c.clusterName)
	fmt.Printf("  Resource Group: %s\n", c.resourceGroup)

	if props.PowerState != nil && props.PowerState.Code != nil {
		fmt.Printf("  Status: %s\n", *props.PowerState.Code)
	}

	if props.KubernetesVersion != nil {
		fmt.Printf("  Kubernetes Version: %s\n", *props.KubernetesVersion)
	}

	if props.Fqdn != nil {
		fmt.Printf("  FQDN: %s\n", *props.Fqdn)
	}

	if cluster.Location != nil {
		fmt.Printf("  Location: %s\n", *cluster.Location)
	}

	if props.AgentPoolProfiles != nil {
		totalNodes := int32(0)
		for _, pool := range props.AgentPoolProfiles {
			if pool.Count != nil {
				totalNodes += *pool.Count
			}
		}
		fmt.Printf("  Total Nodes: %d\n", totalNodes)
	}

	if props.NetworkProfile != nil && props.NetworkProfile.NetworkPlugin != nil {
		fmt.Printf("  Network Plugin: %s\n", *props.NetworkProfile.NetworkPlugin)
	}

	return nil
}

// ListPods lists all pods in the kube-system namespace
func (c *AKSClient) ListPods() error {
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

// GetSubscriptionID returns the configured Azure subscription ID
func (c *AKSClient) GetSubscriptionID() string {
	return c.subscriptionID
}

// GetResourceGroup returns the configured resource group
func (c *AKSClient) GetResourceGroup() string {
	return c.resourceGroup
}

func RunAKSTest() error {
	// Get cluster details from environment variables or use defaults
	clusterName := os.Getenv("AKS_CLUSTER_NAME")
	if clusterName == "" {
		clusterName = "my-aks-cluster" // Default cluster name
	}

	resourceGroup := os.Getenv("AZURE_RESOURCE_GROUP")
	if resourceGroup == "" {
		return fmt.Errorf("AZURE_RESOURCE_GROUP environment variable must be set")
	}

	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		return fmt.Errorf("AZURE_SUBSCRIPTION_ID environment variable must be set")
	}

	fmt.Printf("Connecting to AKS cluster '%s' in resource group '%s' (subscription: %s)...\n",
		clusterName, resourceGroup, subscriptionID)

	// Create AKS client
	client, err := NewAKSClient(clusterName, resourceGroup, subscriptionID)
	if err != nil {
		return fmt.Errorf("failed to create AKS client: %w", err)
	}

	fmt.Println("✓ Successfully connected to AKS cluster!")

	// Get cluster information
	if err := client.GetClusterInfo(); err != nil {
		log.Printf("Failed to get cluster info: %v", err)
	}

	// List pods in kube-system namespace
	if err := client.ListPods(); err != nil {
		log.Printf("Failed to list pods: %v", err)
	}

	fmt.Println("\n✓ AKS operations completed successfully!")
	return nil
}
