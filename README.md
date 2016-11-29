# proximity

Simple https proxy for debugging purposes (by mitm relaying)

Usage of proximity:
```
  -cert string
    	A PEM eoncoded certificate file. (default "cert.pem")
  -key string
    	A PEM encoded private key file. (default "key.pem")
  -h	dump only headers
  -l string
    	local address (default ":9999")
  -m int
    	Interval of metrics logging (default 5)
  -no-verify
    	Do not verify TLS/SSL certificates.
  -r string
    	remote address (default "http://localhost:80")
```

proximity will default to non-tls unless you specify certificate and key

To create a self-signed certificate:

    openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365

and answer some questions.

Use flag -no-verify to ignore tls verification errors



