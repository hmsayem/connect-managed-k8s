package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/joho/godotenv"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

const (
	AWSDefaultRegion = "us-east-1"
)

// AWSConfig represents AWS configuration options
type AWSConfig struct {
	Region       string
	Profile      string
	AccessKey    string
	SecretKey    string
	SessionToken string
}

// AWSClientManager manages AWS clients and configurations
type AWSClientManager struct {
	config    AWSConfig
	awsConfig aws.Config
}

// NewAWSClientManager creates a new AWS client manager
func NewAWSClientManager(cfg AWSConfig) (*AWSClientManager, error) {
	manager := &AWSClientManager{
		config: cfg,
	}

	if err := manager.initializeAWSConfig(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize AWS config: %w", err)
	}

	return manager, nil
}

// initializeAWSConfig initializes the AWS configuration based on the provided options
func (m *AWSClientManager) initializeAWSConfig(ctx context.Context) error {
	var awsCfg aws.Config
	var err error

	if m.config.Region == "" {
		m.config.Region = AWSDefaultRegion
	}

	if m.config.AccessKey != "" && m.config.SecretKey != "" {
		fmt.Println("Using static AWS credentials")
		awsCfg, err = m.configWithStaticCredentials(ctx)
	} else if m.config.Profile != "" {
		fmt.Printf("Using AWS profile: %s\n", m.config.Profile)
		awsCfg, err = m.configWithSharedProfile(ctx)
	} else {
		fmt.Println("Using default AWS credential chain")
		awsCfg, err = m.configWithDefaultChain(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	if err := m.validateCredentials(ctx, awsCfg); err != nil {
		return fmt.Errorf("AWS credential validation failed: %w", err)
	}

	m.awsConfig = awsCfg
	return nil
}

// configWithStaticCredentials creates AWS config using static credentials
func (m *AWSClientManager) configWithStaticCredentials(ctx context.Context) (aws.Config, error) {
	customProvider := credentials.StaticCredentialsProvider{
		Value: aws.Credentials{
			AccessKeyID:     m.config.AccessKey,
			SecretAccessKey: m.config.SecretKey,
			SessionToken:    m.config.SessionToken,
		},
	}

	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(m.config.Region),
		config.WithCredentialsProvider(customProvider),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config with static credentials: %w", err)
	}

	return awsCfg, nil
}

// configWithSharedProfile creates AWS config using shared profile
func (m *AWSClientManager) configWithSharedProfile(ctx context.Context) (aws.Config, error) {
	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(m.config.Region),
		config.WithSharedConfigProfile(m.config.Profile),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config with profile %s: %w", m.config.Profile, err)
	}

	return awsCfg, nil
}

// configWithDefaultChain creates AWS config using default credential chain
func (m *AWSClientManager) configWithDefaultChain(ctx context.Context) (aws.Config, error) {
	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(m.config.Region),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config with default chain: %w", err)
	}

	return awsCfg, nil
}

// validateCredentials validates AWS credentials by making a test STS call
func (m *AWSClientManager) validateCredentials(ctx context.Context, awsCfg aws.Config) error {
	stsClient := sts.NewFromConfig(awsCfg)

	result, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}

	if result.Account == nil || result.Arn == nil || result.UserId == nil {
		return fmt.Errorf("incomplete AWS caller identity information")
	}

	fmt.Printf("AWS Credentials Validated:\n")
	fmt.Printf("  Account ID: %s\n", aws.ToString(result.Account))
	fmt.Printf("  User ID: %s\n", aws.ToString(result.UserId))
	fmt.Printf("  ARN: %s\n", aws.ToString(result.Arn))

	return nil
}

// GetAWSConfig returns the initialized AWS configuration
func (m *AWSClientManager) GetAWSConfig() aws.Config {
	return m.awsConfig
}

// GetAccountID retrieves the AWS Account ID dynamically using STS
func (m *AWSClientManager) GetAccountID(ctx context.Context) (string, error) {
	stsClient := sts.NewFromConfig(m.awsConfig)
	result, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get AWS account ID: %w", err)
	}

	return aws.ToString(result.Account), nil
}

// EKSClient wraps the EKS and Kubernetes clients with improved AWS configuration
type EKSClient struct {
	awsClientManager *AWSClientManager
	eksClient        *eks.Client
	k8sClient        *kubernetes.Clientset
	clusterName      string
	region           string
}

