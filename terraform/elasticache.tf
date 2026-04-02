# ── ElastiCache Redis ─────────────────────────────────────────────────────────
# Managed Redis replacing the redis StatefulSet from Kubernetes.
# Used for pub/sub, presence tracking, execution locks, and rate limiting.

resource "aws_elasticache_subnet_group" "main" {
  name       = "pairly-redis-subnet-group"
  subnet_ids = aws_subnet.private[*].id

  tags = {
    Name = "pairly-redis-subnet-group"
  }
}

resource "aws_elasticache_parameter_group" "main" {
  name   = "pairly-redis-params"
  family = "redis7"

  parameter {
    name  = "notify-keyspace-events"
    value = "Ex"
  }
}

resource "aws_elasticache_replication_group" "main" {
  replication_group_id = "pairly-redis"
  description          = "Redis replication group for Pairly"

  node_type            = var.redis_node_type
  num_cache_clusters   = var.redis_num_cache_nodes
  engine_version       = "7.1"
  port                 = 6379

  subnet_group_name    = aws_elasticache_subnet_group.main.name
  security_group_ids   = [aws_security_group.redis.id]
  parameter_group_name = aws_elasticache_parameter_group.main.name

  # Automatic failover requires num_cache_clusters >= 2
  automatic_failover_enabled = var.redis_num_cache_nodes > 1 ? true : false

  at_rest_encryption_enabled = true
  transit_encryption_enabled = false

  snapshot_retention_limit = 3
  snapshot_window          = "03:00-04:00"
  maintenance_window       = "sun:04:00-sun:05:00"

  tags = {
    Name = "pairly-redis"
  }
}