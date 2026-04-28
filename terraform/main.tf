locals {
  template_vars = {
    namespace                     = var.namespace
    service_type                  = var.service_type
    service_account_name          = var.service_account_name
    argo_mcp_image                = var.argo_mcp_image
    argo_mcp_port                 = var.argo_mcp_port
    argo_mcp_replicas             = var.argo_mcp_replicas
    git_mcp_image                 = var.git_mcp_image
    git_mcp_port                  = var.git_mcp_port
    git_mcp_replicas              = var.git_mcp_replicas
    k8s_mcp_image                 = var.k8s_mcp_image
    k8s_mcp_port                  = var.k8s_mcp_port
    k8s_mcp_replicas              = var.k8s_mcp_replicas
    k8s_api_server                = var.k8s_api_server
    k8s_insecure                  = tostring(var.k8s_insecure)
    k8s_namespace_scope           = var.k8s_namespace_scope
    argocd_server                 = var.argocd_server
    argocd_insecure               = tostring(var.argocd_insecure)
    argo_rollouts_namespace_scope = var.argo_rollouts_namespace_scope
    github_api_url                = var.github_api_url
    gitlab_api_url                = var.gitlab_api_url
    gitea_api_url                 = var.gitea_api_url
    agents_namespace              = var.agents_namespace
    agent_service_account_name    = var.agent_service_account_name
    orchestrator_agent_image      = var.orchestrator_agent_image
    orchestrator_agent_replicas   = var.orchestrator_agent_replicas
    executor_agent_image          = var.executor_agent_image
    executor_agent_replicas       = var.executor_agent_replicas
    agent_probe_interval_seconds  = var.agent_probe_interval_seconds
    agent_mcp_timeout             = var.agent_mcp_timeout
    agent_mcp_max_retries         = var.agent_mcp_max_retries
    git_mcp_url                   = "http://git-mcp-server.${var.namespace}.svc.cluster.local:${var.git_mcp_port}/mcp"
    argo_mcp_url                  = "http://argo-mcp-server.${var.namespace}.svc.cluster.local:${var.argo_mcp_port}/mcp"
    k8s_mcp_url                   = "http://k8s-mcp-server.${var.namespace}.svc.cluster.local:${var.k8s_mcp_port}/mcp"
  }
}

resource "kubernetes_manifest" "mcp_namespace" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/namespace.yaml", local.template_vars)
  )
}

resource "kubernetes_service_account_v1" "mcp_server" {
  metadata {
    name      = var.service_account_name
    namespace = var.namespace
    labels = {
      "app.kubernetes.io/part-of" = "mcp"
    }
  }

  depends_on = [kubernetes_manifest.mcp_namespace]
}

resource "kubernetes_secret_v1" "argo_mcp_secret" {
  metadata {
    name      = "argo-mcp-secret"
    namespace = var.namespace
  }

  type = "Opaque"

  string_data = {
    ARGOCD_AUTH_TOKEN = var.argocd_token
  }

  depends_on = [kubernetes_manifest.mcp_namespace]
}

resource "kubernetes_secret_v1" "git_mcp_secret" {
  metadata {
    name      = "git-mcp-secret"
    namespace = var.namespace
  }

  type = "Opaque"

  string_data = {
    GIT_GITHUB_TOKEN = var.github_token
    GIT_GITLAB_TOKEN = var.gitlab_token
    GIT_GITEA_TOKEN  = var.gitea_token
  }

  depends_on = [kubernetes_manifest.mcp_namespace]
}

resource "kubernetes_manifest" "argo_mcp_configmap" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/argo-mcp-configmap.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.mcp_namespace]
}

resource "kubernetes_manifest" "argo_mcp_deployment" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/argo-mcp-deployment.yaml", local.template_vars)
  )

  depends_on = [
    kubernetes_manifest.argo_mcp_configmap,
    kubernetes_secret_v1.argo_mcp_secret,
    kubernetes_service_account_v1.mcp_server,
  ]
}

resource "kubernetes_manifest" "argo_mcp_service" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/argo-mcp-service.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.argo_mcp_deployment]
}

resource "kubernetes_manifest" "git_mcp_configmap" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/git-mcp-configmap.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.mcp_namespace]
}

resource "kubernetes_manifest" "git_mcp_deployment" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/git-mcp-deployment.yaml", local.template_vars)
  )

  depends_on = [
    kubernetes_manifest.git_mcp_configmap,
    kubernetes_secret_v1.git_mcp_secret,
    kubernetes_service_account_v1.mcp_server,
  ]
}

resource "kubernetes_manifest" "git_mcp_service" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/git-mcp-service.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.git_mcp_deployment]
}

resource "kubernetes_manifest" "k8s_mcp_clusterrole" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/k8s-mcp-clusterrole.yaml", local.template_vars)
  )
}

resource "kubernetes_manifest" "k8s_mcp_clusterrolebinding" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/k8s-mcp-clusterrolebinding.yaml", local.template_vars)
  )

  depends_on = [
    kubernetes_manifest.k8s_mcp_clusterrole,
    kubernetes_service_account_v1.mcp_server,
  ]
}

resource "kubernetes_manifest" "k8s_mcp_configmap" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/k8s-mcp-configmap.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.mcp_namespace]
}

resource "kubernetes_manifest" "k8s_mcp_deployment" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/k8s-mcp-deployment.yaml", local.template_vars)
  )

  depends_on = [
    kubernetes_manifest.k8s_mcp_configmap,
    kubernetes_manifest.k8s_mcp_clusterrolebinding,
    kubernetes_service_account_v1.mcp_server,
  ]
}

resource "kubernetes_manifest" "k8s_mcp_service" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/mcp/k8s-mcp-service.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.k8s_mcp_deployment]
}

resource "kubernetes_manifest" "agents_namespace" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/agents/namespace.yaml", local.template_vars)
  )
}

resource "kubernetes_manifest" "agent_service_account" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/agents/service-account.yaml", local.template_vars)
  )

  depends_on = [kubernetes_manifest.agents_namespace]
}

resource "kubernetes_manifest" "execution_agent_deployment" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/agents/executor-deployment.yaml", local.template_vars)
  )

  depends_on = [
    kubernetes_manifest.agent_service_account,
    kubernetes_manifest.argo_mcp_service,
    kubernetes_manifest.git_mcp_service,
    kubernetes_manifest.k8s_mcp_service,
  ]
}

resource "kubernetes_manifest" "orchestrator_agent_deployment" {
  manifest = yamldecode(
    templatefile("${path.module}/../kubernetes/agents/orchestrator-deployment.yaml", local.template_vars)
  )

  depends_on = [
    kubernetes_manifest.agent_service_account,
    kubernetes_manifest.execution_agent_deployment,
    kubernetes_manifest.argo_mcp_service,
    kubernetes_manifest.git_mcp_service,
    kubernetes_manifest.k8s_mcp_service,
  ]
}
