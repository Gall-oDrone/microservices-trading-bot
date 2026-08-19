terraform {
  # Remote state so it survives IDE/EC2 teardown and is shared across machines.
  # deploy-data-collector.sh / cleanup-data-collector.sh call `terraform init`
  # with no -backend-config, so the settings are hardcoded here on purpose.
  # See docs/data-collector/TERRAFORM-STATE-BACKEND.md for the full rationale.
  backend "s3" {
    bucket         = "mtb-tfstate-326105557351-us-east-1"
    key            = "microservices-trading-bot/development/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "mtb-tflock"
    encrypt        = true
  }
}
