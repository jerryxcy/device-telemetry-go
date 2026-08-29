#!/usr/bin/env bash
#
# telemetry-service 的端到端測試 —— 跨兩個服務。
#
# 設備上報 ──gRPC──► telemetry-service ──gRPC──► device-service ──► PostgreSQL
#
# 它同時驗證兩層拒絕的分界:
#   整批的前提不成立(未註冊、已停用) → gRPC 錯誤碼,連 results 都沒有
#   單筆的問題(時鐘、資料不合法)     → 正常回應,結果放在對應的 ReadingResult
#
#   前置:make up、兩個服務都要跑,以及 brew install grpcurl
#         終端機 A:make run
#         終端機 B:GRPC_ADDR=:9091 go run ./cmd/telemetry-service
#   用法:./scripts/smoke-telemetry-grpc.sh [http-url] [device-grpc] [telemetry-grpc]
#         預設 http://localhost:8080 / localhost:9090 / localhost:9091
#
# 全部通過 exit 0,任何一項失敗 exit 1。腳本只碰自己建立的 TSMOKE-* 設備,
# 收尾時連同讀數一起刪掉(走的正是 ADR-0003 的應用層交易刪除)。
#
# ── 手動測試 ─────────────────────────────────────────────────────────
# 每個呼叫上面都附了等價的指令。時間戳用 $NOW 是為了讓重跑不會撞到
# 上一輪的資料;手動執行時自己代一個 UTC 時間即可,例如:
#   NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

set -uo pipefail

BASE="${1:-http://localhost:8080}"
DEVICE_GRPC="${2:-localhost:9090}"
TELEMETRY_GRPC="${3:-localhost:9091}"
SUBMIT=telemetry.v1.TelemetryService/SubmitReadings

SERIAL=TSMOKE-0001
# 整輪共用同一個時間戳,重複偵測才有確定的行為。
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
# 這兩個用字面值,才不必處理 date 在 macOS 與 Linux 上不同的參數。
TOO_OLD=2020-01-01T00:00:00Z
TOO_FUTURE=2099-01-01T00:00:00Z

PASS=0
FAIL=0
TMP="$(mktemp)"
ERR="$(mktemp)"
trap 'rm -f "$TMP" "$ERR"' EXIT

for t in grpcurl jq curl; do
	command -v "$t" >/dev/null || {
		echo "需要 $t:brew install $t" >&2
		exit 2
	}
done

# ---------------------------------------------------------------- 輔助函式

# submit JSON → 設定 $CODE(OK 或 gRPC 錯誤碼)與 $BODY
submit() {
	if BODY=$(grpcurl -plaintext -d "$1" "$TELEMETRY_GRPC" "$SUBMIT" 2>"$ERR"); then
		CODE=OK
	else
		CODE=$(sed -n 's/^[[:space:]]*Code:[[:space:]]*//p' "$ERR" | head -1)
		BODY=$(cat "$ERR")
		[ -n "$CODE" ] || CODE=UNKNOWN
	fi
}

# statuses → 把 results 的 status 壓成一行,例如 ACCEPTED,DUPLICATE
statuses() { jq -r '[.results[].status] | join(",")' <<<"$BODY" 2>/dev/null; }

# http METHOD PATH [JSON] → 設定 $STATUS。只用來擺設備狀態,不做斷言。
http() {
	local method=$1 path=$2 data=${3:-}
	if [ -n "$data" ]; then
		STATUS=$(curl -sS -o "$TMP" -w '%{http_code}' -X "$method" "$BASE$path" \
			-H 'Content-Type: application/json' -d "$data")
	else
		STATUS=$(curl -sS -o "$TMP" -w '%{http_code}' -X "$method" "$BASE$path")
	fi
}

check() { # check 說明 期望 實際
	if [ "$2" = "$3" ]; then
		printf '  \033[32m✓\033[0m %s\n' "$1"
		PASS=$((PASS + 1))
	else
		printf '  \033[31m✗\033[0m %s — 期望 %s,實際 %s\n' "$1" "$2" "$3"
		FAIL=$((FAIL + 1))
	fi
}

section() { printf '\n\033[1m%s\033[0m\n' "$1"; }

# ---------------------------------------------------------------- 前置檢查

for pair in "$BASE/healthz|device-service HTTP" ; do
	url=${pair%%|*}
	curl -sS -o /dev/null --max-time 3 "$url" 2>/dev/null || {
		echo "連不上 ${pair##*|}($url):make run 起來了嗎?" >&2
		exit 2
	}
