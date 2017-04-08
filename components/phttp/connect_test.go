// Copyright (c) 2017 Yandex LLC. All rights reserved.
// Author: Vladimir Skipor <skipor@yandex-team.ru>

package phttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/yandex/pandora/core"
	"github.com/yandex/pandora/core/aggregate"
)

var _ = Describe("connect", func() {
	tunnelHandler := func(originURL string) http.Handler {
		u, err := url.Parse(originURL)
		Expect(err).To(BeNil())
		originHost := u.Host
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			Expect(originHost).To(Equal(r.RequestURI))
			toOrigin, err := net.Dial("tcp", originHost)
			Expect(err).To(BeNil())
			conn, bufReader, err := w.(http.Hijacker).Hijack()
			Expect(err).To(BeNil())
			Expect(bufReader.Reader.Buffered()).To(BeZero(),
				"Current implementation should not send requested data before got response.")
			_, err = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
			Expect(err).To(BeNil())
			go func() { io.Copy(toOrigin, conn) }()
			go func() { io.Copy(conn, toOrigin) }()
		})
	}

	testClient := func(tunnelSSL bool) func() {
		return func() {
			origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusOK)
			}))
			defer origin.Close()

			var proxy *httptest.Server
			if tunnelSSL {
				proxy = httptest.NewTLSServer(tunnelHandler(origin.URL))
			} else {
				proxy = httptest.NewServer(tunnelHandler(origin.URL))
			}
			defer proxy.Close()

			req, err := http.NewRequest("GET", origin.URL, nil)
			Expect(err).To(BeNil())

			conf := NewDefaultConnectGunConfig()
			conf.ConnectSSL = tunnelSSL
			scheme := "http://"
			if tunnelSSL {
				scheme = "https://"
			}
			conf.Target = strings.TrimPrefix(proxy.URL, scheme)

			client := newConnectClient(conf)

			res, err := client.Do(req)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusOK))
		}
	}

	It("HTTP client", testClient(false))
	It("HTTPS client", testClient(true))

	It("gun", func() {
		origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusOK)
		}))
		defer origin.Close()
		proxy := httptest.NewServer(tunnelHandler(origin.URL))
		defer proxy.Close()

		req, err := http.NewRequest("GET", origin.URL, nil)
		Expect(err).To(BeNil())

		conf := NewDefaultConnectGunConfig()
		conf.Target = strings.TrimPrefix(proxy.URL, "http://")
		connectGun := NewConnectGun(conf)

		results := core.NewResults(1)
		connectGun.BindResultsTo(results)

		err = connectGun.Shoot(context.Background(), newTestAmmo(req))
		Expect(err).To(BeNil())

		var sample *aggregate.Sample
		Expect(results).To(Receive(&sample))

		Expect(sample.ProtoCode()).To(Equal(http.StatusOK))
	})
})