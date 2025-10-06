# Trading service policy
path "secret/data/trading/*" {
  capabilities = ["read", "list"]
}

path "secret/data/bitso/*" {
  capabilities = ["read", "list"]
}

path "secret/data/kafka/*" {
  capabilities = ["read", "list"]
}

path "secret/data/redis/*" {
  capabilities = ["read", "list"]
}

# Allow services to renew their own tokens
path "auth/token/renew-self" {
  capabilities = ["update"]
}

# Allow services to lookup their own token
path "auth/token/lookup-self" {
  capabilities = ["read"]
}
