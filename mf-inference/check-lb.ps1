# Does the gateway spread work across the mlc replicas, and does the hardware
# cope with the load it is given?
#
#   .\check-lb.ps1                        # 3 requests, 150 tokens each
#   .\check-lb.ps1 -Concurrency 6 -MaxTokens 400
#   .\check-lb.ps1 -Concurrency 1         # baseline: one request, no contention
#
# Parameterised because the two questions need different loads. Balancing only
# becomes visible when requests overlap; capacity only becomes visible when they
# overlap enough to hurt. Sweeping the concurrency is how you find where this
# card stops coping — 6 concurrent 400-token generations across two replicas on
# one 6 GB GPU did not complete inside three minutes.

param(
	[int]$Concurrency = 3,
	[int]$MaxTokens = 150,
	[int]$TimeoutSeconds = 120
)

$ErrorActionPreference = "Stop"

# --- key -------------------------------------------------------------------
$line = Select-String -Path .env -Pattern '^LLM_API_KEY=' | Select-Object -First 1
if (-not $line) { Write-Error "LLM_API_KEY not found in .env" }
$key = $line.Line -replace '^LLM_API_KEY=', ''
Write-Host "key length: $($key.Length)" -ForegroundColor DarkGray

# A prompt that takes seconds, not milliseconds.
#
# This matters more than it looks. least_conn routes to the replica with the
# fewest active connections, so it only has a decision to make while requests
# actually overlap. A 60ms "say hello" finishes before the next one starts:
# every replica is idle at every arrival, the tie never breaks, and everything
# lands on the first upstream — which reads as a broken load balancer and is
# not one. Long generations are what create real concurrency.
$body = @{
	model      = "gemma-2-2b-it-q4f16_1-MLC"
	max_tokens = $MaxTokens
	messages   = @(@{
			role    = "user"
			content = "Explain how goroutines, channels and the Go scheduler work together."
		})
} | ConvertTo-Json -Depth 5 -Compress
Set-Content -Path body.json -NoNewline -Value $body

function Served-By {
  # The compose log prefix is the container name, so the first field names the
  # replica that handled the request.
  (docker compose logs --no-color mlc 2>$null |
    Select-String "POST /v1/chat/completions" |
    ForEach-Object { ($_.Line -split '\s+')[0] })
}

Write-Host "`n=== replicas ===" -ForegroundColor Cyan
docker compose ps mlc

# There is nothing to balance across one replica, and reporting "not working"
# in that case sends you hunting a bug that isn't there. `docker compose up`
# reconciles the project to the compose file, which declares no replica count —
# so touching any service quietly scales mlc back to one.
$replicaCount = @(docker compose ps -q mlc).Count
if ($replicaCount -lt 2) {
	Write-Host "`nOnly $replicaCount replica running - nothing to balance." -ForegroundColor Yellow
	Write-Host "  docker compose up -d --scale mlc=2" -ForegroundColor Yellow
	Write-Host "Continuing anyway; treat the result as a single-replica baseline." -ForegroundColor Yellow
}

# Where each replica's log stands before this run, so the counts below describe
# these six requests rather than everything since the container started.
$before = @(Served-By).Count

# Concurrent and slow enough to overlap. PowerShell's Start-Job costs a good
# fraction of a second to spin up, so with millisecond requests the "parallel"
# jobs would still finish one after another — the reason an earlier version of
# this script reported a false negative.
Write-Host "`n=== $Concurrency concurrent requests, $MaxTokens tokens each ===" -ForegroundColor Cyan
$started = Get-Date
$jobs = 1..$Concurrency | ForEach-Object {
  Start-Job -ArgumentList $PWD, $key, $_, $TimeoutSeconds -ScriptBlock {
    param($dir, $key, $n, $timeout)
    Set-Location $dir
    curl.exe -s -o NUL --max-time $timeout -w "$n -> %{http_code}  %{time_total}s`n" `
      -X POST http://127.0.0.1:8080/v1/chat/completions `
      -H "Content-Type: application/json" -H "X-API-Key: $key" `
      -d "@body.json"
  }
}
$jobs | Wait-Job | Receive-Job
$jobs | Remove-Job
$wall = [math]::Round(((Get-Date) - $started).TotalSeconds, 1)
Write-Host "wall clock for the whole batch: ${wall}s" -ForegroundColor DarkGray

# Give the replicas a moment to flush their access logs before reading them.
Start-Sleep -Seconds 2

Write-Host "`n=== served by ===" -ForegroundColor Cyan
$new = @(Served-By) | Select-Object -Skip $before
$new | Group-Object | Select-Object @{n = 'replica'; e = { $_.Name } }, Count | Format-Table -AutoSize

if (($new | Sort-Object -Unique).Count -ge 2) {
  Write-Host "Load balancing: WORKING - more than one replica served traffic." -ForegroundColor Green
}
else {
  Write-Host "Load balancing: one replica took everything." -ForegroundColor Yellow
  Write-Host "If the requests above each took well under a second, they did not" -ForegroundColor Yellow
  Write-Host "overlap and this is not a verdict. Run .\diagnose-lb.ps1 instead." -ForegroundColor Yellow
}

# An even split is not the pass condition and should not be expected: least_conn
# sends each request to whichever replica is free, so a 4/2 is as correct as a
# 3/3. Both replicas appearing at all is what is being checked.

Write-Host "`n=== GPU ===" -ForegroundColor Cyan
nvidia-smi --query-gpu=memory.used,memory.total --format=csv

Remove-Item body.json -ErrorAction SilentlyContinue
