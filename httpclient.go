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
// Copyright 2020-2023 Opsdis

package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"time"
)

// HTTPClient used for retrieve data from an HTTP based api
type HTTPClient struct {
	InsecureHTTPS       bool
	Timeout             int
	Keepalive           int
	Tlshandshaketimeout int
}

// GetClient return a http client
func (c HTTPClient) GetClient() *http.Client {

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	var client = &http.Client{
		Timeout: time.Duration(c.Timeout) * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(c.Timeout) * time.Second,
				KeepAlive: time.Duration(c.Keepalive) * time.Second,
			}).DialContext,
			//TLSHandshakeTimeout: time.Duration(c.Tlshandshaketimeout) * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: c.InsecureHTTPS,
				//RootCAs:            rootCAs,
			},
			//ExpectContinueTimeout: 4 * time.Second,
			//ResponseHeaderTimeout: 3 * time.Second,
		},
	}
	return client
}
