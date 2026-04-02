terraform {
  required_version = ">= 1.7.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
  }

  # Remote state in S3 — create this bucket manually before running terraform init.
  # This ensures state is shared across machines and never lost.
  backend "s3" {
    bucket = "pairly-terraform-state"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"

    # DynamoDB table for state locking — prevents two people running apply at once.
    dynamodb_table = "pairly-terraform-locks"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "pairly"
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}

# Kubernetes provider uses the EKS cluster output.
# This lets Terraform manage K8s resources after the cluster is created.
provider "kubernetes" {
  host                   = aws_eks_cluster.main.endpoint
  cluster_ca_certificate = base64decode(aws_eks_cluster.main.certificate_authority[0].data)

  exec {
    api_version = "client.authentication.k8s.io/v1beta1"
    command     = "aws"
    args        = ["eks", "get-token", "--cluster-name", var.cluster_name]
  }
}