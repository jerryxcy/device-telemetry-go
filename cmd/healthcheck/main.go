// healthcheck 是給 docker-compose healthcheck 用的小工具。
// 因為最終 image 沒有 curl,所以自己編一個。
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(os.Args[1])
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
