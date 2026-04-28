output "namespace" {
  description = "Namespace where MCP resources are deployed."
  value       = var.namespace
}

output "argo_mcp_service" {
  description = "Argo MCP service name."
  value       = kubernetes_manifest.argo_mcp_service.manifest.metadata.name
}

output "git_mcp_service" {
  description = "Git MCP service name."
  value       = kubernetes_manifest.git_mcp_service.manifest.metadata.name
}

output "k8s_mcp_service" {
  description = "Kubernetes MCP service name."
  value       = kubernetes_manifest.k8s_mcp_service.manifest.metadata.name
}

output "agents_namespace" {
  description = "Namespace where agent runtime resources are deployed."
  value       = var.agents_namespace
}

output "orchestrator_agent_deployment" {
  description = "Orchestrator deployment name."
  value       = kubernetes_manifest.orchestrator_agent_deployment.manifest.metadata.name
}

output "execution_agent_deployment" {
  description = "Execution deployment name."
  value       = kubernetes_manifest.execution_agent_deployment.manifest.metadata.name
}