// NewEKSClient creates a new EKS client with improved AWS configuration management
func NewEKSClient(clusterName string, awsConfig AWSConfig) (*EKSClient, error) {
	clientManager, err := NewAWSClientManager(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS client manager: %w", err)
	}

	eksClient := eks.NewFromConfig(clientManager.GetAWSConfig())

	client := &EKSClient{
		awsClientManager: clientManager,
		eksClient:        eksClient,
		clusterName:      clusterName,
		region:           awsConfig.Region,
	}

	if err := client.initKubernetesClient(); err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	return client, nil
}

// initKubernetesClient initializes the Kubernetes client using EKS cluster info
func (c *EKSClient) initKubernetesClient() error {
	clusterOutput, err := c.eksClient.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name: aws.String(c.clusterName),
	})
	if err != nil {
		return fmt.Errorf("failed to describe EKS cluster: %w", err)
	}

	cluster := clusterOutput.Cluster
	if cluster.Status != "ACTIVE" {
		return fmt.Errorf("cluster %s is not active, current status: %s", c.clusterName, cluster.Status)
	}

	caCert, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
	if err != nil {
		return fmt.Errorf("failed to decode certificate authority data: %w", err)
	}

	generator, err := token.NewGenerator(true, false)
	if err != nil {
		return fmt.Errorf("failed to create token generator: %w", err)
	}

	tok, err := generator.GetWithOptions(context.TODO(), &token.GetTokenOptions{
		ClusterID: c.clusterName,
	})
	if err != nil {
		return fmt.Errorf("failed to generate auth token: %w", err)
	}

	kubeConfig := &rest.Config{
		Host:        *cluster.Endpoint,
		BearerToken: tok.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caCert,
		},
	}

	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	c.k8sClient = clientset
	return nil
}

// GetClusterInfo returns basic information about the EKS cluster
func (c *EKSClient) GetClusterInfo() error {
	clusterOutput, err := c.eksClient.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name: aws.String(c.clusterName),
	})
	if err != nil {
		return fmt.Errorf("failed to describe cluster: %w", err)
	}

	cluster := clusterOutput.Cluster
	fmt.Printf("Cluster Information:\n")
	fmt.Printf("  Name: %s\n", *cluster.Name)
	fmt.Printf("  Status: %s\n", cluster.Status)
	fmt.Printf("  Version: %s\n", *cluster.Version)
	fmt.Printf("  Endpoint: %s\n", *cluster.Endpoint)
	fmt.Printf("  Created: %s\n", cluster.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Platform Version: %s\n", *cluster.PlatformVersion)

	return nil
}

// ListKubeSystemPods lists all pods in the kube-system namespace
func (c *EKSClient) ListPods() error {
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

// GetAccountID returns the AWS account ID for this EKS client
func (c *EKSClient) GetAccountID(ctx context.Context) (string, error) {
	return c.awsClientManager.GetAccountID(ctx)
}

// GetRegion returns the configured AWS region
func (c *EKSClient) GetRegion() string {
	return c.region
}

// RunAWSTest runs the AWS EKS test client
func RunEKSTest() error {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found, using environment variables")
	}

	clusterName := os.Getenv("EKS_CLUSTER_NAME")
	if clusterName == "" {
		return fmt.Errorf("EKS_CLUSTER_NAME environment variable is required")
	}

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = AWSDefaultRegion
		fmt.Printf("AWS_REGION not set, using default: %s\n", region)
	}

	awsConfig := AWSConfig{
		Region:       region,
		Profile:      os.Getenv("AWS_PROFILE"),
		AccessKey:    os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretKey:    os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken: os.Getenv("AWS_SESSION_TOKEN"),
	}

	fmt.Printf("Connecting to EKS cluster '%s' in region '%s'...\n", clusterName, region)

	client, err := NewEKSClient(clusterName, awsConfig)
	if err != nil {
		return fmt.Errorf("failed to create EKS client: %w", err)
	}

	fmt.Println("✓ Successfully connected to EKS cluster!")

	accountID, err := client.GetAccountID(context.Background())
	if err != nil {
		log.Printf("Warning: Failed to get AWS account ID: %v", err)
	} else {
		fmt.Printf("Connected to AWS Account: %s\n", accountID)
	}

	if err := client.GetClusterInfo(); err != nil {
		log.Printf("Failed to get cluster info: %v", err)
	}

	if err := client.ListPods(); err != nil {
		log.Printf("Failed to list pods: %v", err)
	}

	fmt.Println("\n✓ EKS operations completed successfully!")
	return nil
}
