#!/usr/bin/env bash
#
# device-service 的 gRPC 介面端到端測試。
#
# 對照 smoke-device-http.sh —— 兩者打的是同一個 device.Service,只是換一層傳輸協定。
# 這支特別驗證那條界線:「設備沒註冊」是一個答案(回傳值),
# 「查詢失敗」才是錯誤(gRPC status code)。搞混的話 telemetry-service
# 會把資料庫故障誤判成「這台設備沒註冊」而安靜地丟掉讀數。
#
#   前置:make run-device,以及 brew install grpcurl
#   用法:./scripts/smoke-device-grpc.sh [http-url] [grpc-addr]
#         預設 http://localhost:8080 與 localhost:9090
#
# 全部通過 exit 0,任何一項失敗 exit 1。腳本只碰自己建立的 GSMOKE-* 設備。
#
# ── 手動測試 ─────────────────────────────────────────────────────────
# 每個呼叫上面都附了等價的指令,可以整行複製執行:
#   grpcurl -plaintext localhost:9090 list        列出服務(靠 server reflection)
#   grpcurl -plaintext -d '{...}' localhost:9090 device.v1.DeviceService/CheckEligibility
# 設備的建立與停用走 HTTP,因為 gRPC 這邊只開了查詢。

set -uo pipefail

BASE="${1:-http://localhost:8080}"
GRPC="${2:-localhost:9090}"
METHOD=device.v1.DeviceService/CheckEligibility
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

# 先確認服務在。少了這一關,服務沒起來時會跑完全部斷言再噴一整面紅字,
# 真正的原因反而看不出來。
grpcurl -plaintext "$GRPC" list >/dev/null 2>&1 || {
	echo "連不上 device-service 的 gRPC($GRPC)。先起服務:" >&2
	echo "  make up          用容器起全部" >&2
	echo "  make run-device  只在本機跑這一個" >&2
	exit 2
}

# ---------------------------------------------------------------- 輔助函式

# grpc JSON → 設定 $CODE(OK 或 gRPC 錯誤碼)與 $BODY
grpc() {
	if BODY=$(grpcurl -plaintext -d "$1" "$GRPC" "$METHOD" 2>"$ERR"); then
		CODE=OK
	else
		# 失敗時 grpcurl 印到 stderr:  ERROR:\n  Code: InvalidArgument\n  Message: ...
		CODE=$(sed -n 's/^[[:space:]]*Code:[[:space:]]*//p' "$ERR" | head -1)
		BODY=$(cat "$ERR")
		[ -n "$CODE" ] || CODE=UNKNOWN
	fi
}

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

# ---------------------------------------------------------------- 開始

section "1. server reflection"
# reflection 讓 grpcurl 不必手動指定 .proto 檔,地位等同 HTTP 那邊能直接 curl。
# 正式環境通常會關掉。
#
# grpcurl -plaintext localhost:9090 list
SERVICES=$(grpcurl -plaintext "$GRPC" list 2>&1)
check "列得出 device.v1.DeviceService" 1 \
	"$(grep -c '^device\.v1\.DeviceService$' <<<"$SERVICES")"

# grpcurl -plaintext localhost:9090 describe device.v1.DeviceService
check "服務裡有 CheckEligibility" 1 \
	"$(grpcurl -plaintext "$GRPC" describe device.v1.DeviceService 2>&1 |
		grep -c 'rpc CheckEligibility')"

# 有了 reflection,grpcurl 會先取得完整的服務描述,所以呼叫不存在的方法
# 在本機就被擋下 —— 根本不會發出 RPC,也因此沒有 gRPC status code。
# 這跟 HTTP 那邊要送出去才知道 404 不一樣。
#
# grpcurl -plaintext -d '{"serial":"X"}' localhost:9090 device.v1.DeviceService/NoSuchMethod
check "不存在的方法在 client 端就被擋下" 1 \
	"$(grpcurl -plaintext -d '{"serial":"X"}' "$GRPC" device.v1.DeviceService/NoSuchMethod 2>&1 |
		grep -c 'does not include a method named')"

section "2. 已註冊且啟用 → ELIGIBLE"
# 用 HTTP 建立、用 gRPC 查詢 —— 這一發同時證明兩個 server 共用同一個 device.Service。
#
# curl -s -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' \
#   -d '{"serial":"GSMOKE-0001","name":"gRPC 測試機","location":"1F"}' | jq
http POST /devices '{"serial":"GSMOKE-0001","name":"gRPC 測試機","location":"1F"}'
check "HTTP 註冊成功" 200 "$STATUS"

