package main

import (
	"log"
	"io"
	"fmt"
	"time"
	"net"
	"strings"
	"net/http"
	"spx/utils"
	"spx/config"
)

var proxyCredentials string
var proxyCredentialsBase64 string

var (
	FILTER_HEADERS = []string{
		"Prxoy-Authenticate",
		"Proxy-Connection",
		"Transfer-Encoding",
		"Upgrade",
	}
)

func transfer(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
}

func copyHeader(dst, src http.Header) {
	for key, vv := range src {
		for _, val := range vv {
			dst.Add(key, val)
		}
	}
}

func filterProxyHeaders(header http.Header) {
	for _, key := range FILTER_HEADERS {
		header.Del(key)
	}
}

func connectTunnelHandler(w http.ResponseWriter, r *http.Request) {
	proxy_conn, err := net.DialTimeout("tcp4", r.Host, 10 * time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Send 200 Connection established
	w.WriteHeader(http.StatusOK)

	// hijack connnection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Proxy hijack error", http.StatusServiceUnavailable)
		return
	}
	client_conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	go transfer(proxy_conn, client_conn)
	go transfer(client_conn, proxy_conn)
}

func httpHandler(w http.ResponseWriter, r *http.Request) {
	filterProxyHeaders(r.Header)
	proxy_resp, err := DefaultProxyTransport.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer proxy_resp.Body.Close()

	copyHeader(w.Header(), proxy_resp.Header)
	w.WriteHeader(proxy_resp.StatusCode)
	io.Copy(w, proxy_resp.Body)
}

func ProxyAuthenticateHandler(handle http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func (w http.ResponseWriter, r *http.Request) {
		if len(proxyCredentials) == 0 {
			handle(w, r)
			return
		}

		// Need authorization
		credientials := strings.TrimPrefix(r.Header.Get("Proxy-Authorization"), "Basic ")
		log.Println(credientials)
		log.Println(proxyCredentials)
		if credientials == "" || (credientials != proxyCredentials && credientials != proxyCredentialsBase64){
			w.Header().Set("Proxy-Authenticate", "Basic realm=\"Password\"")
			http.Error(w, "", http.StatusProxyAuthRequired)
			log.Println("Accessing proxy deny, password is wrong or empty")
			return
		}
		handle(w, r)
	})
}

func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		log.Printf("%s %s", r.Method, r.Host)
		connectTunnelHandler(w, r)
	} else {
		log.Printf("%s %s", r.Method, r.URL)
		httpHandler(w, r)
	}
}

func NewProxyServerFromPort(port int) http.Server {
	addr := fmt.Sprintf(":%d", port)
	proxyHandler := ProxyAuthenticateHandler(ProxyHandler)
	return http.Server{
		Addr: addr,
		Handler: proxyHandler,
	}
}

func NewProxyServer() http.Server {
	appConfig := config.GetInstance()
	proxyUser := appConfig.AuthUser
	proxyPassword := appConfig.AuthPassword

	if proxyUser != "" && proxyPassword != "" {
		proxyCredentials = fmt.Sprintf("%s:%s", proxyUser, proxyPassword)
	}
	if len(proxyCredentials) > 0 {
		proxyCredentialsBase64 = utils.Base64Encode(proxyCredentials)
	}

	return NewProxyServerFromPort(appConfig.Port)
}
