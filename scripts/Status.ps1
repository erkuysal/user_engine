# UserEngine - Check Service Status (PowerShell)
# Usage: .\scripts\Status.ps1

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$LogDir = Join-Path $ProjectDir "logs"

$RedisAddr = if ($env:REDIS_ADDR) { $env:REDIS_ADDR } else { "localhost:6379" }

Write-Host ""
Write-Host "UserEngine Service Status" -ForegroundColor Cyan
Write-Host "════════════════════════════════════════"

function Get-ServiceStatus {
    param(
        [string]$Name
    )
    
    $PidFile = Join-Path $LogDir "$Name.pid"
    $Status = "stopped"
    $StatusColor = "Red"
    $PidInfo = ""
    
    if (Test-Path $PidFile) {
        $Pid = Get-Content $PidFile
        try {
            $Process = Get-Process -Id $Pid -ErrorAction SilentlyContinue
            if ($Process) {
                $Status = "running"
                $StatusColor = "Green"
                $PidInfo = "(PID: $Pid)"
            }
        }
        catch { }
    }
    
    Write-Host "  $($Name.PadRight(12)) " -NoNewline
    Write-Host "● " -NoNewline -ForegroundColor $StatusColor
    Write-Host "$Status $PidInfo"
}

Get-ServiceStatus "gateway"
Get-ServiceStatus "api"
Get-ServiceStatus "sweeper"
Get-ServiceStatus "debouncer"

Write-Host ""
Write-Host "Redis Connection" -ForegroundColor Cyan
Write-Host "════════════════════════════════════════"

$redisHost = $RedisAddr.Split(":")[0]
$redisPort = $RedisAddr.Split(":")[1]

try {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $tcp.Connect($redisHost, [int]$redisPort)
    $tcp.Close()
    Write-Host "  " -NoNewline
    Write-Host "● " -NoNewline -ForegroundColor Green
    Write-Host "connected ($RedisAddr)"
}
catch {
    Write-Host "  " -NoNewline
    Write-Host "● " -NoNewline -ForegroundColor Red
    Write-Host "disconnected ($RedisAddr)"
}

Write-Host ""
Write-Host "Endpoints" -ForegroundColor Cyan
Write-Host "════════════════════════════════════════"
Write-Host "  WebSocket: ws://localhost:8080/ws"
Write-Host "  REST API:  http://localhost:8081"
Write-Host ""

