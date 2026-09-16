package req

import (
	"crypto/tls"
	"net/http"

	"go.mau.fi/util/exhttp"
)

func MakeTransportOverride(cli *Client) func(settings exhttp.ClientSettings) http.RoundTripper {
	return func(settings exhttp.ClientSettings) http.RoundTripper {
		if settings.Dial != nil {
			cli.SetDial(settings.Dial)
		}
		if settings.HTTPProxy != nil {
			cli.SetProxy(settings.HTTPProxy)
		}
		if settings.TLSHandshakeTimeout != 0 {
			cli.SetTLSHandshakeTimeout(settings.TLSHandshakeTimeout)
		}
		if settings.ResponseHeaderTimeout != 0 {
			cli.SetResponseHeaderTimeout(settings.ResponseHeaderTimeout)
		}
		if settings.IdleConnTimeout != 0 {
			cli.SetIdleConnTimeout(settings.IdleConnTimeout)
		}
		if settings.DisableHTTP2 {
			cli.EnableForceHTTP1()
		}
		if settings.TLSConfig != nil {
			cli.SetTLSClientConfig(settings.TLSConfig)
		}
		if settings.InsecureTLS {
			if cli.TLSClientConfig == nil {
				cli.TLSClientConfig = &tls.Config{}
			}
			cli.TLSClientConfig.InsecureSkipVerify = true
		}
		return cli.GetTransport()
	}
}

func WithTransportOverride(base exhttp.ClientSettings, cli *Client) exhttp.ClientSettings {
	base.TransportOverride = MakeTransportOverride(cli)
	return base
}
