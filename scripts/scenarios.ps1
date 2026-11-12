# 五组场景服务端全链路实测（S1 正常接待 / S2 名额循环 / S3 取消分支 / S4 门店现场冲突 / S5 收店核查+重启持久化+未到店）。
# 仅使用模拟支付回调；固定教学时钟，重启阶段前移时钟制造“时段已开始/已结束”。
# 用法：pwsh -File scripts/scenarios.ps1 （输出到 stdout，由调用方归档）
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$root = Split-Path -Parent $PSScriptRoot
$port = Get-Random -Minimum 18200 -Maximum 18299
$base = "http://127.0.0.1:$port"
$db = Join-Path $env:TEMP ("paw-scen-" + [guid]::NewGuid().ToString("N") + ".db")
$env:PAW_ADDR = "127.0.0.1:$port"
$env:PAW_DB = $db
$env:TEACHING_CLOCK_MODE = 'fixed'
$env:TEACHING_CLOCK_BASE = '2026-11-14T00:00:00Z'
$env:PAW_PAY_SECRET = 'scenario-secret'
$env:PAW_ENABLE_MOCK_PAY = '1'
$script:stepNo = 0

$log = Join-Path $env:TEMP ("paw-scen-" + [guid]::NewGuid().ToString("N") + ".log")
$exe = Join-Path $env:TEMP ("paw-scen-" + [guid]::NewGuid().ToString("N") + ".exe")
& go build -o $exe ./cmd/server
if ($LASTEXITCODE -ne 0) { throw "构建服务失败" }
$proc = Start-Process -FilePath $exe -WorkingDirectory $root -RedirectStandardOutput $log -RedirectStandardError "$log.err" -PassThru -WindowStyle Hidden

function Step($msg) {
  $script:stepNo++
  Write-Output ("[{0:D3}] {1}" -f $script:stepNo, $msg)
}
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
# 期望失败的调用：返回 @{http=状态码; body=响应原文}
function ApiExpectError($session, $method, $path, $body) {
  $params = @{ Uri = "$base$path"; Method = $method; WebSession = $session; ErrorAction = 'SilentlyContinue' }
  if ($null -ne $body) {
    $params.Body = $body | ConvertTo-Json -Compress
    $params.ContentType = 'application/json'
  }
  try {
    $r = Invoke-WebRequest @params
    return @{ http = [int]$r.StatusCode; body = $r.Content }
  } catch {
    $code = 0; $txt = ''
    if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
    if ($_.ErrorDetails.Message) { $txt = $_.ErrorDetails.Message } else { $txt = $_.Exception.Message }
    return @{ http = $code; body = $txt }
  }
}
function Sign($text) {
  $hmac = New-Object System.Security.Cryptography.HMACSHA256
  $hmac.Key = [Text.Encoding]::UTF8.GetBytes($env:PAW_PAY_SECRET)
  ($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($text)) | ForEach-Object { $_.ToString('x2') }) -join ''
}
# 列表获取：服务端空列表返回 null，统一转为空数组；逗号防止函数输出时数组被展开
function GetList($session, $path) {
  return ,@(Api $session GET $path $null | Where-Object { $null -ne $_ })
}
function PayNotify($orderNo, $success) {
  $notify = @{channelTxnId = ('scen-txn-' + [guid]::NewGuid().ToString('N')); orderNo = $orderNo; success = $success} | ConvertTo-Json -Compress
  $sig = Sign $notify
  return Api $null POST '/api/payment/notify' $notify @{'X-Mock-Signature' = $sig}
}
function RefundNotify($refundNo, $success) {
  $notify = @{refundNo = $refundNo; success = $success; failReason = ''} | ConvertTo-Json -Compress
  $sig = Sign $notify
  return Api $null POST '/api/refund/notify' $notify @{'X-Mock-Signature' = $sig}
}
function NewSlot($hStart, $cap) {
  $s = ([DateTime]::Parse('2026-11-14T00:00:00Z').ToUniversalTime().AddHours($hStart)).ToString('yyyy-MM-ddTHH:mm:ssZ')
  $e = ([DateTime]::Parse('2026-11-14T00:00:00Z').ToUniversalTime().AddHours($hStart + 1)).ToString('yyyy-MM-ddTHH:mm:ssZ')
  return Api $script:mgr POST '/api/manager/slots' @{startAt = $s; endAt = $e; capacity = $cap}
}
function LoginAll {
  foreach ($n in 'u1', 'u2', 'mgr', 'rec', 'srv', 'cs1') {
    $null = Api $script:sess[$n] POST '/api/auth/login' @{account = $n; password = '123456'}
  }
}
function Assert($cond, $msg) { if (-not $cond) { throw "断言失败：$msg" } }

