# start-api.ps1 - Apply migrations and start the VigilAgent API server

# Source local environment variables
. "$PSScriptRoot\local-dev.ps1"

Write-Host "Running database migrations..." -ForegroundColor Cyan
go run ./cmd/migrate up

Write-Host "Starting VigilAgent API Server on :8080..." -ForegroundColor Green
go run ./cmd/api
