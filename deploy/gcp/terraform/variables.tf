variable "project_id" {
  description = "GCP project ID."
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run and Artifact Registry."
  type        = string
  default     = "us-central1"
}

variable "service_name" {
  description = "Cloud Run service name for adk-chat."
  type        = string
  default     = "gitops-adk-chat"
}

variable "container_image" {
  description = "Container image URL, e.g. us-central1-docker.pkg.dev/PROJECT/REPO/adk-chat:latest."
  type        = string
}

variable "artifact_registry_repository" {
  description = "Artifact Registry repository name."
  type        = string
  default     = "gitops-agent"
}

variable "create_artifact_registry" {
  description = "Create Artifact Registry repository if true."
  type        = bool
  default     = true
}

variable "google_api_key_secret_name" {
  description = "Secret Manager secret name containing GOOGLE_API_KEY."
  type        = string
  default     = "gitops-google-api-key"
}

variable "create_google_api_key_secret" {
  description = "Create the GOOGLE_API_KEY secret if true (without a secret version)."
  type        = bool
  default     = false
}

variable "cloud_run_service_account_id" {
  description = "Service account ID (not email) used by Cloud Run."
  type        = string
  default     = "gitops-adk-chat-sa"
}

variable "allow_unauthenticated" {
  description = "Allow unauthenticated invocation for Cloud Run service."
  type        = bool
  default     = true
}

variable "cpu" {
  description = "Cloud Run CPU limit."
  type        = string
  default     = "2"
}

variable "memory" {
  description = "Cloud Run memory limit."
  type        = string
  default     = "2Gi"
}

variable "timeout_seconds" {
  description = "Cloud Run request timeout in seconds."
  type        = number
  default     = 3600
}

variable "min_instance_count" {
  description = "Minimum Cloud Run instances."
  type        = number
  default     = 0
}

variable "max_instance_count" {
  description = "Maximum Cloud Run instances."
  type        = number
  default     = 10
}

variable "adk_model" {
  description = "Model name passed to ADK_MODEL."
  type        = string
  default     = "gemini-2.5-flash"
}

variable "adk_public_origin" {
  description = "Public HTTPS origin for ADK web UI/api wiring, e.g. https://service-xyz.run.app. Leave empty for first apply and update after service URL is known."
  type        = string
  default     = ""
}

variable "mcp_timeout" {
  description = "MCP timeout value."
  type        = string
  default     = "20s"
}

variable "mcp_max_retries" {
  description = "MCP max retries."
  type        = number
  default     = 2
}

variable "git_mcp_url" {
  description = "Git MCP endpoint URL (/mcp)."
  type        = string
}

variable "argo_mcp_url" {
  description = "Argo MCP endpoint URL (/mcp)."
  type        = string
}

variable "k8s_mcp_url" {
  description = "Kubernetes MCP endpoint URL (/mcp)."
  type        = string
}
