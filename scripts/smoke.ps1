# 业务冒烟（S1 最小闭环）：发布时段→预约→支付→回调→核销→开始→完成→日清。
# 仅使用模拟支付渠道回调；不依赖 Docker/MySQL。用法：pwsh -File scripts/smoke.ps1
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$port = Get-Random -Minimum 18100 -Maximum 18199
$base = "http://127.0.0.1:$port"
$db = Join-Path $env:TEMP ("paw-smoke-" + [guid]::NewGuid().ToString("N") + ".db")
$env:PAW_ADDR = "127.0.0.1:$port"
$env:PAW_DB = $db
$env:TEACHING_CLOCK_MODE = 'fixed'
$env:TEACHING_CLOCK_BASE = '2026-11-14T06:00:00Z'
$env:PAW_PAY_SECRET = 'smoke-secret'

$log = Join-Path $env:TEMP ("paw-smoke-" + [guid]::NewGuid().ToString("N") + ".log")
$exe = Join-Path $env:TEMP ("paw-smoke-" + [guid]::NewGuid().ToString("N") + ".exe")
& go build -o $exe ./cmd/server
if ($LASTEXITCODE -ne 0) { throw "构建服务失败" }
$proc = Start-Process -FilePath $exe -WorkingDirectory $root -RedirectStandardOutput $log -RedirectStandardError "$log.err" -PassThru -WindowStyle Hidden

function Api($session, $method, $path, $body, $headers = @{}) {
  $params = @{ Uri = "$base$path"; Method = $method; WebSession = $session; Headers = $headers }
  if ($null -ne $body) {
    $json = if ($body -is [string]) { $body } else { $body | ConvertTo-Json -Compress }
    $params.Body = $json
    $params.ContentType = 'application/json'
  }
  try {
    $r = Invoke-RestMethod @params
    if ($r.code -ne 0) { throw "$($r.code): $($r.message)" }
    return $r.data
  } catch {
    if ($_.ErrorDetails.Message) { throw $_.ErrorDetails.Message }
    throw $_
  }
}

function Sign($text) {
  $hmac = New-Object System.Security.Cryptography.HMACSHA256
  $hmac.Key = [Text.Encoding]::UTF8.GetBytes($env:PAW_PAY_SECRET)
  ($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)) | ForEach-Object { $_.ToString('x2') }) -join ''
}

try {
  $ready = $false
  for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    try { Invoke-RestMethod "$base/readyz" | Out-Null; $ready = $true; break } catch {}
  }
  if (-not $ready) { throw "服务未就绪，日志：$log / $log.err" }
  if ($proc.HasExited) { throw "服务进程提前退出，日志：$log / $log.err" }

  $user = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  $mgr = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  $rec = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  $srv = New-Object Microsoft.PowerShell.Commands.WebRequestSession

  Api $user POST '/api/auth/login' @{account='u1';password='123456'} | Out-Null
  Api $mgr POST '/api/auth/login' @{account='mgr';password='123456'} | Out-Null
  Api $rec POST '/api/auth/login' @{account='rec';password='123456'} | Out-Null
  Api $srv POST '/api/auth/login' @{account='srv';password='123456'} | Out-Null

  $slot = Api $mgr POST '/api/manager/slots' @{startAt='2026-11-14T09:00:00Z';endAt='2026-11-14T10:00:00Z';capacity=3}
  $res = Api $user POST '/api/reservations' @{slotId=$slot.slotId;petId=1}
  $pay = Api $user POST "/api/reservations/$($res.reservationId)/pay" $null

  $notify = @{channelTxnId=('smoke-txn-' + [guid]::NewGuid().ToString('N'));orderNo=$pay.orderNo;success=$true} | ConvertTo-Json -Compress
  $sig = Sign $notify
  $n = Api $null POST '/api/payment/notify' $notify @{'X-Mock-Signature'=$sig}
  if ($n.result -ne 'CONFIRMED') { throw "支付回调结果异常：$($n.result)" }

  Api $rec POST "/api/reception/reservations/$($res.reservationId)/checkin" $null | Out-Null
  Api $srv POST "/api/service/reservations/$($res.reservationId)/start" $null | Out-Null
  Api $srv POST "/api/service/reservations/$($res.reservationId)/complete" $null | Out-Null
  $daily = Api $mgr GET '/api/manager/daily-close?date=2026-11-14' $null

  if ($daily.CompletedIncomeCents -ne 9900) { throw "已完成收入异常：$($daily.CompletedIncomeCents)" }
  if ($daily.UnaccountedCents -ne 0) { throw "金额勾稽差额非 0：$($daily.UnaccountedCents)" }
  $smoke = Api $null GET '/api/smoke' $null

  "PASS 冒烟：slot=$($slot.slotId) reservation=$($res.reservationId) payment=$($pay.paymentId) 收入=$($daily.CompletedIncomeCents) 差额=$($daily.UnaccountedCents) 种子=$($smoke.stores)/$($smoke.staff)/$($smoke.slots)"
} finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
  # 兜底：按端口清理可能残留的子进程，保证冒烟可重复执行
  Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique |
    ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
  Start-Sleep -Milliseconds 300
  Remove-Item $db, "$db-shm", "$db-wal", $log, "$log.err", $exe -ErrorAction SilentlyContinue
}
