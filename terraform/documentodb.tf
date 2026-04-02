# ── DocumentDB (MongoDB-compatible) ──────────────────────────────────────────
# Managed MongoDB-compatible database replacing the mongodb StatefulSet.
# Wire-compatible with MongoDB 4.0 driver — no code changes needed.

resource "aws_docdb_subnet_group" "main" {
  name       = "pairly-docdb-subnet-group"
  subnet_ids = aws_subnet.private[*].id

  tags = {
    Name = "pairly-docdb-subnet-group"
  }
}

resource "aws_docdb_cluster_parameter_group" "main" {
  family      = "docdb5.0"
  name        = "pairly-docdb-params"
  description = "DocumentDB cluster parameter group for Pairly"

  parameter {
    name  = "tls"
    value = "disabled"
  }
}

resource "aws_docdb_cluster" "main" {
  cluster_identifier               = "pairly-docdb"
  engine                           = "docdb"
  engine_version                   = "5.0.0"
  master_username                  = var.docdb_username
  master_password                  = var.docdb_password
  db_subnet_group_name             = aws_docdb_subnet_group.main.name
  vpc_security_group_ids           = [aws_security_group.docdb.id]
  db_cluster_parameter_group_name  = aws_docdb_cluster_parameter_group.main.name

  storage_encrypted       = true
  backup_retention_period = 7
  preferred_backup_window = "03:00-04:00"

  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "pairly-docdb-final-snapshot"

  tags = {
    Name = "pairly-docdb"
  }
}

# Primary instance — handles reads and writes
resource "aws_docdb_cluster_instance" "primary" {
  identifier         = "pairly-docdb-primary"
  cluster_identifier = aws_docdb_cluster.main.id
  instance_class     = var.docdb_instance_class

  tags = { Name = "pairly-docdb-primary" }
}

# Read replica — offloads read traffic (history queries, snapshot reads)
resource "aws_docdb_cluster_instance" "replica" {
  identifier         = "pairly-docdb-replica"
  cluster_identifier = aws_docdb_cluster.main.id
  instance_class     = var.docdb_instance_class

  tags = { Name = "pairly-docdb-replica" }
}