# UserEngine - Stop All Services (PowerShell)
# Usage: .\scripts\Stop.ps1

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$LogDir = Join-Path $ProjectDir "logs"

Write-Host "Stopping UserEngine services..." -ForegroundColor Yellow

function Stop-Service {
    param([string]$Name)
    
    $PidFile = Join-Path $LogDir "$Name.pid"
    
    if (Test-Path $PidFile) {
        $Pid = Get-Content $PidFile
        try {
            $Process = Get-Process -Id $Pid -ErrorAction SilentlyContinue
            if ($Process) {
                Stop-Process -Id $Pid -Force
                Write-Host "✓ Stopped $Name (PID: $Pid)" -ForegroundColor Green
            }
            else {
                Write-Host "○ $Name was not running" -ForegroundColor Yellow
            }
        }
        catch {
            Write-Host "○ $Name was not running" -ForegroundColor Yellow
        }
        Remove-Item $PidFile -Force -ErrorAction SilentlyContinue
    }
    else {
        Write-Host "○ $Name pid file not found" -ForegroundColor Yellow
    }
}

Stop-Service "gateway"
Stop-Service "api"
Stop-Service "sweeper"
Stop-Service "debouncer"

Write-Host ""
Write-Host "All services stopped" -ForegroundColor Green

