# Does the gateway actually spread work across the mlc replicas?
#
# Run from mf-inference\ on the GPU box after scaling:
#   docker compose up -d --scale mlc=2 --force-recreate gateway
#   .\check-lb.ps1
#
# Worth having as a script rather than a paragraph in the README: a load
# balancer that isn't balancing looks exactly like one that is, from outside.
# The only honest check is which replica actually served the work.

$ErrorActionPreference = "Stop"

# --- key -------------------------------------------------------------------
$line = Select-String -Path .env -Pattern '^LLM_API_KEY=' | Select-Object -First 1
if (-not $line) { Write-Error "LLM_API_KEY not found in .env" }
$key = $line.Line -replace '^LLM_API_KEY=', ''
Write-Host "key length: $($key.Length)" -ForegroundColor DarkGray

Set-Content -Path body.json -NoNewline -Value '{"model":"gemma-2-2b-it-q4f16_1-MLC","messages":[{"role":"user","content":"Say hello in one sentence."}]}'

function Served-By {
  # The compose log prefix is the container name, so the first field names the
  # replica that handled the request.
  (docker compose logs --no-color mlc 2>$null |
    Select-String "POST /v1/chat/completions" |
    ForEach-Object { ($_.Line -split '\s+')[0] })
}

Write-Host "`n=== replicas ===" -ForegroundColor Cyan
docker compose ps mlc

# Where each replica's log stands before this run, so the counts below describe
# these six requests rather than everything since the container started.
$before = @(Served-By).Count

Write-Host "`n=== 6 requests ===" -ForegroundColor Cyan
1..6 | ForEach-Object {
  curl.exe -s -o NUL -w "$_ -> %{http_code}  %{time_total}s`n" `
    -X POST http://127.0.0.1:8080/v1/chat/completions `
    -H "Content-Type: application/json" -H "X-API-Key: $key" `
    -d "@body.json"
}

Write-Host "`n=== served by ===" -ForegroundColor Cyan
$new = @(Served-By) | Select-Object -Skip $before
$new | Group-Object | Select-Object @{n = 'replica'; e = { $_.Name } }, Count | Format-Table -AutoSize

if (($new | Sort-Object -Unique).Count -ge 2) {
  Write-Host "Load balancing: WORKING - more than one replica served traffic." -ForegroundColor Green
}
else {
  Write-Host "Load balancing: NOT working - one replica took everything." -ForegroundColor Yellow
  Write-Host "Next: docker compose logs --tail 40 gateway" -ForegroundColor Yellow
}

# Six short requests will not be evenly split: least_conn sends each one to
# whichever replica is free, and these finish fast enough that the first is
# often idle again before the third arrives. Both replicas appearing at all is
# the result being checked, not a 3/3 split.

Write-Host "`n=== GPU ===" -ForegroundColor Cyan
nvidia-smi --query-gpu=memory.used,memory.total --format=csv

Remove-Item body.json -ErrorAction SilentlyContinue
