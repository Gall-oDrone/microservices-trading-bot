output "instance_id" {
  value = aws_instance.collector.id
}

output "instance_public_ip" {
  value = aws_instance.collector.public_ip
}

output "instance_private_ip" {
  value = aws_instance.collector.private_ip
}

output "security_group_id" {
  value = aws_security_group.collector.id
}

output "iam_role_arn" {
  value = aws_iam_role.collector.arn
}

output "instance_profile_name" {
  value = aws_iam_instance_profile.collector.name
}
