# Why is the load balancer only using one replica?
#
# Three questions, in the order that narrows it fastest:
#   1. Do the replicas have distinct IPs at all?
#   2. Does the service name resolve to more than one of them?
#   3. Is the health check URI real, or is Caddy parking replicas for failing
#      a probe against an endpoint that does not exist?
#
# Deliberately quiet: the gateway's access log is dominated by 15-second
# Prometheus scrapes, so `logs --tail` is nearly useless for this.

$ErrorActionPreference = "Continue"

Write-Host "=== 1. replica IPs ===" -ForegroundColor Cyan
docker inspect -f '{{.Name}} -> {{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' `
  mf-inference-mlc-1 mf-inference-mlc-2

Write-Host "`n=== 2. what does the name 'mlc' resolve to? ===" -ForegroundColor Cyan
# Repeated, because Docker's embedded DNS round-robins the order it returns
# records in. A musl `getent` prints only the first, so one call proves nothing
# and several calls returning different addresses proves the records are there.
Write-Host "(six lookups; different addresses across lines means DNS has both)" -ForegroundColor DarkGray
docker compose exec -T gateway sh -c "for i in 1 2 3 4 5 6; do getent hosts mlc; done"

Write-Host "`n=== 3. is the health check URI real? ===" -ForegroundColor Cyan
# Caddy is configured with health_uri /v1/models, which was a guess about what
# mlc_llm serves. If that path 404s, Caddy is parking replicas for failing a
# probe against an endpoint that never existed.
foreach ($name in @("mf-inference-mlc-1", "mf-inference-mlc-2")) {
  $code = docker exec $name sh -c "curl -s -o /dev/null -w '%{http_code}' http://localhost:8000/v1/models" 2>$null
  Write-Host "$name  /v1/models -> $code"
}

Write-Host "`n=== 4. gateway: only the interesting lines ===" -ForegroundColor Cyan
docker compose logs --no-color --tail 400 gateway |
  Select-String -Pattern "health|unhealthy|upstream|dynamic|error" |
  Select-Object -Last 20
