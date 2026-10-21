# Build the embedded UI, then both Go binaries on Windows.
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

Push-Location server/web
try {
    npm ci
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
} finally {
    Pop-Location
}

New-Item -ItemType Directory -Force -Path bin | Out-Null
Push-Location server
try {
    go build -o ../bin/probe-server.exe ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "server build failed" }
} finally {
    Pop-Location
}
Push-Location agent
try {
    go build -o ../bin/probe-agent.exe ./cmd/agent
    if ($LASTEXITCODE -ne 0) { throw "agent build failed" }
} finally {
    Pop-Location
}
Write-Host "built bin/probe-server.exe and bin/probe-agent.exe"
