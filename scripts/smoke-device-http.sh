#!/usr/bin/env bash
#
# device-service 的端到端煙霧測試。
#
# 它打的是真的 HTTP,所以一次驗證 handler → service → store → PostgreSQL 整條鏈。
# 單元測試用假的 Repository,驗不到 SQL 欄位順序、pgx 錯誤碼、交易這些東西。
#
#   前置:make run-device(或 docker compose up -d --build)
#   用法:./scripts/smoke-device-http.sh [base-url]   預設 http://localhost:8080
#
# 全部通過 exit 0,任何一項失敗 exit 1。腳本只碰自己建立的 SMOKE-* 設備。
#
# ── 手動測試 ─────────────────────────────────────────────────────────
# 每個 req 上面都附了等價的 curl,可以整行複製到終端機單獨執行:
#   curl -i ...          看 status code 與 header
#   curl -s ... | jq     看格式化過的 JSON body
# 想換位址就把 localhost:8080 換掉。

set -uo pipefail

BASE="${1:-http://localhost:8080}"
PASS=0
FAIL=0
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

if ! command -v jq >/dev/null; then
	echo "需要 jq:brew install jq" >&2
	exit 2
fi

# 先確認服務在。少了這一關,服務沒起來時會跑完全部斷言再噴一整面紅字,
# 真正的原因反而看不出來。
curl -sS -o /dev/null --max-time 3 "$BASE/healthz" 2>/dev/null || {
	echo "連不上 device-service($BASE)。先起服務:" >&2
	echo "  make up          用容器起全部" >&2
	echo "  make run-device  只在本機跑這一個" >&2
	exit 2
}

# ---------------------------------------------------------------- 輔助函式

