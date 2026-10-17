package healthcheck

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func Run(url string, out io.Writer) int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(out, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(out, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

func LocalURL(listen, path string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		port = listen
	}
	return "http://127.0.0.1:" + port + path
}
