package httpapi

import "net/http"

// TODO(day1): 五個 handler 自己實作。
//
// 每支的固定套路:
//   1. 路徑參數用 r.PathValue("serial") 取(Go 1.22+ 內建)
//   2. 請求主體用 json.NewDecoder(r.Body).Decode(&in) 解到 device.RegisterInput
//      或 device.UpdateInput —— 那兩個型別就是為了這裡而存在的
//   3. 呼叫 h.svc.XXX(r.Context(), ...)
//   4. 成功用 writeJSON,失敗一律丟給 writeError(錯誤對應已經寫好了)
//
// 寫完問自己:handler 裡有沒有出現任何業務規則?有的話該搬去 service 層。

// registerDevice 對應 POST /devices。冪等,所以重複註冊回 200 而不是 409。
func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

// listDevices 提示:limit/offset 從 r.URL.Query() 取,記得處理解析失敗與預設值。
func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

func (h *Handler) getDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

// updateDevice 對應 PUT /devices/{serial}。整份取代 Name / Location / Lifecycle。
func (h *Handler) updateDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

// retireDevice 對應 DELETE /devices/{serial}。語意是退役,不是刪除。
// 冪等,所以成功一律 204,不管它本來是不是已經退役了。
func (h *Handler) retireDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}
