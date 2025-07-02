# Managed Kubernetes Connection Tool

A Go application demonstrating secure connections to Kubernetes clusters across AWS EKS, Google GKE, and Azure AKS using official cloud SDKs.

## What This Does

- **Connects** to managed Kubernetes clusters on AWS, GCP, and Azure
- **Authenticates** using each cloud provider's native authentication methods
- **Retrieves** cluster information and lists system pods


## Authentication Methods

| Cloud | Primary Method | Alternative |
|-------|---------------|-------------|
| AWS | AWS CLI / IAM Role | Access Keys, Profile |
| GCP | Application Default Credentials | Service Account Key |
| Azure | Azure CLI | Service Principal, Managed Identity |
