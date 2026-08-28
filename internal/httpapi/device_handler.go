package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jerryxcy/device-telemetry-go/internal/device"
)

// 這一層的固定套路:
//   1. 路徑參數用 r.PathValue("serial") 取(Go 1.22+ 內建)
//   2. 請求主體用 json.NewDecoder(r.Body).Decode(&in) 解到 device.RegisterInput
//      或 device.UpdateInput —— 那兩個型別就是為了這裡而存在的
//   3. 呼叫 h.svc.XXX(r.Context(), ...)
//   4. 成功用 writeJSON,失敗一律丟給 writeError(錯誤對應在 response.go)
//
// handler 裡不該出現任何業務規則 —— 驗證、預設值、狀態判斷都屬於 service 層。

// registerDevice 對應 POST /devices。冪等,所以重複註冊回 200 而不是 409。
func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	var in device.RegisterInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid JSON body"})
		return
	}
	d, err := h.svc.Register(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, d)
}

// queryInt 取出一個整數查詢參數。
// 沒帶就回 0 —— 交給 service 套預設值,handler 不重複那條規則。
// 帶了但不是數字,那是客戶端的錯,回 false 讓呼叫端回 400。
func queryInt(r *http.Request, key string) (int, bool) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// listDevices 對應 GET /devices。
//
// limit/offset 在這裡只做「字串 → int」的轉換:沒帶就傳 0 讓 service 套預設值,
// 帶了但不是整數則回 400。預設 50、上限 200 這些規則住在 service。
func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request) {
	limit, ok := queryInt(r, "limit")
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "limit must be an integer"})
		return
	}
	offset, ok := queryInt(r, "offset")
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "offset must be an integer"})
		return
	}
	devices, err := h.svc.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

// getDevice 對應 GET /devices/{serial}。設備不存在時回 404。
func (h *Handler) getDevice(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Get(r.Context(), r.PathValue("serial"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, d)
}

// updateDevice 對應 PUT /devices/{serial}。整份取代 Name / Location / Enabled。
func (h *Handler) updateDevice(w http.ResponseWriter, r *http.Request) {
	var in device.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid JSON body"})
		return
	}
	d, err := h.svc.Update(r.Context(), r.PathValue("serial"), in)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, d)
}

// deleteDevice 對應 DELETE /devices/{serial}。連同該設備的讀數一起刪除。
// 冪等,所以成功一律 204,不管那台設備本來存不存在。
func (h *Handler) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("serial")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
