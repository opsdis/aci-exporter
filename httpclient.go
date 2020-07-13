// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// Copyright 2020 Opsdis AB

package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"time"
)

// HTTPClient used for retrieve data from a HTTP based api
type HTTPClient struct {
	InsecureHTTPS       bool
	Timeout             int
	Keepalive           int
	Tlshandshaketimeout int
	cookieJar           http.CookieJar
}

// GetJar return the the client jar
func (c HTTPClient) GetJar() http.CookieJar {
	return c.cookieJar
}

// GetClient return a http client
func (c HTTPClient) GetClient() *http.Client {

	if c.Timeout == 0 {
		c.Timeout = 3
	}
	if c.Keepalive == 0 {
		c.Keepalive = 10
	}
	if c.Tlshandshaketimeout == 0 {
		c.Tlshandshaketimeout = 10
	}
	//
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	/*
		/ Read in the cert file

		const (
			localCertFile = "/usr/local/internal-ca/ca.crt"
		)

			certs, err := ioutil.ReadFile(localCertFile)
		if err != nil {
			log.Fatalf("Failed to append %q to RootCAs: %v", localCertFile, err)
		}

		/ Append our cert to the system pool
		if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
			log.Println("No certs appended, using system certs only")
		}
	*/
	//
	var client = &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(c.Timeout) * time.Second,
				KeepAlive: time.Duration(c.Keepalive) * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: time.Duration(c.Tlshandshaketimeout) * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: c.InsecureHTTPS,
				//RootCAs:            rootCAs,
			},

			ExpectContinueTimeout: 4 * time.Second,
			ResponseHeaderTimeout: 3 * time.Second,
		},
		Jar: c.cookieJar,
	}
	return client
}
