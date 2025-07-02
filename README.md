# Cloud Test Client

A unified test client for AWS EKS and GCP GKE clusters with improved configuration management following core backend patterns.

## 📁 File Structure

```
test/
├── main.go  - Main entry point that can call either AWS or GCP tests
├── aws.go   - AWS EKS client implementation
├── gke.go   - GCP GKE client implementation
└── README.md
```

## 🚀 Usage

### Auto-detection (Recommended)
The application automatically detects which cloud provider to use based on environment variables:

```bash
# Test AWS EKS (if EKS_CLUSTER_NAME is set)
export EKS_CLUSTER_NAME=my-eks-cluster
go run .

# Test GCP GKE (if both GKE_CLUSTER_NAME and GOOGLE_CLOUD_PROJECT are set)
export GKE_CLUSTER_NAME=my-gke-cluster
export GOOGLE_CLOUD_PROJECT=my-project
go run .
```

### Explicit Provider Selection
You can also force a specific provider:

```bash
# Force AWS test
go run . aws

# Force GCP test  
go run . gcp
```

## ⚙️ Environment Variables

### AWS Configuration
```bash
# Required
EKS_CLUSTER_NAME=my-eks-cluster

# Optional
AWS_REGION=us-west-2                    # Default: us-east-1
AWS_PROFILE=my-profile                  # AWS CLI profile
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE  # Static credentials
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI...  # Static credentials
AWS_SESSION_TOKEN=optional-token        # Session token
```

### GCP Configuration
```bash
# Required
GKE_CLUSTER_NAME=my-gke-cluster
GOOGLE_CLOUD_PROJECT=my-project-id

# Optional
GKE_ZONE=us-central1-a                           # Default: us-central1
GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json  # Service account file
GCP_CREDENTIALS_JSON=base64-encoded-json         # Base64 encoded SA JSON
```

## 📊 Authentication Methods

### AWS Authentication Priority
1. **Static Credentials** - `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY`
2. **AWS Profile** - `AWS_PROFILE`
3. **Default Credential Chain** - Environment variables, IAM roles, etc.

### GCP Authentication Priority
1. **Base64 Encoded JSON** - `GCP_CREDENTIALS_JSON`
2. **Service Account File** - `GOOGLE_APPLICATION_CREDENTIALS`
3. **Application Default Credentials** - gcloud auth, compute engine SA, etc.

## 🔧 Features

### AWS EKS Client (`aws.go`)
- ✅ Multiple authentication methods (static, profile, default chain)
- ✅ STS-based credential validation
- ✅ Account ID verification and display
- ✅ EKS cluster info retrieval
- ✅ Kubernetes API access with IAM authentication
- ✅ Pod listing in kube-system namespace

### GCP GKE Client (`gke.go`)
- ✅ Multiple authentication methods (JSON, file, ADC)
- ✅ Storage API-based credential validation
- ✅ Project ID verification and display
- ✅ GKE cluster info retrieval
- ✅ Kubernetes API access with Google OAuth
- ✅ Pod listing in kube-system namespace

### Main Controller (`main.go`)
- ✅ Auto-detection of cloud provider based on environment
- ✅ Command-line argument support for explicit selection
- ✅ Comprehensive error handling and logging
- ✅ Usage instructions and examples

## 🎯 Examples

### Development with AWS Profile
```bash
export EKS_CLUSTER_NAME=dev-cluster
export AWS_PROFILE=development
export AWS_REGION=us-west-2
go run . aws
```

### CI/CD with Static Credentials
```bash
export EKS_CLUSTER_NAME=prod-cluster
export AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID
export AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
go run . aws
```

### GCP with Service Account File
```bash
export GKE_CLUSTER_NAME=prod-cluster
export GOOGLE_CLOUD_PROJECT=my-project
export GOOGLE_APPLICATION_CREDENTIALS=/secrets/sa.json
go run . gcp
```

### GCP with Base64 Encoded Credentials
```bash
export GKE_CLUSTER_NAME=prod-cluster
export GOOGLE_CLOUD_PROJECT=my-project
export GCP_CREDENTIALS_JSON=$(base64 -w 0 < service-account.json)
go run . gcp
```

## 🏗️ Architecture

The application follows the core backend plugin patterns:

- **Client Manager Pattern** - Centralized client creation and configuration
- **Multiple Auth Sources** - Support for various credential providers
- **Credential Validation** - Test credentials before attempting operations
- **Proper Error Handling** - Context-aware error messages
- **Configuration Validation** - Validate required fields and formats

## 🔗 Dependencies

- **AWS SDK v2** - For AWS API interactions
- **Google Cloud Client Libraries** - For GCP API interactions
- **Kubernetes Client-Go** - For Kubernetes API access
- **AWS IAM Authenticator** - For EKS authentication
- **godotenv** - For .env file support 