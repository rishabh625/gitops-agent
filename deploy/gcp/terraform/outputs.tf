output "cloud_run_service_url" {
  description = "Public URL of the Cloud Run service."
  value       = google_cloud_run_v2_service.adk_chat.uri
}

output "cloud_run_service_account_email" {
  description = "Service account email used by Cloud Run."
  value       = google_service_account.cloud_run.email
}

output "google_api_key_secret_name" {
  description = "Secret Manager secret name used for GOOGLE_API_KEY."
  value       = var.google_api_key_secret_name
}
