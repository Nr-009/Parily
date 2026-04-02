# Copy this to terraform.tfvars and fill in the values.
# Never commit terraform.tfvars to git — it contains secrets.
# Add terraform.tfvars to .gitignore.

aws_region   = "us-east-1"
environment  = "prod"
cluster_name = "pairly"

# EKS
kubernetes_version = "1.29"
node_instance_type = "t3.medium"
node_min_size      = 2
node_max_size      = 10
node_desired_size  = 3

# RDS
rds_instance_class    = "db.t3.micro"
rds_db_name           = "pairly"
rds_username          = "pairly"
rds_password          = "CHANGE_ME"
rds_allocated_storage = 20

# DocumentDB
docdb_instance_class = "db.t3.medium"
docdb_username       = "pairly"
docdb_password       = "CHANGE_ME"

# ElastiCache
redis_node_type       = "cache.t3.micro"
redis_num_cache_nodes = 1

# MSK
msk_instance_type          = "kafka.t3.small"
msk_number_of_broker_nodes = 2
msk_kafka_version          = "3.5.1"