# grpcurl -plaintext -d '{"serial":"GSMOKE-0001"}' \
#   localhost:9090 device.v1.DeviceService/CheckEligibility
grpc '{"serial":"GSMOKE-0001"}'
check "呼叫成功(沒有 gRPC 錯誤)" OK "$CODE"
check "回 ELIGIBILITY_ELIGIBLE" ELIGIBILITY_ELIGIBLE "$(jq -r .eligibility <<<"$BODY")"

section "3. 停用後 → DISABLED"
# PUT 是整份取代,所以 name/location 也要一起送回去。
#
# curl -s -X PUT localhost:8080/devices/GSMOKE-0001 \
#   -H 'Content-Type: application/json' \
#   -d '{"name":"gRPC 測試機","location":"1F","enabled":false}' | jq
http PUT /devices/GSMOKE-0001 '{"name":"gRPC 測試機","location":"1F","enabled":false}'
check "HTTP 停用成功" 200 "$STATUS"

# grpcurl -plaintext -d '{"serial":"GSMOKE-0001"}' \
#   localhost:9090 device.v1.DeviceService/CheckEligibility
grpc '{"serial":"GSMOKE-0001"}'
check "呼叫成功" OK "$CODE"
check "回 ELIGIBILITY_DISABLED" ELIGIBILITY_DISABLED "$(jq -r .eligibility <<<"$BODY")"

section "4. 未註冊 → NOT_REGISTERED,而且是「答案」不是「錯誤」"
# 這是整支腳本最重要的一節。
#
# svc.Get 回 device.ErrNotFound,在 Go 那邊是 err != nil,很自然會想丟成
# codes.NotFound。但語意上「這台設備沒註冊」是一個明確的答案,要走回傳值;
# 只有「我查不到答案」(DB 掛了)才走 status.Error。
#
# 弄反的話,telemetry-service 收到資料庫故障會誤判成「這台設備沒註冊」,
# 然後安靜地丟掉讀數 —— 而且外表看起來一切正常。
#
# grpcurl -plaintext -d '{"serial":"GSMOKE-NEVER-EXISTED"}' \
#   localhost:9090 device.v1.DeviceService/CheckEligibility
grpc '{"serial":"GSMOKE-NEVER-EXISTED"}'
check "沒有回 gRPC 錯誤" OK "$CODE"
check "回 ELIGIBILITY_NOT_REGISTERED" ELIGIBILITY_NOT_REGISTERED "$(jq -r .eligibility <<<"$BODY")"

section "5. 刪掉之後 → NOT_REGISTERED"
# curl -i -X DELETE localhost:8080/devices/GSMOKE-0001
http DELETE /devices/GSMOKE-0001
check "HTTP 刪除成功" 204 "$STATUS"

# grpcurl -plaintext -d '{"serial":"GSMOKE-0001"}' \
#   localhost:9090 device.v1.DeviceService/CheckEligibility
grpc '{"serial":"GSMOKE-0001"}'
check "刪除後回 NOT_REGISTERED" ELIGIBILITY_NOT_REGISTERED "$(jq -r .eligibility <<<"$BODY")"

section "6. 錯誤處理"
# 空 serial 是呼叫端的 bug,不是「一台沒註冊的設備」—— 走錯誤那條。
# 而且 RegisterInput.Validate 保證空 serial 永遠註冊不進去,
# 回 NOT_REGISTERED 等於用一個永遠成立的答案掩蓋掉對方的錯誤。
#
# grpcurl -plaintext -d '{"serial":""}' \
#   localhost:9090 device.v1.DeviceService/CheckEligibility
grpc '{"serial":""}'
check "空 serial → InvalidArgument" InvalidArgument "$CODE"

# 欄位沒帶等同空字串 —— proto3 沒有「沒送」這回事,沒送就是零值。
#
# grpcurl -plaintext -d '{}' \
#   localhost:9090 device.v1.DeviceService/CheckEligibility
grpc '{}'
check "省略 serial 也是 InvalidArgument" InvalidArgument "$CODE"

# ---------------------------------------------------------------- 結果

printf '\n'
if [ "$FAIL" -eq 0 ]; then
	printf '\033[32m全部通過:%d 項\033[0m\n' "$PASS"
	exit 0
fi
printf '\033[31m失敗 %d 項\033[0m(通過 %d 項)\n' "$FAIL" "$PASS"
exit 1
