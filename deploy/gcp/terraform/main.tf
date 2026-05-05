locals {
  required_apis = [
    "run.googleapis.com",
    "artifactregistry.googleapis.com",
    "secretmanager.googleapis.com",
    "iam.googleapis.com",
  ]
}

resource "google_project_service" "required" {
  for_each           = toset(local.required_apis)
  project            = var.project_id
  service            = each.key
  disable_on_destroy = false
}

resource "google_artifact_registry_repository" "images" {
  count = var.create_artifact_registry ? 1 : 0

  project       = var.project_id
  location      = var.region
  repository_id = var.artifact_registry_repository
  description   = "Container images for gitops-agent"
  format        = "DOCKER"

  depends_on = [google_project_service.required]
}

resource "google_service_account" "cloud_run" {
  account_id   = var.cloud_run_service_account_id
  display_name = "gitops adk chat runtime"
  project      = var.project_id

  depends_on = [google_project_service.required]
}

resource "google_secret_manager_secret" "google_api_key" {
  count = var.create_google_api_key_secret ? 1 : 0

  secret_id = var.google_api_key_secret_name
  project   = var.project_id

  replication {
    auto {}
  }

  depends_on = [google_project_service.required]
}

data "google_secret_manager_secret" "google_api_key" {
  count = var.create_google_api_key_secret ? 0 : 1

  project   = var.project_id
  secret_id = var.google_api_key_secret_name

  depends_on = [google_project_service.required]
}

locals {
  google_api_key_secret_id = var.create_google_api_key_secret ? google_secret_manager_secret.google_api_key[0].id : data.google_secret_manager_secret.google_api_key[0].id
}

resource "google_secret_manager_secret_iam_member" "cloud_run_secret_access" {
  project   = var.project_id
  secret_id = local.google_api_key_secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run.email}"
}

resource "google_project_iam_member" "cloud_run_vertex_user" {
  project = var.project_id
  role    = "roles/aiplatform.user"
  member  = "serviceAccount:${google_service_account.cloud_run.email}"
}

resource "google_cloud_run_v2_service" "adk_chat" {
  name     = var.service_name
  location = var.region
  project  = var.project_id
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    service_account = google_service_account.cloud_run.email
    timeout         = "${var.timeout_seconds}s"

    scaling {
      min_instance_count = var.min_instance_count
      max_instance_count = var.max_instance_count
    }

    containers {
      image   = var.container_image
      command = ["/app/cloud-run-entrypoint.sh"]

      resources {
        limits = {
          cpu    = var.cpu
          memory = var.memory
        }
      }

      env {
        name  = "ADK_MODEL"
        value = var.adk_model
      }

      env {
        name  = "ADK_PUBLIC_ORIGIN"
        value = var.adk_public_origin
      }

      env {
        name  = "SKILL_SOURCE"
        value = "local"
      }

      env {
        name  = "SKILLS_ROOT"
        value = "/app/gitops-skills"
      }

      env {
        name  = "MCP_TIMEOUT"
        value = var.mcp_timeout
      }

      env {
        name  = "MCP_MAX_RETRIES"
        value = tostring(var.mcp_max_retries)
      }

      env {
        name  = "GIT_MCP_URL"
        value = var.git_mcp_url
      }

      env {
        name  = "ARGO_MCP_URL"
        value = var.argo_mcp_url
      }

      env {
        name  = "K8S_MCP_URL"
        value = var.k8s_mcp_url
      }

      env {
        name = "GOOGLE_API_KEY"
        value_source {
          secret_key_ref {
            secret  = var.google_api_key_secret_name
            version = "latest"
          }
        }
      }
    }
  }

  depends_on = [
    google_project_service.required,
    google_secret_manager_secret_iam_member.cloud_run_secret_access,
  ]
}

resource "google_cloud_run_v2_service_iam_member" "public_invoker" {
  count = var.allow_unauthenticated ? 1 : 0

  name     = google_cloud_run_v2_service.adk_chat.name
  location = google_cloud_run_v2_service.adk_chat.location
  project  = var.project_id
  role     = "roles/run.invoker"
  member   = "allUsers"
}
