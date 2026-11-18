param(
    [switch]$BootstrapAdmin,
    [string]$Username,
    [string]$DisplayName
)

$ErrorActionPreference = 'Stop'
$envPath = Join-Path $PSScriptRoot '.env'
$settings = @(
    'APP_ENV', 'HTTP_ADDR', 'DATABASE_URL', 'CORS_ALLOWED_ORIGINS',
    'JWT_ISSUER', 'JWT_AUDIENCE', 'JWT_ACCESS_SECRET', 'JWT_REFRESH_SECRET', 'COOKIE_SECURE'
)
$loaded = @()

Push-Location -LiteralPath $PSScriptRoot
try {
    if (Test-Path -LiteralPath $envPath -PathType Leaf) {
        foreach ($line in Get-Content -LiteralPath $envPath -Encoding utf8) {
            if ($line -notmatch '^\s*([A-Z_]+)\s*=\s*(.*?)\s*$') { continue }
            $key = $Matches[1]
            $value = $Matches[2]
            if ($key -cnotin $settings -or $null -ne [Environment]::GetEnvironmentVariable($key)) { continue }
            if ($value -match '^"(.*)"$' -or $value -match "^'(.*)'$") {
                $value = $Matches[1]
            }
            [Environment]::SetEnvironmentVariable($key, $value)
            $loaded += $key
        }
    }
    if ($BootstrapAdmin) {
        if (!$Username -or !$DisplayName) { throw 'Indica -Username y -DisplayName.' }
        $OutputEncoding = [Text.UTF8Encoding]::new($false)
        $password = Read-Host 'Contraseña del administrador (mínimo 12 caracteres)' -MaskInput
        $password | go run ./cmd/server bootstrap-admin $Username $DisplayName
    } else {
        go run ./cmd/server
    }
    if ($LASTEXITCODE -ne 0) { throw "Go exited with code $LASTEXITCODE." }
} finally {
    foreach ($key in $loaded) {
        [Environment]::SetEnvironmentVariable($key, $null)
    }
    Pop-Location
}
