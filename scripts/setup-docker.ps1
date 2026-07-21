# VigilAgent - Docker Setup Script
# Run this as Administrator, then restart your computer.

Write-Host "=== VigilAgent Docker Setup ===" -ForegroundColor Cyan
Write-Host ""

# Step 1: Enable WSL2
Write-Host "[1/4] Enabling Windows Subsystem for Linux..." -ForegroundColor Yellow
dism.exe /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart
if ($LASTEXITCODE -ne 0) {
    Write-Host "FAILED to enable WSL. Make sure you're running as Administrator." -ForegroundColor Red
    exit 1
}

# Step 2: Enable Virtual Machine Platform
Write-Host "[2/4] Enabling Virtual Machine Platform..." -ForegroundColor Yellow
dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart
if ($LASTEXITCODE -ne 0) {
    Write-Host "FAILED to enable Virtual Machine Platform. Make sure you're running as Administrator." -ForegroundColor Red
    exit 1
}

# Step 3: Install WSL2 Linux kernel update
Write-Host "[3/4] Installing WSL2 Linux kernel update..." -ForegroundColor Yellow
wsl --update 2>&1

# Step 4: Set WSL2 as default
Write-Host "[4/4] Setting WSL2 as default version..." -ForegroundColor Yellow
wsl --set-default-version 2 2>&1

Write-Host ""
Write-Host "=== Setup Complete! ===" -ForegroundColor Green
Write-Host ""
Write-Host "NEXT STEPS:" -ForegroundColor Cyan
Write-Host "1. RESTART YOUR COMPUTER now" -ForegroundColor White
Write-Host "2. After restart, open PowerShell and run:" -ForegroundColor White
Write-Host "   wsl --install -d Ubuntu" -ForegroundColor Yellow
Write-Host "3. Set up your Ubuntu username/password when prompted" -ForegroundColor White
Write-Host "4. Docker Desktop should start automatically after Ubuntu is installed" -ForegroundColor White
Write-Host "5. Then run these commands to complete Phase 2:" -ForegroundColor White
Write-Host "   cd D:\Work\Projects\VigilAgent" -ForegroundColor Yellow
Write-Host "   docker compose -f docker-compose.dev.yml up -d" -ForegroundColor Yellow
Write-Host "   go run ./cmd/migrate up" -ForegroundColor Yellow
Write-Host "   go test -v ./..." -ForegroundColor Yellow
Write-Host ""