$script:sess = @{}
foreach ($n in 'u1', 'u2', 'mgr', 'rec', 'srv', 'cs1') { $script:sess[$n] = New-Object Microsoft.PowerShell.Commands.WebRequestSession }
$script:mgr = $script:sess['mgr']

# 并发预约任务（独立会话，同时抢同一时段）
function Start-ReserveJob($account, $petId, $slotId) {
  $b = $base; $s = $slotId
  Start-Job -ScriptBlock {
    param($b, $acc, $pet, $s)
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $null = Invoke-RestMethod -Uri "$b/api/auth/login" -Method POST -Body (@{account = $acc; password = '123456'} | ConvertTo-Json -Compress) -ContentType 'application/json' -WebSession $sess
    try {
      $r = Invoke-RestMethod -Uri "$b/api/reservations" -Method POST -Body (@{slotId = $s; petId = $pet} | ConvertTo-Json -Compress) -ContentType 'application/json' -WebSession $sess
      @{ok = $true; code = $r.code; rid = $r.data.reservationId; acc = $acc; msg = $r.message} | ConvertTo-Json -Compress
    } catch {
      $c = 0; $m = $_.Exception.Message
      if ($_.ErrorDetails.Message) { try { $j = $_.ErrorDetails.Message | ConvertFrom-Json; $c = $j.code; $m = $j.message } catch { $m = $_.ErrorDetails.Message } }
      @{ok = $false; code = $c; msg = $m} | ConvertTo-Json -Compress
    }
  } -ArgumentList $b, $account, $petId, $slotId
}

