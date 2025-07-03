# Managed Kubernetes Connection Tool

A Go application demonstrating secure connections to Kubernetes clusters across AWS EKS, Google GKE, and Azure AKS using official cloud SDKs.

## What This Does

- **Connects** to managed Kubernetes clusters on AWS, GCP, and Azure
- **Authenticates** using each cloud provider's native authentication methods
- **Retrieves** cluster information and lists system pods
- **Uses token-based authentication** for modern, secure AKS connections

## Authentication Approach

### AWS EKS
- IAM authenticator with temporary tokens
- Multiple credential sources (CLI, profiles, IAM roles)

### Google GKE  
- OAuth2 with Application Default Credentials
- Service account or user-based authentication

### Azure AKS (Token-First Approach)
**Primary: Azure AD Token Authentication**
- Uses Azure AD tokens with official AKS server application ID (`6dae42f8-4368-4678-94ff-3960e28e3630`)
- Direct connection to Kubernetes API endpoint with bearer token
- Automatically extracts CA certificate for secure TLS verification
- Falls back to insecure connection if CA certificate unavailable
- Works with both Azure AD-integrated and traditional clusters
- Modern, secure, and recommended approach

**Fallback: Kubeconfig Authentication** 
- Falls back to `ListClusterAdminCredentials` if token auth fails
- Uses traditional kubeconfig-based authentication
- Supports clusters with local accounts enabled

## Common Issues

### AKS CA Certificate Extraction
The code automatically extracts the CA certificate for secure TLS verification:
1. **First**: Tries to get CA cert from admin credentials
2. **Fallback**: Tries to get CA cert from user credentials  
3. **Final Fallback**: Uses insecure connection if CA cert unavailable

### AKS Local Accounts Disabled
If you see "Getting static credential is not allowed because this cluster is set to disable local accounts":
- This is **normal** for Azure AD-integrated clusters
- The code automatically uses Azure AD token authentication
- No additional configuration needed

### AKS Permission Errors
For "AuthorizationFailed" errors, ensure your service principal has the "Azure Kubernetes Service Cluster Admin Role"

### General Issues
- **Authentication errors**: Re-run cloud provider login commands (`aws configure`, `gcloud auth application-default login`, `az login`)
- **Permission errors**: Verify cluster access permissions
- **Connection timeouts**: Check cluster names and regions
