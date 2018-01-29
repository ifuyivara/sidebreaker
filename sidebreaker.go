package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/rubyist/circuitbreaker"
)

// Host struct for the configuration
type Host struct {
	Host      string  `json:"host"`
	BreakType string  `json:"breakType"`
	Timeout   int     `json:"timeout"`
	Threshold int64   `json:"threshold"`
	Rate      float64 `json:"rate"`
}

// Configuration struct, contains an array of hosts
type Configuration struct {
	Port    int    `json:"port"`
	Verbose bool   `json:"verbose"`
	Hosts   []Host `json:"Hosts"`
}

// Breakers struct, each host in the configuration will get it's own circuit breaker
type Breakers struct {
	Host    Host
	Breaker *circuit.Breaker
}

func main() {

	// Load sidebreaker configuration file
	file, _ := os.Open("config.json")
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err := decoder.Decode(&configuration)
	if err != nil {
		log.Println("error loading sidebreaker configuration:", err)
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}

	log.Println("Starting sidebreaker...")
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = configuration.Verbose

	// Initialize the circuit breakers according to their configuration
	// Create a map with the hostname as the key for fast access
	hostMap := map[string]Breakers{}
	for _, v := range configuration.Hosts {
		breaker := circuit.NewBreaker()
		switch v.BreakType {
		case "consecutive":
			breaker = circuit.NewConsecutiveBreaker(v.Threshold)
			break
		case "threshold":
			breaker = circuit.NewThresholdBreaker(v.Threshold)
			break
		case "rate":
			breaker = circuit.NewRateBreaker(v.Rate/100, 100)
			break
		default:
			breaker = circuit.NewConsecutiveBreaker(5)
		}
		hostMap[v.Host] = Breakers{v, breaker}
	}

	// Only hijack CONNECT requests of hosts that are present in our configuration.
	// We will inspect the request and make a decision based on the hostname
	proxy.OnRequest(isHostInConfig(hostMap)).HijackConnect(func(req *http.Request, client net.Conn, ctx *goproxy.ProxyCtx) {

		host := hostMap[req.URL.Hostname()]
		// Use the circuit breaker for this host
		if host.Breaker.Ready() {

			clientBuf := bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client))
			remote, err := net.DialTimeout("tcp", req.URL.Host, time.Duration(host.Host.Timeout)*time.Millisecond)

			// If the initial connection errors out or timesout return an error to the client and mark the fail in the breaker
			if err != nil {
				host.Breaker.Fail()
				ctx.Warnf("error connecting to remote: %v", err)
				client.Write([]byte("HTTP/1.1 500 Cannot reach destination\r\n\r\n"))
				client.Close()
				return
			}

			ctx.Logf("Accepting CONNECT to %s", req.URL.Host)
			clientBuf.Writer.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))

			// Use channels to send timeout or success signals
			done := make(chan bool, 1)
			// The timeout for this host is defined in the configuration
			timeout := time.Duration(host.Host.Timeout) * time.Millisecond
			// Since there is now a channel between the remote and the client we will be
			// tunneling all the data back and forth and waiting for it to finish or timesout
			go func() {
				var wg sync.WaitGroup
				wg.Add(2)
				go copyOrWarn(ctx, remote, client, &wg)
				go copyOrWarn(ctx, client, remote, &wg)
				wg.Wait()
				done <- true
			}()
			select {
			case <-done:
				// If it finishes in time mark the success in the breaker and close the clients
				host.Breaker.Success()
				client.Close()
				remote.Close()
			case <-time.After(timeout):
				// If the call times out mark the fail in the breaker and close the clients
				host.Breaker.Fail()
				ctx.Warnf("Call error, request timed out at %d milliseconds. Breaker fail increased", host.Host.Timeout)
				client.Write([]byte("HTTP/1.1 504 Gateway Timeout\r\n\r\n"))
				client.Close()
				remote.Close()
			}
		} else {
			// If the circuit breaker is tripped return an error immediatelly and close the client
			ctx.Warnf("Circuit breaker is tripped. Returning error immediatelly")
			client.Write([]byte("HTTP/1.1 503 Cannot reach destination\r\n\r\n"))
			client.Close()
		}

	})

	log.Printf("Sidebreaker listening on port %d\n", configuration.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", configuration.Port), proxy))

}

// Test wether the host is in our configuration
func isHostInConfig(hostMap map[string]Breakers) goproxy.ReqConditionFunc {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) bool {
		_, ok := hostMap[req.URL.Hostname()]
		return ok
	}
}

// Given two clients copy their data and mark a waiting group as done
func copyOrWarn(ctx *goproxy.ProxyCtx, dst io.Writer, src io.Reader, wg *sync.WaitGroup) {
	if _, err := io.Copy(dst, src); err != nil {
		ctx.Warnf("Error copying to client: %s", err)
	}
	wg.Done()
}
