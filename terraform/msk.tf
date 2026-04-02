# ── MSK (Managed Kafka) ───────────────────────────────────────────────────────
# Managed Kafka replacing the kafka StatefulSet from Kubernetes.
# Same three topics: edit-events, execution-events, dead-letter.

resource "aws_msk_configuration" "main" {
  name           = "pairly-msk-config"
  kafka_versions = [var.msk_kafka_version]
  description    = "MSK configuration for Pairly"

  # Match retention settings from docker-compose and kubernetes
  server_properties = <<-EOF
    auto.create.topics.enable=false
    log.retention.hours=720
    log.retention.bytes=-1
    num.partitions=1
    default.replication.factor=2
    min.insync.replicas=1
  EOF
}

resource "aws_msk_cluster" "main" {
  cluster_name           = "pairly-kafka"
  kafka_version          = var.msk_kafka_version
  number_of_broker_nodes = var.msk_number_of_broker_nodes

  broker_node_group_info {
    instance_type   = var.msk_instance_type
    client_subnets  = aws_subnet.private[*].id
    security_groups = [aws_security_group.msk.id]

    storage_info {
      ebs_storage_info {
        volume_size = 100
      }
    }
  }

  configuration_info {
    arn      = aws_msk_configuration.main.arn
    revision = aws_msk_configuration.main.latest_revision
  }

  client_authentication {
    unauthenticated = true
  }

  encryption_info {
    encryption_in_transit {
      client_broker = "PLAINTEXT"
      in_cluster    = true
    }
  }

  broker_logs {
    cloudwatch_logs {
      enabled   = true
      log_group = "/aws/msk/pairly"
    }
  }

  open_monitoring {
    prometheus {
      jmx_exporter {
        enabled_in_broker = true
      }
      node_exporter {
        enabled_in_broker = true
      }
    }
  }

  tags = {
    Name = "pairly-kafka"
  }
}