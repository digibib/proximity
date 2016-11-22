package main

import (

	"crypto/tls"
	"flag"
	"fmt"
	"net/http/httputil"
	"net/url"
	"net/http"
	"os"
	"time"
	"log"
	"io/ioutil"
	metrics "github.com/rcrowley/go-metrics"
)

var (
	metricsInterval = flag.Int("m", 5, "Interval of metrics logging")
	certFile    = flag.String("cert", "cert.pem", "A PEM eoncoded certificate file.")
	keyFile     = flag.String("key", "key.pem", "A PEM encoded private key file.")
	localAddr   = flag.String("l", ":9999", "local address")
	remoteAddr  = flag.String("r", "http://localhost:80", "remote address")
	onlyHeaders = flag.Bool("h", false, "dump only headers")
	noverify    = flag.Bool("no-verify", false, "Do not verify TLS/SSL certificates.")
)

func main() {
	flag.Parse()

	fmt.Printf("Proxying from %v to %v\n\n", *localAddr, *remoteAddr)
	serve()
}

func serve() {
	go metrics.Log(metrics.DefaultRegistry, time.Duration(*metricsInterval)*time.Second, log.New(os.Stderr, "", 0))
	requestsCounter := metrics.GetOrRegisterCounter("numRequests", metrics.DefaultRegistry)
	successCounter := metrics.GetOrRegisterCounter("successfulRequests", metrics.DefaultRegistry)
	failureCounter := metrics.GetOrRegisterCounter("failedRequests", metrics.DefaultRegistry)

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {

		end, err := url.Parse(req.URL.RequestURI())
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse requested uri '%s': %s\n", req.URL, err)
			w.WriteHeader(599)
			return
		}

		// target for proxy
		target, err := url.Parse(*remoteAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse url: %s\n", err)
			w.WriteHeader(599)
			return
		}

		fmt.Printf("----- PARAMS ------\n")
		args, _ := url.ParseQuery(req.URL.RawQuery)
		for a,v := range args {
			fmt.Fprintf(os.Stderr, "%s:\t%s\t\n", a, v[0])
		}

		// copy host + scheme to response
		end.Host = target.Host
		end.Scheme = target.Scheme

		// Setup tls transport
		var tlsConfig *tls.Config

		if *noverify {
			tlsConfig = &tls.Config{ InsecureSkipVerify: true }
		} else {
			// Load client cert
			cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
			if err != nil {
				log.Fatal(err)
			}

			tlsConfig = &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
			tlsConfig.BuildNameToCertificate()
		}
		transport := &http.Transport{TLSClientConfig: tlsConfig}

		// build destination request and copy headers
		newreq, err := http.NewRequest(req.Method, end.String(), req.Body)
		
		for header, values := range req.Header {
			for _, value := range values {
				w.Header().Add(header, value)
			}
		}

		// dump request
		requestsCounter.Inc(1)
		fmt.Printf("----- REQUEST ------\n")
		if x, err := httputil.DumpRequestOut(newreq, !*onlyHeaders); err == nil {
			fmt.Fprintf(os.Stderr, "%s\n", string(x))
		}

		// Do real request
		client := &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				for header, values := range via[0].Header {
					for _, value := range values {
						req.Header.Add(header, value)
					}
				}

				fmt.Printf("----- REDIRECT ------\n")
				if x, err := httputil.DumpRequestOut(req, !*onlyHeaders); err == nil {
					fmt.Fprintf(os.Stderr, "%s\n", string(x))
				}
				return nil
			},
		}

		// build response
		var res *http.Response
		res, err = client.Do(newreq)

		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read response: %s\n", err)
			w.WriteHeader(599)
			failureCounter.Inc(1)
			return
		}

		defer res.Body.Close()

		if res.StatusCode == 200 {
			successCounter.Inc(1)
		} else {
			failureCounter.Inc(1)
		}

		// Dump response - optionally with body
		fmt.Printf("----- RESPONSE ------\n")

		if x, err := httputil.DumpResponse(res, !*onlyHeaders); err == nil {
			fmt.Printf(string(x))
		}

		// let response through
		var b []byte
		b, err = ioutil.ReadAll(res.Body)

		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read body: %s\n", err)
			w.WriteHeader(599)
			return
		}
		for header, values := range res.Header {
			for _, value := range values {
				w.Header().Add(header, value)
			}
		}
		w.WriteHeader(res.StatusCode)
		w.Write(b)
	})
	http.ListenAndServe(*localAddr, nil)
}
