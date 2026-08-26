package httpapi

import "net/http"

// TODO(day1): 五個 handler 自己實作。
//
// 每支的固定套路:
//   1. 從 r.PathValue("id") 取路徑參數(Go 1.22+ 內建,不需要第三方套件)
//   2. 用 json.NewDecoder(r.Body).Decode(&req) 解請求;請自己定義 request struct,
//      不要直接把 device.Device 當輸入 —— 否則 client 可以偽造 ID 和 CreatedAt
//   3. 呼叫 h.svc.XXX(r.Context(), ...)
//   4. 成功用 writeJSON,失敗一律丟給 writeError(它已經寫好錯誤對應了)
//
// 寫完記得問自己:handler 裡有沒有出現任何業務規則?有的話該搬去 service 層。

func (h *Handler) createDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request) {
	// 提示:limit/offset 從 r.URL.Query() 取,記得處理解析失敗與預設值
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

func (h *Handler) getDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

func (h *Handler) updateDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}

func (h *Handler) deleteDevice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, errorBody{Error: "not implemented"})
}
