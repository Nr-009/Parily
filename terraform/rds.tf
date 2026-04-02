# ── RDS PostgreSQL ────────────────────────────────────────────────────────────
# Managed PostgreSQL replacing the postgres StatefulSet from Kubernetes.
# Multi-AZ for automatic failover.

resource "aws_db_subnet_group" "main" {
  name       = "pairly-rds-subnet-group"
  subnet_ids = aws_subnet.private[*].id

  tags = {
    Name = "pairly-rds-subnet-group"
  }
}

resource "aws_db_instance" "postgres" {
  identifier        = "pairly-postgres"
  engine            = "postgres"
  engine_version    = "16.2"
  instance_class    = var.rds_instance_class
  allocated_storage = var.rds_allocated_storage
  storage_type      = "gp3"
  storage_encrypted = true

  db_name  = var.rds_db_name
  username = var.rds_username
  password = var.rds_password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  # Multi-AZ — synchronous replication to standby in second AZ.
  # Automatic failover in ~60s if primary becomes unavailable.
  multi_az = true

  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "pairly-postgres-final-snapshot"

  auto_minor_version_upgrade = true

  tags = {
    Name = "pairly-postgres"
  }
}