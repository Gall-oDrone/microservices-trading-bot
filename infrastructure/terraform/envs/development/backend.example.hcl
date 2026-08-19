# Reference values for the development remote backend.
# These are already hardcoded in backend.tf, so `terraform init` needs no
# -backend-config for this environment. Kept here for documentation / DR.
bucket         = "mtb-tfstate-326105557351-us-east-1"
key            = "microservices-trading-bot/development/terraform.tfstate"
region         = "us-east-1"
dynamodb_table = "mtb-tflock"
encrypt        = true