# req METHOD PATH [JSON]  → 設定 $STATUS 與 $BODY
req() {
	local method=$1 path=$2 data=${3:-}
	if [ -n "$data" ]; then
		STATUS=$(curl -sS -o "$TMP" -w '%{http_code}' -X "$method" "$BASE$path" \
			-H 'Content-Type: application/json' -d "$data")
	else
		STATUS=$(curl -sS -o "$TMP" -w '%{http_code}' -X "$method" "$BASE$path")
	fi
	BODY=$(cat "$TMP")
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

section "1. 健康檢查"

# curl -s localhost:8080/healthz | jq
req GET /healthz
check "healthz 回 200" 200 "$STATUS"

# curl -s localhost:8080/readyz | jq
req GET /readyz
check "readyz 回 200(資料庫連得上)" 200 "$STATUS"

section "2. 註冊"

# curl -s -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' \
#   -d '{"serial":"SMOKE-0001","name":"一樓溫度計","location":"1F"}' | jq
req POST /devices '{"serial":"SMOKE-0001","name":"一樓溫度計","location":"1F"}'
check "回 200" 200 "$STATUS"
check "新設備一律 enabled=true" true "$(jq -r .enabled <<<"$BODY")"
check "created_at 等於 updated_at" \
	"$(jq -r .created_at <<<"$BODY")" "$(jq -r .updated_at <<<"$BODY")"
check "時間是 UTC(結尾為 Z)" Z "$(jq -r '.created_at | .[-1:]' <<<"$BODY")"

section "3. 重複註冊 —— 冪等,且不覆蓋既有欄位"

# 這一發跨三層:Create 撞 23505 → errors.As → ErrAlreadyExists
#            → Register 走冪等分支 → GetBySerial 回既有那筆。
#
# curl -s -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' \
#   -d '{"serial":"SMOKE-0001","name":"不該蓋掉原本的名字","location":"9F"}' | jq
req POST /devices '{"serial":"SMOKE-0001","name":"不該蓋掉原本的名字","location":"9F"}'
check "回 200 而不是 409" 200 "$STATUS"
check "name 沒有被覆蓋" "一樓溫度計" "$(jq -r .name <<<"$BODY")"
check "location 沒有被覆蓋" "1F" "$(jq -r .location <<<"$BODY")"
check "從 DB 讀回來的時間也是 UTC" Z "$(jq -r '.created_at | .[-1:]' <<<"$BODY")"

section "4. 查詢"

# curl -s -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' \
#   -d '{"serial":"SMOKE-0002","name":"二樓濕度計","location":"2F"}' | jq
req POST /devices '{"serial":"SMOKE-0002","name":"二樓濕度計","location":"2F"}'
check "第二台註冊成功" 200 "$STATUS"

# curl -s localhost:8080/devices/SMOKE-0001 | jq
req GET /devices/SMOKE-0001
check "取單一台回 200" 200 "$STATUS"
check "serial 正確" SMOKE-0001 "$(jq -r .serial <<<"$BODY")"

# curl -s localhost:8080/devices | jq
req GET /devices
check "列表回 200" 200 "$STATUS"
check "依 serial 排序" sorted \
	"$(jq -r 'if (map(.serial) | sort) == map(.serial) then "sorted" else "unsorted" end' <<<"$BODY")"

# curl -s 'localhost:8080/devices?limit=200&offset=0' | jq
#
# 分頁找,不假設 SMOKE-0001 落在第一頁。列表依 serial 排序,資料庫裡只要
# 有夠多排在 S 前面的設備(例如壓測留下的 BULK-*),第一頁就看不到它 ——
# 這條斷言本來會因此失敗,而失敗的原因跟被測的行為無關。
FOUND=0
OFFSET=0
while :; do
	req GET "/devices?limit=200&offset=$OFFSET"
	if [ "$(jq '[.[] | select(.serial=="SMOKE-0001")] | length' <<<"$BODY")" -eq 1 ]; then
		FOUND=1
		break
	fi
	# 回傳不足一整頁就代表翻到底了。
	[ "$(jq length <<<"$BODY")" -lt 200 ] && break
	OFFSET=$((OFFSET + 200))
done
check "列表(翻頁)含 SMOKE-0001" 1 "$FOUND"

# curl -s 'localhost:8080/devices?limit=1' | jq
req GET '/devices?limit=1'
check "limit=1 只回一筆" 1 "$(jq length <<<"$BODY")"

# curl -s 'localhost:8080/devices?limit=9999' | jq 'length'
req GET '/devices?limit=9999'
check "limit 超過上限被夾到 200 以內" ok \
	"$(jq 'if length <= 200 then "ok" else "too many" end' -r <<<"$BODY")"

section "5. 更新"

# curl -s -X PUT localhost:8080/devices/SMOKE-0001 \
#   -H 'Content-Type: application/json' \
#   -d '{"name":"一樓溫度計(改)","location":"1F 大廳","enabled":false}' | jq
req PUT /devices/SMOKE-0001 '{"name":"一樓溫度計(改)","location":"1F 大廳","enabled":false}'
check "回 200" 200 "$STATUS"
check "name 有更新" "一樓溫度計(改)" "$(jq -r .name <<<"$BODY")"
check "enabled 有更新(最容易漏的欄位)" false "$(jq -r .enabled <<<"$BODY")"
check "serial 不可變" SMOKE-0001 "$(jq -r .serial <<<"$BODY")"
UPDATED_AT=$(jq -r .updated_at <<<"$BODY")
CREATED_AT=$(jq -r .created_at <<<"$BODY")
check "updated_at 晚於 created_at" later \
	"$(if [[ "$UPDATED_AT" > "$CREATED_AT" ]]; then echo later; else echo "not-later"; fi)"

# PUT 是整份取代:沒送的欄位會變成零值。這是正確行為,不是 bug。
#
# curl -s -X PUT localhost:8080/devices/SMOKE-0001 \
#   -H 'Content-Type: application/json' \
#   -d '{"name":"只送 name"}' | jq
req PUT /devices/SMOKE-0001 '{"name":"只送 name"}'
check "只送 name 時 location 被清空" "" "$(jq -r .location <<<"$BODY")"
check "只送 name 時 enabled 變 false" false "$(jq -r .enabled <<<"$BODY")"

section "6. 刪除 —— 冪等"

# curl -i -X DELETE localhost:8080/devices/SMOKE-0001
req DELETE /devices/SMOKE-0001
check "第一次刪除回 204" 204 "$STATUS"

# 同一行再跑一次 —— 客戶端重送 DELETE 時必須一樣成功,不能變 404。
# curl -i -X DELETE localhost:8080/devices/SMOKE-0001
req DELETE /devices/SMOKE-0001
check "重複刪除仍回 204" 204 "$STATUS"

# curl -i -X DELETE localhost:8080/devices/NEVER-EXISTED
req DELETE /devices/NEVER-EXISTED
check "刪不存在的也回 204" 204 "$STATUS"

# curl -i localhost:8080/devices/SMOKE-0001
req GET /devices/SMOKE-0001
check "刪完再查回 404" 404 "$STATUS"

section "7. 錯誤處理"

# curl -i -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' -d '{"serial":"","name":"x"}'
req POST /devices '{"serial":"","name":"x"}'
check "serial 空 → 400" 400 "$STATUS"

# curl -i -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' -d '{"serial":"SMOKE-0003","name":""}'
req POST /devices '{"serial":"SMOKE-0003","name":""}'
check "name 空 → 400" 400 "$STATUS"

# curl -i -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' -d '{"serial":"SMOKE-0003","name":"   "}'
req POST /devices '{"serial":"SMOKE-0003","name":"   "}'
check "name 全空白 → 400" 400 "$STATUS"

# 壞掉的 JSON 走的是 handler 自己那條 400,不是 writeError ——
# json.SyntaxError 不滿足 errors.Is(err, ErrInvalidInput)。
# curl -i -X POST localhost:8080/devices \
#   -H 'Content-Type: application/json' -d 'this is not json'
req POST /devices 'this is not json'
check "壞掉的 JSON → 400" 400 "$STATUS"

# curl -i localhost:8080/devices/DOES-NOT-EXIST
req GET /devices/DOES-NOT-EXIST
check "查不存在的設備 → 404" 404 "$STATUS"

# curl -i 'localhost:8080/devices?limit=abc'
req GET '/devices?limit=abc'
check "limit 不是整數 → 400" 400 "$STATUS"

# 405 是 Go 1.22 ServeMux 自動處理的,回應會帶 Allow header。
# curl -i -X PATCH localhost:8080/devices/SMOKE-0002
req PATCH /devices/SMOKE-0002
check "不支援的 method → 405" 405 "$STATUS"

section "8. 收尾"

# curl -i -X DELETE localhost:8080/devices/SMOKE-0002
req DELETE /devices/SMOKE-0002
check "清掉測試資料" 204 "$STATUS"

# ---------------------------------------------------------------- 結果

printf '\n'
if [ "$FAIL" -eq 0 ]; then
	printf '\033[32m全部通過:%d 項\033[0m\n' "$PASS"
	exit 0
fi
printf '\033[31m失敗 %d 項\033[0m(通過 %d 項)\n' "$FAIL" "$PASS"
exit 1
