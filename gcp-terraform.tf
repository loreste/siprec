# Terraform configuration for deploying SIPREC Server on GCP
# Usage: terraform init && terraform plan && terraform apply

terraform {
  required_version = ">= 1.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 4.0"
    }
  }
}

# Variables
variable "project_id" {
  description = "GCP Project ID"
  type        = string
}

variable "region" {
  description = "GCP Region"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP Zone"
  type        = string
  default     = "us-central1-a"
}

variable "machine_type" {
  description = "GCP Machine Type"
  type        = string
  default     = "e2-standard-2"
}

variable "disk_size" {
  description = "Boot disk size in GB"
  type        = number
  default     = 50
}

variable "siprec_repo_url" {
  description = "SIPREC Repository URL"
  type        = string
  default     = "https://github.com/yourusername/siprec.git"
}

# Provider configuration
provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

# Create a VPC network
resource "google_compute_network" "siprec_network" {
  name                    = "siprec-network"
  auto_create_subnetworks = false
  description             = "Network for SIPREC Server"
}

# Create a subnet
resource "google_compute_subnetwork" "siprec_subnet" {
  name          = "siprec-subnet"
  ip_cidr_range = "10.0.0.0/24"
  region        = var.region
  network       = google_compute_network.siprec_network.id
  description   = "Subnet for SIPREC Server"
}

# Firewall rules for SIPREC
resource "google_compute_firewall" "siprec_firewall" {
  name    = "siprec-firewall"
  network = google_compute_network.siprec_network.id

  allow {
    protocol = "tcp"
    ports    = ["22", "80", "443", "5060", "5061", "5062", "8080"]
  }

  allow {
    protocol = "udp"
    ports    = ["5060", "5061", "16384-32768"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["siprec-server"]
  description   = "Firewall rules for SIPREC Server"
}

# Create a static external IP
resource "google_compute_address" "siprec_external_ip" {
  name         = "siprec-external-ip"
  region       = var.region
  address_type = "EXTERNAL"
  description  = "Static external IP for SIPREC Server"
}

# Create startup script from template
data "template_file" "startup_script" {
  template = file("${path.module}/gcp-startup-script.sh")
  vars = {
    repo_url = var.siprec_repo_url
  }
}

# Create the SIPREC server instance
resource "google_compute_instance" "siprec_server" {
  name         = "siprec-server"
  machine_type = var.machine_type
  zone         = var.zone
  tags         = ["siprec-server"]

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = var.disk_size
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = google_compute_network.siprec_network.id
    subnetwork = google_compute_subnetwork.siprec_subnet.id
    
    access_config {
      nat_ip = google_compute_address.siprec_external_ip.address
    }
  }

  metadata = {
    startup-script = data.template_file.startup_script.rendered
    enable-oslogin = "TRUE"
  }

  service_account {
    scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
      "https://www.googleapis.com/auth/logging.write",
      "https://www.googleapis.com/auth/monitoring.write"
    ]
  }

  metadata_startup_script = data.template_file.startup_script.rendered

  labels = {
    environment = "production"
    service     = "siprec"
  }
}

# Output values
output "siprec_external_ip" {
  description = "External IP address of the SIPREC server"
  value       = google_compute_address.siprec_external_ip.address
}

output "siprec_internal_ip" {
  description = "Internal IP address of the SIPREC server"
  value       = google_compute_instance.siprec_server.network_interface[0].network_ip
}

output "ssh_command" {
  description = "SSH command to connect to the server"
  value       = "gcloud compute ssh siprec-server --zone=${var.zone} --project=${var.project_id}"
}

output "web_interface" {
  description = "Web interface URL"
  value       = "http://${google_compute_address.siprec_external_ip.address}"
}

output "sip_endpoint" {
  description = "SIP endpoint for testing"
  value       = "${google_compute_address.siprec_external_ip.address}:5060"
}