output "bucket_name" {
  value = aws_s3_bucket.archive.bucket
}

output "bucket_arn" {
  value = aws_s3_bucket.archive.arn
}

output "bucket_id" {
  value = aws_s3_bucket.archive.id
}
