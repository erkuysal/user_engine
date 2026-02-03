# UserEngine - Start All Services (PowerShell)
# Usage: .\scripts\Start.ps1 [-Build] [-Production]

param(
    [switch]$Build,
    [switch]$Production
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$BinDir = Join-Path $ProjectDir "bin"
$LogDir = Join-Path $ProjectDir "logs"

# Set environment mode
if ($Production) {
    $env:ENVIRONMENT = "production"
    Write-Host "Running in PRODUCTION mode" -ForegroundColor Red
} else {
    $env:ENVIRONMENT = if ($env:ENVIRONMENT) { $env:ENVIRONMENT } else { "development" }
    Write-Host "Running in $($env:ENVIRONMENT) mode" -ForegroundColor Green
}

# Load .env file if exists
$EnvFile = Join-Path $ProjectDir ".env"
if (Test-Path $EnvFile) {
    Write-Host "Loading configuration from .env" -ForegroundColor Cyan
    Get-Content $EnvFile | ForEach-Object {
        if ($_ -match '^\s*([^#][^=]+)=(.*)$') {
            $key = $matches[1].Trim()
            $value = $matches[2].Trim()
            # Don't override ENVIRONMENT if already set
            if ($key -ne "ENVIRONMENT" -or -not $env:ENVIRONMENT) {
                [Environment]::SetEnvironmentVariable($key, $value, "Process")
            }
        }
    }
}

# Fallback to defaults if not set
$env:REDIS_ADDR = if ($env:REDIS_ADDR) { $env:REDIS_ADDR } else { "localhost:6379" }
$env:JWT_SECRET = if ($env:JWT_SECRET) { $env:JWT_SECRET } else { "dev-secret-change-in-production" }
$env:GATEWAY_ADDR = if ($env:GATEWAY_ADDR) { $env:GATEWAY_ADDR } else { ":8080" }
$env:API_ADDR = if ($env:API_ADDR) { $env:API_ADDR } else { ":8081" }

# Validate production configuration
if ($env:ENVIRONMENT -eq 'production') {
    Write-Host "Validating production configuration..." -ForegroundColor Yellow
    
    if ($env:JWT_SECRET -like "dev-secret*") {
        Write-Host "❌ Error: JWT_SECRET must be changed for production" -ForegroundColor Red
        Write-Host "   Set a secure JWT_SECRET in your .env file" -ForegroundColor Yellow
        exit 1
    }
    
    if ($env:CORS_ALLOW_ALL -eq "true") {
        Write-Host "❌ Error: CORS_ALLOW_ALL=true is not allowed in production" -ForegroundColor Red
        exit 1
    }
    
    if (-not $env:CORS_ALLOWED_ORIGINS) {
        Write-Host "❌ Error: CORS_ALLOWED_ORIGINS must be set in production" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "✓ Production configuration is valid" -ForegroundColor Green
}

function Write-Banner {
    Write-Host ""
    Write-Host "╔═══════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "║         UserEngine Presence Engine        ║" -ForegroundColor Cyan
    Write-Host "╚═══════════════════════════════════════════╝" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "  Environment: " -NoNewline -ForegroundColor Yellow
    Write-Host $env:ENVIRONMENT -ForegroundColor $(if ($env:ENVIRONMENT -eq 'production') { 'Red' } else { 'Green' })
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

