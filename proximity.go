package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	metrics "github.com/rcrowley/go-metrics"
)

var (
	metricsInterval = flag.Int("m", 60, "Interval of metrics logging")
	certFile        = flag.String("cert", "cert.pem", "A PEM eoncoded certificate file.")
	keyFile         = flag.String("key", "key.pem", "A PEM encoded private key file.")
	localAddr       = flag.String("l", ":9999", "local address")
	remoteAddr      = flag.String("r", "http://localhost:80", "remote address")
	verbosity       = flag.Int("v", 0, "1: only params, 2: add request and response headers, 3: add request and response body")
	noverify        = flag.Bool("no-verify", false, "Do not verify TLS/SSL certificates.")
)

func main() {
	flag.Parse()

	log.Printf("Proxying from %v to %v\n\n", *localAddr, *remoteAddr)

	go metrics.Log(metrics.DefaultRegistry, time.Duration(*metricsInterval)*time.Second, log.New(os.Stderr, "", 0))

	http.HandleFunc("/", proxy)
	log.Fatal(http.ListenAndServe(*localAddr, nil))
}

func proxy(w http.ResponseWriter, req *http.Request) {

	requestsCounter := metrics.GetOrRegisterCounter("numRequests", metrics.DefaultRegistry)
	successCounter := metrics.GetOrRegisterCounter("successfulRequests", metrics.DefaultRegistry)
	failureCounter := metrics.GetOrRegisterCounter("failedRequests", metrics.DefaultRegistry)

	end, err := url.Parse(req.URL.RequestURI())
	if err != nil {
		log.Printf("failed to parse requested uri '%s': %s\n", req.URL, err)
		w.WriteHeader(599)
		return
	}

	// target for proxy
	target, err := url.Parse(*remoteAddr)
	if err != nil {
		log.Printf("failed to parse url: %s\n", err)
		w.WriteHeader(599)
		return
	}
	log.Printf("--> %v\n", req.Host)

	// copy host + scheme to response
	end.Host = target.Host
	end.Scheme = target.Scheme

	// Setup tls transport
	var tlsConfig *tls.Config

	if *noverify {
		tlsConfig = &tls.Config{InsecureSkipVerify: true}
	} else {
		// Load client cert
		cert, err := tls.LoadX509KeyPair(*certFile, *keyFile)
		if err != nil {
			log.Printf("failed to load certificate: %s\n", err)
		}

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		tlsConfig.BuildNameToCertificate()
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}

	// build destination request, clone body
	buf, _ := ioutil.ReadAll(req.Body)
	b1 := ioutil.NopCloser(bytes.NewBuffer(buf))
	b2 := ioutil.NopCloser(bytes.NewBuffer(buf))

	newreq, err := http.NewRequest(req.Method, end.String(), b1)
	if err != nil {
		log.Printf("failed to create new HTTP request: %s\n", err)
		w.WriteHeader(599)
		return
	}

	for header, values := range req.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	// dump request according to verbosity
	requestsCounter.Inc(1)
	if *verbosity > 0 {
		req.Body = b2 // return original body
		log.Println("----- PARAMS ------")
		err := req.ParseForm()
		if err != nil {
			log.Printf("failed to parse HTTP request params/formdata: %s\n", err)
		}
		for k, v := range req.Form {
			val, _ := url.QueryUnescape(v[0])
			log.Printf("%s:\t%s\t\n", k, val)
		}
		if *verbosity > 1 {
			if x, err := httputil.DumpRequestOut(newreq, (*verbosity == 3)); err == nil {
				log.Println("----- REQUEST ------")
				log.Printf("%s", string(x))
			}
		}
		req.Body = b2 // return original body
	}

	// Do real request
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				log.Fatal("stopped after 10 redirects")
			}
			for header, values := range via[0].Header {
				for _, value := range values {
					req.Header.Add(header, value)
				}
			}

			if *verbosity >= 1 {
				log.Println("----- REDIRECT ------")
				if x, err := httputil.DumpRequestOut(req, (*verbosity == 3)); err == nil {
					log.Printf("%s", string(x))
				}
			}
			return nil
		},
	}

	// build response
	var res *http.Response
	res, err = client.Do(newreq)

	if err != nil {
		log.Printf("failed to read response: %s\n", err)
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
	if *verbosity > 1 {
		log.Println("----- RESPONSE ------")
		if x, err := httputil.DumpResponse(res, (*verbosity == 3)); err == nil {
			log.Print(string(x))
		}
	}

	// let response through
	var b []byte
	b, err = ioutil.ReadAll(res.Body)

	if err != nil {
		log.Printf("failed to read body: %s\n", err)
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
	log.Println("<-- END")
}
