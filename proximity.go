package main

import (

	"crypto/tls"
	"flag"
	"fmt"
	"net/http/httputil"
	"net/url"
	"net/http"
	"os"
	//"strings"
	"io/ioutil"

)

var (
	matchid = uint64(0)
	connid  = uint64(0)

	localAddr   = flag.String("l", ":9999", "local address")
	remoteAddr  = flag.String("r", "http://localhost:80", "remote address")
	onlyHeaders = flag.Bool("h", false, "dump only headers")
	verbose     = flag.Bool("v", false, "display server actions")
	noverify    = flag.Bool("no-verify", false, "Do not verify TLS/SSL certificates.")
	colors      = flag.Bool("c", false, "output ansi colors")
)

func main() {
	flag.Parse()

	fmt.Printf("Proxying from %v to %v\n\n", *localAddr, *remoteAddr)
	serve()
}

func serve() {
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
		// copy host + scheme to response
		end.Host = target.Host
		end.Scheme = target.Scheme
		
		// build request and copy headers
		b2b, err := http.NewRequest(req.Method, end.String(), req.Body)
		for header, values := range req.Header {
			for _, value := range values {
				b2b.Header.Add(header, value)
			}
		}

		fmt.Printf("----- PARAMS ------\n")
		args, _ := url.ParseQuery(req.URL.RawQuery)
		for a,v := range args {
			fmt.Fprintf(os.Stderr, "%s:\t%s\t\n", a, v[0])
		}

		// dump request
		fmt.Printf("----- REQUEST ------\n")
		if x, err := httputil.DumpRequestOut(b2b, !*onlyHeaders); err == nil {
			fmt.Fprintf(os.Stderr, "%s\n", string(x))
		}

		client := &http.Client{
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
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: *noverify,
				},
			},
		}

		// build response
		var res *http.Response
		res, err = client.Do(b2b)
		defer res.Body.Close()

		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read response: %s\n", err)
			w.WriteHeader(599)
			return
		}

		// Dump response - optionally with body
		fmt.Printf("----- RESPONSE ------\n")
		if x, err := httputil.DumpResponse(res, !*onlyHeaders); err == nil {
			fmt.Fprintf(os.Stderr, "%s\n", string(x))
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