done
grpcurl -plaintext "$TELEMETRY_GRPC" list >/dev/null 2>&1 || {
	echo "連不上 telemetry-service($TELEMETRY_GRPC):" >&2
	echo "  GRPC_ADDR=:9091 go run ./cmd/telemetry-service" >&2
	exit 2
}

# 上一輪若中途失敗,殘留的設備會讓重複偵測失準。先清掉。
http DELETE "/devices/$SERIAL"

# ---------------------------------------------------------------- 開始

section "1. 兩個服務都在"
# grpcurl -plaintext localhost:9091 list
check "telemetry-service 列得出 TelemetryService" 1 \
	"$(grpcurl -plaintext "$TELEMETRY_GRPC" list 2>&1 | grep -c '^telemetry\.v1\.TelemetryService$')"

# grpcurl -plaintext localhost:9090 list
check "device-service 列得出 DeviceService" 1 \
	"$(grpcurl -plaintext "$DEVICE_GRPC" list 2>&1 | grep -c '^device\.v1\.DeviceService$')"

section "2. 未註冊的設備 → 整批拒絕"
# 注意:這裡連 results 都沒有。設備該做的是先去註冊,不是重送。
#
# grpcurl -plaintext -d '{"serial":"TSMOKE-0001","readings":[
#   {"recordedAt":"2026-01-01T00:00:00Z","metric":"temperature","value":21.5}]}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"$SERIAL\",\"readings\":[{\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":21.5}]}"
check "回 FailedPrecondition" FailedPrecondition "$CODE"

section "3. 註冊之後可以上報"
# curl -s -X POST localhost:8080/devices -H 'Content-Type: application/json' \
#   -d '{"serial":"TSMOKE-0001","name":"上報測試機","location":"1F"}' | jq
http POST /devices "{\"serial\":\"$SERIAL\",\"name\":\"上報測試機\",\"location\":\"1F\"}"
check "HTTP 註冊成功" 200 "$STATUS"

# grpcurl -plaintext -d '{"serial":"TSMOKE-0001","readings":[
#   {"recordedAt":"...","metric":"temperature","value":21.5},
#   {"recordedAt":"...","metric":"humidity","value":60}]}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"$SERIAL\",\"readings\":[
  {\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":21.5},
  {\"recordedAt\":\"$NOW\",\"metric\":\"humidity\",\"value\":60}
]}"
check "呼叫成功" OK "$CODE"
check "兩筆都是 ACCEPTED" \
	"READING_STATUS_ACCEPTED,READING_STATUS_ACCEPTED" "$(statuses)"

section "4. 重送 → DUPLICATE(冪等)"
# 設備因逾時重送時,唯一鍵 (serial, recorded_at, metric) 必須重現,平台安靜地吸收。
# 這也順便證明了上一節真的寫進資料庫了 —— 沒寫進去的話這裡會是 ACCEPTED。
#
# 指令跟上一節一模一樣,原封不動再送一次。
submit "{\"serial\":\"$SERIAL\",\"readings\":[
  {\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":21.5},
  {\"recordedAt\":\"$NOW\",\"metric\":\"humidity\",\"value\":60}
]}"
check "呼叫成功" OK "$CODE"
check "兩筆都是 DUPLICATE" \
	"READING_STATUS_DUPLICATE,READING_STATUS_DUPLICATE" "$(statuses)"

section "5. 同一批新舊混雜"
# 逐筆判定要各自正確,不能整批同一個答案。
#
# grpcurl -plaintext -d '{"serial":"TSMOKE-0001","readings":[
#   {"recordedAt":"...","metric":"temperature","value":21.5},
#   {"recordedAt":"...","metric":"pressure","value":1013}]}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"$SERIAL\",\"readings\":[
  {\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":21.5},
  {\"recordedAt\":\"$NOW\",\"metric\":\"pressure\",\"value\":1013}
]}"
check "DUPLICATE 之後接 ACCEPTED" \
	"READING_STATUS_DUPLICATE,READING_STATUS_ACCEPTED" "$(statuses)"

