# ── Outputs ───────────────────────────────────────────────────────────────────
# Run `terraform output` after apply to get all connection strings.
# These map directly to the env vars in k8s/configmap.yaml.

output "eks_cluster_name" {
  description = "EKS cluster name — use with: aws eks update-kubeconfig --name <value>"
  value       = aws_eks_cluster.main.name
}

output "eks_cluster_endpoint" {
  description = "EKS API server endpoint"
  value       = aws_eks_cluster.main.endpoint
}

output "kubeconfig_command" {
  description = "Run this to configure kubectl for this cluster"
  value       = "aws eks update-kubeconfig --region ${var.aws_region} --name ${var.cluster_name}"
}

output "postgres_endpoint" {
  description = "RDS PostgreSQL endpoint — set as POSTGRES_HOST in configmap"
  value       = aws_db_instance.postgres.address
}

output "postgres_port" {
  description = "RDS PostgreSQL port"
  value       = aws_db_instance.postgres.port
}

output "postgres_connection_string" {
  description = "Full PostgreSQL connection string"
  value       = "postgres://${var.rds_username}:${var.rds_password}@${aws_db_instance.postgres.endpoint}/${var.rds_db_name}"
  sensitive   = true
}

output "documentdb_endpoint" {
  description = "DocumentDB cluster endpoint — set as MONGO_URI in configmap"
  value       = aws_docdb_cluster.main.endpoint
}

output "documentdb_connection_string" {
  description = "Full DocumentDB connection string"
  value       = "mongodb://${var.docdb_username}:${var.docdb_password}@${aws_docdb_cluster.main.endpoint}:27017/?tls=false"
  sensitive   = true
}

output "redis_endpoint" {
  description = "ElastiCache Redis primary endpoint — set as REDIS_ADDR in configmap"
  value       = aws_elasticache_replication_group.main.primary_endpoint_address
}

output "kafka_bootstrap_brokers" {
  description = "MSK Kafka bootstrap brokers — set as KAFKA_BROKER in configmap"
  value       = aws_msk_cluster.main.bootstrap_brokers
}

output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = aws_subnet.private[*].id
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = aws_subnet.public[*].id
}