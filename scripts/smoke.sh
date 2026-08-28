#!/usr/bin/env bash
#
# device-service 的端到端煙霧測試。
#
# 它打的是真的 HTTP,所以一次驗證 handler → service → store → PostgreSQL 整條鏈。
# 單元測試用假的 Repository,驗不到 SQL 欄位順序、pgx 錯誤碼、交易這些東西。
#
#   前置:make up && make run(或 docker compose up -d --build)
#   用法:./scripts/smoke.sh [base-url]      預設 http://localhost:8080
#
# 全部通過 exit 0,任何一項失敗 exit 1。腳本只碰自己建立的 SMOKE-* 設備。

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
req GET /healthz
check "healthz 回 200" 200 "$STATUS"
req GET /readyz
check "readyz 回 200(資料庫連得上)" 200 "$STATUS"

section "2. 註冊"
req POST /devices '{"serial":"SMOKE-0001","name":"一樓溫度計","location":"1F"}'
check "回 200" 200 "$STATUS"
check "新設備一律 enabled=true" true "$(jq -r .enabled <<<"$BODY")"
check "created_at 等於 updated_at" \
	"$(jq -r .created_at <<<"$BODY")" "$(jq -r .updated_at <<<"$BODY")"
check "時間是 UTC(結尾為 Z)" Z "$(jq -r '.created_at | .[-1:]' <<<"$BODY")"

section "3. 重複註冊 —— 冪等,且不覆蓋既有欄位"
# 這一發跨三層:Create 撞 23505 → errors.As → ErrAlreadyExists
#            → Register 走冪等分支 → GetBySerial 回既有那筆。
req POST /devices '{"serial":"SMOKE-0001","name":"不該蓋掉原本的名字","location":"9F"}'
check "回 200 而不是 409" 200 "$STATUS"
check "name 沒有被覆蓋" "一樓溫度計" "$(jq -r .name <<<"$BODY")"
check "location 沒有被覆蓋" "1F" "$(jq -r .location <<<"$BODY")"
check "從 DB 讀回來的時間也是 UTC" Z "$(jq -r '.created_at | .[-1:]' <<<"$BODY")"

section "4. 查詢"
req POST /devices '{"serial":"SMOKE-0002","name":"二樓濕度計","location":"2F"}'
check "第二台註冊成功" 200 "$STATUS"

req GET /devices/SMOKE-0001
check "取單一台回 200" 200 "$STATUS"
check "serial 正確" SMOKE-0001 "$(jq -r .serial <<<"$BODY")"

req GET /devices
check "列表回 200" 200 "$STATUS"
check "列表含 SMOKE-0001" 1 "$(jq '[.[] | select(.serial=="SMOKE-0001")] | length' <<<"$BODY")"
check "依 serial 排序" sorted \
	"$(jq -r 'if (map(.serial) | sort) == map(.serial) then "sorted" else "unsorted" end' <<<"$BODY")"

req GET '/devices?limit=1'
check "limit=1 只回一筆" 1 "$(jq length <<<"$BODY")"
req GET '/devices?limit=9999'
check "limit 超過上限被夾到 200 以內" ok \
	"$(jq 'if length <= 200 then "ok" else "too many" end' -r <<<"$BODY")"

section "5. 更新"
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
req PUT /devices/SMOKE-0001 '{"name":"只送 name"}'
check "只送 name 時 location 被清空" "" "$(jq -r .location <<<"$BODY")"
check "只送 name 時 enabled 變 false" false "$(jq -r .enabled <<<"$BODY")"

section "6. 刪除 —— 冪等"
req DELETE /devices/SMOKE-0001
check "第一次刪除回 204" 204 "$STATUS"
req DELETE /devices/SMOKE-0001
check "重複刪除仍回 204" 204 "$STATUS"
req DELETE /devices/NEVER-EXISTED
check "刪不存在的也回 204" 204 "$STATUS"
req GET /devices/SMOKE-0001
check "刪完再查回 404" 404 "$STATUS"

section "7. 錯誤處理"
req POST /devices '{"serial":"","name":"x"}'
check "serial 空 → 400" 400 "$STATUS"
req POST /devices '{"serial":"SMOKE-0003","name":""}'
check "name 空 → 400" 400 "$STATUS"
req POST /devices '{"serial":"SMOKE-0003","name":"   "}'
check "name 全空白 → 400" 400 "$STATUS"
req POST /devices 'this is not json'
check "壞掉的 JSON → 400" 400 "$STATUS"
req GET /devices/DOES-NOT-EXIST
check "查不存在的設備 → 404" 404 "$STATUS"
req GET '/devices?limit=abc'
check "limit 不是整數 → 400" 400 "$STATUS"
req PATCH /devices/SMOKE-0002
check "不支援的 method → 405" 405 "$STATUS"

section "8. 收尾"
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