section "6. 逐筆拒絕 —— 其他筆照常寫入"
# 這一節是「兩層拒絕」的下半:被拒的四筆不會影響第一筆寫入。
# 被拒的在打資料庫之前就被濾掉了,否則一筆壞資料會讓整批回滾。
#
# grpcurl -plaintext -d '{"serial":"TSMOKE-0001","readings":[
#   {"recordedAt":"...","metric":"lux","value":800},
#   {"recordedAt":"2020-01-01T00:00:00Z","metric":"temperature","value":1},
#   {"recordedAt":"2099-01-01T00:00:00Z","metric":"temperature","value":2},
#   {"recordedAt":"...","metric":"","value":3},
#   {"metric":"temperature","value":4}]}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"$SERIAL\",\"readings\":[
  {\"recordedAt\":\"$NOW\",\"metric\":\"lux\",\"value\":800},
  {\"recordedAt\":\"$TOO_OLD\",\"metric\":\"temperature\",\"value\":1},
  {\"recordedAt\":\"$TOO_FUTURE\",\"metric\":\"temperature\",\"value\":2},
  {\"recordedAt\":\"$NOW\",\"metric\":\"\",\"value\":3},
  {\"metric\":\"temperature\",\"value\":4}
]}"
check "呼叫成功(逐筆拒絕不影響整批)" OK "$CODE"
check "五筆各自的結果" \
	"READING_STATUS_ACCEPTED,READING_STATUS_CLOCK_OUT_OF_RANGE,READING_STATUS_CLOCK_OUT_OF_RANGE,READING_STATUS_INVALID,READING_STATUS_INVALID" \
	"$(statuses)"
# 沒帶 recordedAt 必須是 INVALID 而不是 CLOCK_OUT_OF_RANGE ——
# nil 的 Timestamp 轉成 time.Time 是 1970-01-01(不是零值),
# 直接轉的話會被誤判成「時鐘太舊」。
check "沒帶 recordedAt 的原因是資料不合法" "invalid reading: recorded_at is required" \
	"$(jq -r '.results[4].message' <<<"$BODY")"

section "7. 停用之後 → 整批拒絕"
# curl -s -X PUT localhost:8080/devices/TSMOKE-0001 -H 'Content-Type: application/json' \
#   -d '{"name":"上報測試機","location":"1F","enabled":false}' | jq
http PUT "/devices/$SERIAL" "{\"name\":\"上報測試機\",\"location\":\"1F\",\"enabled\":false}"
check "HTTP 停用成功" 200 "$STATUS"

# grpcurl -plaintext -d '{"serial":"TSMOKE-0001","readings":[
#   {"recordedAt":"...","metric":"temperature","value":1}]}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"$SERIAL\",\"readings\":[{\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":1}]}"
check "回 PermissionDenied(不是 FailedPrecondition)" PermissionDenied "$CODE"

section "8. 參數錯誤"
# grpcurl -plaintext -d '{"serial":"","readings":[
#   {"recordedAt":"...","metric":"temperature","value":1}]}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"\",\"readings\":[{\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":1}]}"
check "空 serial → InvalidArgument" InvalidArgument "$CODE"

# 空批次是合法的,只是什麼都不做 —— 不查資格、不打資料庫。
# grpcurl -plaintext -d '{"serial":"TSMOKE-0001"}' \
#   localhost:9091 telemetry.v1.TelemetryService/SubmitReadings
submit "{\"serial\":\"$SERIAL\"}"
check "空批次成功且無結果" OK "$CODE"
check "results 是空的" 0 "$(jq -r '.results | length' <<<"$BODY" 2>/dev/null || echo 0)"

section "9. 收尾"
# 刪除設備會連同它的讀數一起刪 —— 由應用層在單一交易裡完成,
# 而非 ON DELETE CASCADE。見 docs/adr/0003。
#
# curl -i -X DELETE localhost:8080/devices/TSMOKE-0001
http DELETE "/devices/$SERIAL"
check "刪除設備與其讀數" 204 "$STATUS"

# 讀數被連帶刪掉之後,同樣的內容再送就會是全新的 —— 但設備已經不存在,
# 所以會回到第 2 節的狀態。
submit "{\"serial\":\"$SERIAL\",\"readings\":[{\"recordedAt\":\"$NOW\",\"metric\":\"temperature\",\"value\":21.5}]}"
check "刪除後回到未註冊" FailedPrecondition "$CODE"

# ---------------------------------------------------------------- 結果

printf '\n'
if [ "$FAIL" -eq 0 ]; then
	printf '\033[32m全部通過:%d 項\033[0m\n' "$PASS"
	exit 0
fi
printf '\033[31m失敗 %d 項\033[0m(通過 %d 項)\n' "$FAIL" "$PASS"
exit 1
