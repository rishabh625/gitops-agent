variable "kubeconfig_path" {
  description = "Absolute path to kubeconfig used to connect to GKE."
  type        = string
  default     = "~/.kube/config"
}

variable "kube_context" {
  description = "Kube context name for the target GKE cluster."
  type        = string
}

variable "namespace" {
  description = "Namespace where MCP servers are deployed."
  type        = string
  default     = "mcp-system"
}

variable "service_account_name" {
  description = "Service account used by MCP workloads."
  type        = string
  default     = "mcp-server-sa"
}

variable "service_type" {
  description = "Kubernetes Service type for MCP endpoints."
  type        = string
  default     = "ClusterIP"
}

variable "argo_mcp_image" {
  description = "Container image for Argo MCP server."
  type        = string
}

variable "argo_mcp_port" {
  description = "Argo MCP server container and service port."
  type        = number
  default     = 8080
}

variable "argo_mcp_replicas" {
  description = "Replica count for Argo MCP deployment."
  type        = number
  default     = 1
}

variable "argocd_server" {
  description = "Argo CD API/server URL (host:port or full URL based on MCP image expectations)."
  type        = string
}

variable "argocd_insecure" {
  description = "Whether Argo CD TLS verification is disabled."
  type        = bool
  default     = false
}

variable "argo_rollouts_namespace_scope" {
  description = "Namespace scope used by the Argo Rollouts integration."
  type        = string
  default     = "*"
}

variable "argocd_token" {
  description = "Auth token used by Argo MCP server to call Argo CD."
  type        = string
  sensitive   = true
}

variable "git_mcp_image" {
  description = "Container image for Git MCP server."
  type        = string
}

variable "git_mcp_port" {
  description = "Git MCP server container and service port."
  type        = number
  default     = 8080
}

variable "git_mcp_replicas" {
  description = "Replica count for Git MCP deployment."
  type        = number
  default     = 1
}

variable "k8s_mcp_image" {
  description = "Container image for Kubernetes MCP server."
  type        = string
}

variable "k8s_mcp_port" {
  description = "Kubernetes MCP server container and service port."
  type        = number
  default     = 8080
}

variable "k8s_mcp_replicas" {
  description = "Replica count for Kubernetes MCP deployment."
  type        = number
  default     = 1
}

variable "k8s_api_server" {
  description = "Kubernetes API URL used by Kubernetes MCP server."
  type        = string
  default     = "https://kubernetes.default.svc"
}

variable "k8s_insecure" {
  description = "Whether TLS verification is disabled for Kubernetes API."
  type        = bool
  default     = false
}

variable "k8s_namespace_scope" {
  description = "Namespace scope used by Kubernetes MCP integration."
  type        = string
  default     = "*"
}

variable "github_api_url" {
  description = "Public GitHub API URL (must start with https://)."
  type        = string
  default     = "https://api.github.com"

  validation {
    condition     = can(regex("^https://", var.github_api_url))
    error_message = "github_api_url must be a public HTTPS URL (for example: https://api.github.com)."
  }
}

variable "gitlab_api_url" {
  description = "Public GitLab API URL (must start with https://)."
  type        = string

  validation {
    condition     = can(regex("^https://", var.gitlab_api_url))
    error_message = "gitlab_api_url must be a public HTTPS URL (for example: https://gitlab.example.com/api/v4)."
  }
}

variable "gitea_api_url" {
  description = "Public Gitea API URL (must start with https://)."
  type        = string

  validation {
    condition     = can(regex("^https://", var.gitea_api_url))
    error_message = "gitea_api_url must be a public HTTPS URL (for example: https://gitea.example.com/api/v1)."
  }
}

variable "github_token" {
  description = "Token for GitHub API."
  type        = string
  sensitive   = true
}

variable "gitlab_token" {
  description = "Token for GitLab API."
  type        = string
  sensitive   = true
}

variable "gitea_token" {
  description = "Token for Gitea API."
  type        = string
  sensitive   = true
}

variable "agents_namespace" {
  description = "Namespace where orchestrator and execution agents are deployed."
  type        = string
  default     = "agent-platform"
}

variable "agent_service_account_name" {
  description = "Service account used by agent workloads."
  type        = string
  default     = "agent-engine-sa"
}

variable "orchestrator_agent_image" {
  description = "Container image for ADK-Go orchestrator agent."
  type        = string
}

variable "orchestrator_agent_replicas" {
  description = "Replica count for orchestrator agent deployment."
  type        = number
  default     = 1
}

variable "executor_agent_image" {
  description = "Container image for ADK-Go execution agent."
  type        = string
}

variable "executor_agent_replicas" {
  description = "Replica count for execution agent deployment."
  type        = number
  default     = 1
}

variable "agent_probe_interval_seconds" {
  description = "Seconds between MCP reachability probes in agent pods."
  type        = number
  default     = 30
}

variable "agent_mcp_timeout" {
  description = "Timeout for MCP calls from agent containers."
  type        = string
  default     = "20s"
}

variable "agent_mcp_max_retries" {
  description = "Max MCP retries for agents."
  type        = number
  default     = 2
}
