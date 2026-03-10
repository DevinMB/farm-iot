ui = true

storage "file" {
  path = "/vault/data"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true  # TLS handled by Traefik + Cloudflare upstream
}

api_addr = "http://vault:8200"
cluster_addr = "http://vault:8201"

# Allow mlock on Linux to prevent secrets from being swapped to disk
disable_mlock = false
