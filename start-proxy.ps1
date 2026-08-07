# start-proxy.ps1 - Start the Secure AI Gateway / Proxy

param (
    [string]$ApiKey = $env:VIGILAGENT_API_KEY,
    [string]$BackendUrl = "http://localhost:8080"
)

# Source local environment variables
. "$PSScriptRoot\local-dev.ps1"

if (-not $ApiKey) {
    Write-Host "Warning: VIGILAGENT_API_KEY is not set. Pass -ApiKey <va_...> or set `$env:VIGILAGENT_API_KEY" -ForegroundColor Yellow
}

$env:VIGILAGENT_API_KEY = $ApiKey
$env:VIGILAGENT_BACKEND_URL = $BackendUrl

Write-Host "Starting VigilAgent Proxy / Secure Gateway on :9090..." -ForegroundColor Green
go run ./cmd/proxy
