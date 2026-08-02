output "vpc_id" { value = module.vpc.vpc_id }
output "private_subnet_ids" { value = module.vpc.private_subnet_ids }
output "public_subnet_ids" { value = module.vpc.public_subnet_ids }
output "ecr_repositories" { value = module.ecr.repository_urls }

output "data_collector_instance_id" {
  value = try(module.data_collector_ec2[0].instance_id, null)
}

output "data_collector_public_ip" {
  value = try(module.data_collector_ec2[0].instance_public_ip, null)
}

output "data_archive_bucket" {
  value = try(module.data_archive_s3[0].bucket_name, null)
}

output "data_collector_rds_endpoint" {
  value = try(module.data_collector_rds[0].db_endpoint, null)
}

output "data_collector_rds_secret_arn" {
  value = try(module.data_collector_rds[0].secret_arn, null)
}
