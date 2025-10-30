variable "region" { type = string }
variable "cluster_name" { type = string }
variable "vpc_id" { type = string }
variable "subnet_ids" { type = list(string) }
variable "kafka_version" { type = string  default = "3.6.0" }
variable "broker_instance_type" { type = string  default = "kafka.m5.large" }
variable "number_of_broker_nodes" { type = number default = 2 }
