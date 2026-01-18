# UserEngine - Start All Services (PowerShell)
# Usage: .\scripts\Start.ps1 [-Build]

param(
    [switch]$Build
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$BinDir = Join-Path $ProjectDir "bin"
$LogDir = Join-Path $ProjectDir "logs"

# Configuration
$env:REDIS_ADDR = if ($env:REDIS_ADDR) { $env:REDIS_ADDR } else { "localhost:6379" }
$env:JWT_SECRET = if ($env:JWT_SECRET) { $env:JWT_SECRET } else { "dev-secret-change-in-production" }
$env:GATEWAY_ADDR = if ($env:GATEWAY_ADDR) { $env:GATEWAY_ADDR } else { ":8080" }
$env:API_ADDR = if ($env:API_ADDR) { $env:API_ADDR } else { ":8081" }

function Write-Banner {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "║         UserEngine Presence Engine        ║" -ForegroundColor Cyan
    Write-Host "╚═══════════════════════════════════════════╝" -ForegroundColor Cyan
    Write-Host ""
}

function Test-Redis {
    Write-Host "Checking Redis connection..." -ForegroundColor Yellow
    
    $redisHost = $env:REDIS_ADDR.Split(":")[0]
    $redisPort = $env:REDIS_ADDR.Split(":")[1]
    
    try {
        $tcp = New-Object System.Net.Sockets.TcpClient
        $tcp.Connect($redisHost, [int]$redisPort)
        $tcp.Close()
        Write-Host "✓ Redis is running at $env:REDIS_ADDR" -ForegroundColor Green
        return $true
    }
    catch {
        Write-Host "✗ Redis not available at $env:REDIS_ADDR" -ForegroundColor Red
        Write-Host "  Please start Redis first:" -ForegroundColor Yellow
        Write-Host "    docker run -d -p 6379:6379 redis:7-alpine"
        return $false
    }
}

function Build-Services {
    Write-Host "Building services..." -ForegroundColor Yellow
    
    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir | Out-Null
    }
    
    Push-Location $ProjectDir
    try {
        go build -o "$BinDir\gateway.exe" .\cmd\gateway
        go build -o "$BinDir\api.exe" .\cmd\api
        go build -o "$BinDir\sweeper.exe" .\cmd\worker-sweeper
        go build -o "$BinDir\debouncer.exe" .\cmd\worker-debouncer
        Write-Host "✓ All services built" -ForegroundColor Green
    }
    finally {
        Pop-Location
    }
}

function Start-Service {
    param(
        [string]$Name,
        [string]$Binary
    )
    
    $LogFile = Join-Path $LogDir "$Name.log"
    
    Write-Host "Starting $Name..." -ForegroundColor Cyan
    
    $Process = Start-Process -FilePath $Binary `
        -RedirectStandardOutput $LogFile `
        -RedirectStandardError "$LogDir\$Name.err.log" `
        -PassThru `
        -WindowStyle Hidden
    
    # Save PID
    $Process.Id | Out-File -FilePath "$LogDir\$Name.pid" -Encoding ASCII
    
    Start-Sleep -Milliseconds 500
    
    if (-not $Process.HasExited) {
        Write-Host "✓ $Name started (PID: $($Process.Id))" -ForegroundColor Green
        return $true
    }
    else {
        Write-Host "✗ $Name failed to start" -ForegroundColor Red
        if (Test-Path $LogFile) {
            Get-Content $LogFile
        }
        return $false
    }
}

function Start-AllServices {
    if (-not (Test-Path $LogDir)) {
        New-Item -ItemType Directory -Path $LogDir | Out-Null
    }
    
    $success = $true
    $success = $success -and (Start-Service -Name "gateway" -Binary "$BinDir\gateway.exe")
    $success = $success -and (Start-Service -Name "api" -Binary "$BinDir\api.exe")
    $success = $success -and (Start-Service -Name "sweeper" -Binary "$BinDir\sweeper.exe")
    $success = $success -and (Start-Service -Name "debouncer" -Binary "$BinDir\debouncer.exe")
    
    if ($success) {
        Write-Host ""
        Write-Host "═══════════════════════════════════════════" -ForegroundColor Green
        Write-Host "  All services started successfully!" -ForegroundColor Green
        Write-Host "═══════════════════════════════════════════" -ForegroundColor Green
        Write-Host ""
        Write-Host "  WebSocket Gateway: " -NoNewline -ForegroundColor Cyan
        Write-Host "ws://localhost$($env:GATEWAY_ADDR)/ws"
        Write-Host "  REST API:          " -NoNewline -ForegroundColor Cyan
        Write-Host "http://localhost$($env:API_ADDR)"
        Write-Host ""
        Write-Host "  Logs: " -NoNewline -ForegroundColor Yellow
        Write-Host $LogDir
        Write-Host "  Stop: " -NoNewline -ForegroundColor Yellow
        Write-Host ".\scripts\Stop.ps1"
        Write-Host ""
    }
}

# Main
Write-Banner

if (-not (Test-Redis)) {
    exit 1
}

# Build if requested or binaries don't exist
if ($Build -or -not (Test-Path "$BinDir\gateway.exe")) {
    Build-Services
}

Start-AllServices

