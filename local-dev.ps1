# local-dev.ps1 - Environment Overrides for Local Testing in PowerShell

$env:GOTOOLCHAIN = "auto"
$env:VIGILAGENT_DATABASE_HOST = "localhost"
$env:VIGILAGENT_DATABASE_PORT = "5432"
$env:VIGILAGENT_DATABASE_USER = "vigilagent"
$env:VIGILAGENT_DATABASE_PASSWORD = "vigilagent_dev"
$env:VIGILAGENT_DATABASE_NAME = "vigilagent"
$env:VIGILAGENT_DATABASE_SSLMODE = "disable"
$env:VIGILAGENT_REDIS_PORT = "6379"

Write-Host "Local dev environment variables set successfully!" -ForegroundColor Green