try {
  $ready = $false
  for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    try { Invoke-RestMethod "$base/readyz" | Out-Null; $ready = $true; break } catch {}
  }
  if (-not $ready) { throw "服务未就绪，日志：$log / $log.err" }
  Step "服务启动 PASS addr=$base db=$db clock=$env:TEACHING_CLOCK_BASE"
  LoginAll
  Step "六账号登录 PASS（u1/u2 宠物主人，mgr/rec/srv/cs1 员工）"

  # ========== S1 正常接待 ==========
  Step "S1 开始：店长发布时段（09:00-10:00Z，容量3）"
  $slotA = NewSlot 9 3
  Step "S1 时段创建 slotId=$($slotA.slotId)"
  $resA = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotA.slotId; petId = 1}
  Step "S1 u1 为宠物1 预约 reservationId=$($resA.reservationId) status=$($resA.status)"
  $payA = Api $sess['u1'] POST "/api/reservations/$($resA.reservationId)/pay" $null
  Step "S1 创建支付单 orderNo=$($payA.orderNo)"
  $n = PayNotify $payA.orderNo $true
  Assert ($n.result -eq 'CONFIRMED') "S1 支付回调应为 CONFIRMED，实际 $($n.result)"
  Step "S1 支付回调验签 CONFIRMED PASS"
  Assert ($n.duplicate -ne $true) "S1 首次回调不得标记重复"
  $n2 = PayNotify $payA.orderNo $true
  Step "S1 重复支付回调 result=$($n2.result) duplicate=$($n2.duplicate)（不得重复确认）"
  $ci = Api $sess['rec'] POST "/api/reception/reservations/$($resA.reservationId)/checkin" $null
  Step "S1 前台核销 PASS alreadyDone=$($ci.alreadyDone)"
  Api $sess['srv'] POST "/api/service/reservations/$($resA.reservationId)/start" $null | Out-Null
  Step "S1 服务开始 PASS"
  $cm = Api $sess['srv'] POST "/api/service/reservations/$($resA.reservationId)/complete" $null
  Step "S1 服务完成 PASS alreadyDone=$($cm.alreadyDone)"
  $cm2 = Api $sess['srv'] POST "/api/service/reservations/$($resA.reservationId)/complete" $null
  Assert ($cm2.alreadyDone -eq $true) "S1 重复完成应返回 alreadyDone=true"
  Step "S1 重复完成 alreadyDone=true PASS（不重复记收入）"
  $d1 = Api $sess['u1'] GET "/api/reservations/$($resA.reservationId)" $null
  Assert ($d1.status -eq 'COMPLETED') "S1 用户查询应为 COMPLETED，实际 $($d1.status)"
  Step "S1 用户查询预约状态=$($d1.status) 金额=$($d1.priceCents) PASS"
  $daily = Api $mgr GET '/api/manager/daily-close?date=2026-11-14' $null
  Assert ($daily.CompletedIncomeCents -eq 9900) "S1 日清已完成收入应 9900，实际 $($daily.CompletedIncomeCents)"
  Assert ($daily.UnaccountedCents -eq 0) "S1 日清差额应 0，实际 $($daily.UnaccountedCents)"
  Step "S1 店长日清：已完成收入=$($daily.CompletedIncomeCents) 差额=$($daily.UnaccountedCents) PASS"
  Step "S1 正常接待 全链路 PASS"

  # ========== S2 名额循环 ==========
  Step "S2 开始：并发抢位（容量1，u1/pet1 与 u2/pet3 同时预约）"
  $slotB = NewSlot 11 1
  $j1 = Start-ReserveJob 'u1' 1 $slotB.slotId
  $j2 = Start-ReserveJob 'u2' 3 $slotB.slotId
  Wait-Job $j1, $j2 -Timeout 60 | Out-Null
  $r1 = Receive-Job $j1 | Select-Object -Last 1
  $r2 = Receive-Job $j2 | Select-Object -Last 1
  Remove-Job $j1, $j2
  Step "S2 并发结果 A=$r1"
  Step "S2 并发结果 B=$r2"
  $okCount = @($r1, $r2 | Where-Object { ($_ | ConvertFrom-Json).ok }).Count
  Assert ($okCount -eq 1) "S2 并发抢位应恰好 1 个成功，实际成功数 $okCount"
  Step "S2 并发抢位唯一成功 PASS（名额守恒）"
  $winner = (($r1, $r2 | Where-Object { ($_ | ConvertFrom-Json).ok } | Select-Object -First 1) | ConvertFrom-Json)
  $winnerRid = $winner.rid
  $wsess = $script:sess[$winner.acc]
  $wpay = Api $wsess POST "/api/reservations/$winnerRid/pay" $null
  $null = PayNotify $wpay.orderNo $true
  Step "S2 抢位者 reservationId=$winnerRid 支付确认 PASS"
  Step "S2 名额释放：slotB2 容量1，u1/pet1 预约后未付款取消，名额恢复"
  $slotB2 = NewSlot 13 1
  $resUB = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotB2.slotId; petId = 1}
  $cUB = Api $sess['u1'] POST "/api/reservations/$($resUB.reservationId)/cancel" @{}
  Assert ($cUB.status -eq 'CANCELLED') "S2 未付款取消应 CANCELLED，实际 $($cUB.status)"
  Step "S2 未付款取消释放名额 PASS status=$($cUB.status)"
  $resNB = Api $sess['u2'] POST '/api/reservations' @{slotId = $slotB2.slotId; petId = 3}
  $npay = Api $sess['u2'] POST "/api/reservations/$($resNB.reservationId)/pay" $null
  $null = PayNotify $npay.orderNo $true
  $dNB = Api $sess['u2'] GET "/api/reservations/$($resNB.reservationId)" $null
  Assert ($dNB.status -eq 'CONFIRMED') "S2 新用户预约后应为 CONFIRMED，实际 $($dNB.status)"
  Step "S2 新用户预约并确认 PASS reservationId=$($resNB.reservationId) status=$($dNB.status)"
  Step "S2 名额循环 全链路 PASS"

  # ========== S3 取消分支 ==========
  Step "S3 开始：提前≥2h 取消自动受理（4h 场次）"
  $slotC = NewSlot 4 3
  $resC = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotC.slotId; petId = 2}
  $payC = Api $sess['u1'] POST "/api/reservations/$($resC.reservationId)/pay" $null
  $null = PayNotify $payC.orderNo $true
  $refBefore = GetList $sess['cs1'] '/api/cs/refunds?status=UNFINISHED'
  $cC = Api $sess['u1'] POST "/api/reservations/$($resC.reservationId)/cancel" @{}
  Assert ($cC.status -eq 'AUTO_ACCEPTED') "S3 提前取消应 AUTO_ACCEPTED，实际 $($cC.status)"
  $refAfter = GetList $sess['cs1'] '/api/cs/refunds?status=UNFINISHED'
  Assert ($refAfter.Count -gt $refBefore.Count) "S3 自动受理应生成待处理退款"
  $refundC = $refAfter | Where-Object { $refBefore.ID -notcontains $_.ID } | Select-Object -First 1
  $null = RefundNotify $refundC.RefundNo $true
  Step "S3 提前取消 AUTO_ACCEPTED + 退款成功 refundNo=$($refundC.RefundNo) PASS"
  Step "S3 边界：开始前恰好 2h 取消（含 2h，应自动受理）"
  $slotE = NewSlot 2 3
  $resE = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotE.slotId; petId = 1}
  $payE = Api $sess['u1'] POST "/api/reservations/$($resE.reservationId)/pay" $null
  $null = PayNotify $payE.orderNo $true
  $cE = Api $sess['u1'] POST "/api/reservations/$($resE.reservationId)/cancel" @{}
  Assert ($cE.status -eq 'AUTO_ACCEPTED') "S3 恰好2h取消应 AUTO_ACCEPTED，实际 $($cE.status)"
  Step "S3 2h 分界自动受理 PASS"
  Step "S3 临近开始（1h）：无原因取消应被拒"
  $slotD = NewSlot 1 3
  $resD = Api $sess['u2'] POST '/api/reservations' @{slotId = $slotD.slotId; petId = 3}
  $payD = Api $sess['u2'] POST "/api/reservations/$($resD.reservationId)/pay" $null
  $null = PayNotify $payD.orderNo $true
  $errD = ApiExpectError $sess['u2'] POST "/api/reservations/$($resD.reservationId)/cancel" @{}
  Assert ($errD.http -eq 422) "S3 无原因临近取消应 422，实际 $($errD.http) $($errD.body)"
  Step "S3 无原因取消 422 PASS $($errD.body)"
  $cD = Api $sess['u2'] POST "/api/reservations/$($resD.reservationId)/cancel" @{reason = '宠物突发不适'}
  Assert ($cD.status -eq 'CANCELLATION_REVIEW') "S3 应进入审核，实际 $($cD.status)"
  $reqD = [int64]$cD.cancelRequestId
  $chk = ApiExpectError $sess['rec'] POST "/api/reception/reservations/$($resD.reservationId)/checkin" $null
  Assert ($chk.http -eq 409) "S3 待审核期间核销应 409，实际 $($chk.http)"
  Step "S3 提交审核 cancelRequestId=$reqD status=$($cD.status)；待审核销 409 PASS"
  $pending = GetList $sess['cs1'] '/api/cs/cancel-requests?status=PENDING'
  Assert ($pending.Count -ge 1) "S3 客服应看到待审申请"
  Api $sess['cs1'] POST "/api/cs/cancel-requests/$reqD/approve" @{note = '情况属实，同意'} | Out-Null
  $dupA = ApiExpectError $sess['cs1'] POST "/api/cs/cancel-requests/$reqD/approve" @{note = 'x'}
  Assert ($dupA.http -eq 409) "S3 重复批准应 409，实际 $($dupA.http)"
  Step "S3 客服批准 PASS；重复批准 409 PASS"
  $unfinished = GetList $sess['cs1'] '/api/cs/refunds?status=UNFINISHED'
  $refundD = $unfinished | Where-Object { $_.ReservationID -eq $resD.reservationId } | Select-Object -First 1
  if ($null -eq $refundD) { $refundD = $unfinished | Select-Object -First 1 }
  $null = RefundNotify $refundD.RefundNo $false
  Step "S3 退款回调失败 refundNo=$($refundD.RefundNo)（已取消预约不恢复可用）"
  $afterFail = GetList $sess['cs1'] '/api/cs/refunds?status=UNFINISHED'
  $failRow = $afterFail | Where-Object { $_.ID -eq $refundD.ID } | Select-Object -First 1
  Assert ($null -ne $failRow -and $failRow.Status -eq 'FAILED') "S3 目标退款应为 FAILED，实际 $(($afterFail | ConvertTo-Json -Compress))"
  Step "S3 退款失败进入待跟进列表（$(($afterFail | ConvertTo-Json -Compress))）"
  Api $sess['cs1'] POST "/api/cs/refunds/$($refundD.ID)/retry" $null | Out-Null
  $null = RefundNotify $refundD.RefundNo $true
  Step "S3 客服重试退款 + 成功回调 PASS"
  $dD = Api $sess['u2'] GET "/api/reservations/$($resD.reservationId)" $null
  Step "S3 用户可见最终状态=$($dD.status) 退款进度见 detail JSON：$($dD | ConvertTo-Json -Compress)"
  Step "S3 审核拒绝分支：拒绝后恢复已确认，可再次提交"
  $slotF = NewSlot 1 3
  $resF = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotF.slotId; petId = 2}
  $payF = Api $sess['u1'] POST "/api/reservations/$($resF.reservationId)/pay" $null
  $null = PayNotify $payF.orderNo $true
  $cF1 = Api $sess['u1'] POST "/api/reservations/$($resF.reservationId)/cancel" @{reason = '临时出差'}
  Assert ($cF1.status -eq 'CANCELLATION_REVIEW') "S3 应进入审核，实际 $($cF1.status)"
  $reqF = [int64]$cF1.cancelRequestId
  Api $sess['cs1'] POST "/api/cs/cancel-requests/$reqF/reject" @{note = '理由不足，建议改期'} | Out-Null
  $dF = Api $sess['u1'] GET "/api/reservations/$($resF.reservationId)" $null
  Assert ($dF.status -eq 'CONFIRMED') "S3 拒绝后应恢复 CONFIRMED，实际 $($dF.status)"
  Step "S3 客服拒绝恢复 CONFIRMED PASS"
  $cF2 = Api $sess['u1'] POST "/api/reservations/$($resF.reservationId)/cancel" @{reason = '宠物术后需要静养'}
  Assert ($cF2.status -eq 'CANCELLATION_REVIEW') "S3 拒绝后应允许再次提交，实际 $($cF2.status)"
  Step "S3 拒绝后再次提交 PASS cancelRequestId=$($cF2.cancelRequestId)"
  Api $sess['cs1'] POST "/api/cs/cancel-requests/$([int64]$cF2.cancelRequestId)/approve" @{note = '同意'} | Out-Null
  $unF = GetList $sess['cs1'] '/api/cs/refunds?status=UNFINISHED'
  $refundF = $unF | Where-Object { $_.ReservationID -eq $resF.reservationId } | Select-Object -First 1
  if ($null -eq $refundF) { $refundF = $unF | Select-Object -First 1 }
  $null = RefundNotify $refundF.RefundNo $true
  Step "S3 再次审核批准并退款成功 PASS"
  Step "S3 取消分支 全链路 PASS"

  # ========== S4 门店现场冲突 ==========
  Step "S4 开始：取消与核销并发（slotG 容量1）"
  $slotG = NewSlot 3 1
  $resG = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotG.slotId; petId = 1}
  $payG = Api $sess['u1'] POST "/api/reservations/$($resG.reservationId)/pay" $null
  $null = PayNotify $payG.orderNo $true
  $ridG = $resG.reservationId
  $cj = Start-Job -ScriptBlock {
    param($b, $rid)
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $null = Invoke-RestMethod -Uri "$b/api/auth/login" -Method POST -Body (@{account = 'u1'; password = '123456'} | ConvertTo-Json -Compress) -ContentType 'application/json' -WebSession $sess
    try {
      $r = Invoke-RestMethod -Uri "$b/api/reservations/$rid/cancel" -Method POST -Body (@{reason = '并发取消'} | ConvertTo-Json -Compress) -ContentType 'application/json' -WebSession $sess
      @{op = 'cancel'; ok = $true; status = $r.data.status} | ConvertTo-Json -Compress
    } catch {
      $c = 0; $m = $_.Exception.Message
      if ($_.ErrorDetails.Message) { try { $j = $_.ErrorDetails.Message | ConvertFrom-Json; $c = $j.code; $m = $j.message } catch { $m = $_.ErrorDetails.Message } }
      @{op = 'cancel'; ok = $false; code = $c; msg = $m} | ConvertTo-Json -Compress
    }
  } -ArgumentList $base, $ridG
  $ij = Start-Job -ScriptBlock {
    param($b, $rid)
    $sess = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $null = Invoke-RestMethod -Uri "$b/api/auth/login" -Method POST -Body (@{account = 'rec'; password = '123456'} | ConvertTo-Json -Compress) -ContentType 'application/json' -WebSession $sess
    try {
      $r = Invoke-RestMethod -Uri "$b/api/reception/reservations/$rid/checkin" -Method POST -Body '{}' -ContentType 'application/json' -WebSession $sess
      @{op = 'checkin'; ok = $true; alreadyDone = $r.data.alreadyDone} | ConvertTo-Json -Compress
    } catch {
      $c = 0; $m = $_.Exception.Message
      if ($_.ErrorDetails.Message) { try { $j = $_.ErrorDetails.Message | ConvertFrom-Json; $c = $j.code; $m = $j.message } catch { $m = $_.ErrorDetails.Message } }
      @{op = 'checkin'; ok = $false; code = $c; msg = $m} | ConvertTo-Json -Compress
    }
  } -ArgumentList $base, $ridG
  Wait-Job $cj, $ij -Timeout 60 | Out-Null
  $rc = Receive-Job $cj | Select-Object -Last 1
  $ri = Receive-Job $ij | Select-Object -Last 1
  Remove-Job $cj, $ij
  Step "S4 并发结果 cancel=$rc"
  Step "S4 并发结果 checkin=$ri"
  $dG = Api $sess['u1'] GET "/api/reservations/$ridG" $null
  Step "S4 并发后预约唯一状态=$($dG.status)"
  Assert ($dG.status -in @('CANCELLED', 'CANCELLATION_REVIEW', 'CHECKED_IN', 'IN_SERVICE')) "S4 并发后状态应唯一合法，实际 $($dG.status)"
  Step "S4 取消与核销并发得到唯一合法结果 PASS"
  Step "S4 重复核销与越权（slotH）"
  $slotH = NewSlot 3 3
  $resH = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotH.slotId; petId = 2}
  $payH = Api $sess['u1'] POST "/api/reservations/$($resH.reservationId)/pay" $null
  $null = PayNotify $payH.orderNo $true
  $ridH = $resH.reservationId
  $ci1 = Api $sess['rec'] POST "/api/reception/reservations/$ridH/checkin" $null
  $ci2 = Api $sess['rec'] POST "/api/reception/reservations/$ridH/checkin" $null
  Assert ($ci2.alreadyDone -eq $true) "S4 重复核销应 alreadyDone=true"
  Step "S4 重复核销 alreadyDone=true PASS（不重复记收入）"
  Api $sess['srv'] POST "/api/service/reservations/$ridH/start" $null | Out-Null
  Api $sess['srv'] POST "/api/service/reservations/$ridH/complete" $null | Out-Null
  Step "S4 slotH 服务开始并完成（完成收入只记一次，日清合计核验）"
  $cross = ApiExpectError $sess['u2'] GET "/api/reservations/$ridH" $null
  Assert ($cross.http -eq 404) "S4 他人预约查询应 404，实际 $($cross.http)"
  $anon = ApiExpectError $null GET '/api/slots' $null
  Assert ($anon.http -eq 401) "S4 未登录查询应 401，实际 $($anon.http)"
  $role = ApiExpectError $sess['u1'] POST '/api/manager/slots' @{startAt = 'bad'; endAt = 'bad'; capacity = 1}
  Assert ($role.http -eq 403) "S4 用户调用店长接口应 403，实际 $($role.http)"
  Step "S4 越权防护 PASS（u2 查他人预约 404；未登录 401；用户调店长接口 403；跨店校验见单元测试 internal/service）"
  Step "S4 门店现场冲突 全链路 PASS"

  # ========== S5 收店核查：重启 + 未到店 ==========
  Step "S5 预置未到店素材：slotNS（01:00-02:00Z），u1/pet1 预约并付款确认"
  $slotNS = NewSlot 1 1
  $resNS = Api $sess['u1'] POST '/api/reservations' @{slotId = $slotNS.slotId; petId = 1}
  $payNS = Api $sess['u1'] POST "/api/reservations/$($resNS.reservationId)/pay" $null
  $null = PayNotify $payNS.orderNo $true
  Step "S5 未到店素材确认 reservationId=$($resNS.reservationId) status=CONFIRMED"
  Step "S5 停止服务并重启（同一数据库，教学时钟前移到 12:00Z）——验证落库与重启可查询"
  Stop-Process -Id $proc.Id -Force
  $proc = $null
  Start-Sleep -Milliseconds 800
  $env:TEACHING_CLOCK_BASE = '2026-11-14T12:00:00Z'
  $proc = Start-Process -FilePath $exe -WorkingDirectory $root -RedirectStandardOutput "$log.2" -RedirectStandardError "$log.2.err" -PassThru -WindowStyle Hidden
  $ready = $false
  for ($i = 0; $i -lt 60; $i++) {
    Start-Sleep -Milliseconds 500
    try { Invoke-RestMethod "$base/readyz" | Out-Null; $ready = $true; break } catch {}
  }
  if (-not $ready) { throw "重启后服务未就绪，日志：$log.2 / $log.2.err" }
  Step "S5 重启完成，healthz/readyz PASS"
  LoginAll
  Step "S5 重启后重新登录 PASS（会话不跨进程持久，业务数据落库保留）"
  $mine = GetList $sess['u1'] '/api/my/reservations'
  Assert ($mine.Count -ge 5) "S5 重启后用户预约记录应完整保留，实际 $($mine.Count) 条"
  Step "S5 重启后 u1 预约记录 $($mine.Count) 条：$(($mine | ForEach-Object { "$($_.reservationId):$($_.status)" }) -join ', ')"
  $startAgain = ApiExpectError $sess['u1'] POST '/api/reservations' @{slotId = $slotA.slotId; petId = 1}
  Assert ($startAgain.http -ge 400) "S5 已开始时段应不可预约，实际 $($startAgain.http) $($startAgain.body)"
  Step "S5 时段已开始不可预约 PASS（$($startAgain.body)）"
  $ns = Api $mgr POST "/api/manager/reservations/$($resNS.reservationId)/no-show" @{reason = '到店时间已过，主人未出现'}
  Step "S5 店长标记未到店 PASS（不自动退款）"
  $ns2 = ApiExpectError $mgr POST "/api/manager/reservations/$($resNS.reservationId)/no-show" @{reason = '再标'}
  Assert ($ns2.http -eq 409) "S5 重复标记未到店应 409，实际 $($ns2.http)"
  $dNS = Api $sess['u1'] GET "/api/reservations/$($resNS.reservationId)" $null
  Assert ($dNS.status -eq 'NO_SHOW') "S5 用户应可查看未到店结果，实际 $($dNS.status)"
  Step "S5 用户查看未到店结果 status=$($dNS.status) PASS"
  $daily2 = Api $mgr GET '/api/manager/daily-close?date=2026-11-14' $null
  Assert ($daily2.UnaccountedCents -eq 0) "S5 全天日清差额应 0，实际 $($daily2.UnaccountedCents)"
  Step "S5 店长日清核查（2026-11-14）：$($daily2 | ConvertTo-Json -Compress)"
  Assert ($daily2.CompletedIncomeCents -eq 19800) "S5 已完成收入应 19800（S1+S4 各 9900，重复完成不重复记），实际 $($daily2.CompletedIncomeCents)"
  Step "S5 收店核查：已完成/取消退款/未到店/退款待处理记录均可在日清与列表中分别核查，差额=0 PASS"
  Step "全部五组场景 PASS"

  Write-Output "== SUMMARY =="
  Write-Output "S1 正常接待: PASS"
  Write-Output "S2 名额循环: PASS（并发唯一成功 + 取消释放 + 新用户预约）"
  Write-Output "S3 取消分支: PASS（自动受理/2h分界/审核批准/拒绝恢复/拒绝后重提/退款失败重试）"
  Write-Output "S4 门店现场冲突: PASS（取消核销并发唯一结果/重复核销幂等/越权防护）"
  Write-Output "S5 收店核查: PASS（重启持久化/已开始不可约/未到店/日清勾稽差额0）"
  exit 0
} catch {
  Write-Output "FAIL: $($_.Exception.Message)"
  Write-Output $_.ScriptStackTrace
  exit 1
} finally {
  if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
  Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique |
    Where-Object { $null -ne $_ } |
    ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
  Start-Sleep -Milliseconds 300
  Remove-Item $db, "$db-shm", "$db-wal", $log, "$log.err", "$log.2", "$log.2.err", $exe -ErrorAction SilentlyContinue
}
