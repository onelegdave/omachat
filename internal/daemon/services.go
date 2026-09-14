package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/onelegdave/omachat/internal/wire"
)

func (d *Daemon) serviceEnabled(network string) bool {
	d.servicesMu.Lock()
	defer d.servicesMu.Unlock()
	return d.activeServices[network]
}

// RestartRequested closes only after a successful setter response reaches the socket.
// The process boundary, not vendor Disconnect implementations, retires transports.
func (d *Daemon) RestartRequested() <-chan struct{} { return d.restartCh }

func (d *Daemon) acknowledgeServiceChange(resp wire.Response) {
	cfg, ok := resp.Result.(wire.ConfigResult)
	if resp.OK && ok && cfg.RestartRequired {
		d.PublishEvent(wire.Event{Event: "config", Data: cfg})
		d.restartOnce.Do(func() { close(d.restartCh) })
	}
}

func (d *Daemon) handleSetEnabledServices(req wire.Request) wire.Response {
	fail := func(err error) wire.Response { return wire.Response{ID: req.ID, Error: err.Error()} }
	data, err := json.Marshal(req.Params)
	if err != nil {
		return fail(err)
	}
	var p wire.SetEnabledServicesParams
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return fail(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fail(fmt.Errorf("invalid service selection"))
	}
	d.servicesMu.Lock()
	if d.restartPending {
		d.servicesMu.Unlock()
		return fail(fmt.Errorf("service change is restarting the helper; wait for reconnection before changing services again"))
	}
	if err := d.config.SetEnabledServices(p.EnabledServices); err != nil {
		d.servicesMu.Unlock()
		return fail(err)
	}
	changed := len(p.EnabledServices) != len(d.activeServices)
	for _, service := range p.EnabledServices {
		if !d.activeServices[service] {
			changed = true
		}
	}
	d.restartPending = changed
	d.servicesMu.Unlock()
	return wire.Response{ID: req.ID, OK: true, Result: d.PluginConfig()}
}

func globalSetting(method string) bool {
	switch method {
	case wire.MethodConfig, wire.MethodSetGiphyKey, wire.MethodSetUiScale, wire.MethodDiscardCapture:
		return true
	}
	return false
}

// serviceRequestError gates new operations; already-submitted sends are never replayed.
func (d *Daemon) serviceRequestError(network string) error {
	d.servicesMu.Lock()
	defer d.servicesMu.Unlock()
	if d.restartPending {
		return fmt.Errorf("service selection is restarting the helper; this request was not started. Retry after reconnection. For any earlier submitted message, check the conversation before sending again")
	}
	if !d.activeServices[network] {
		return fmt.Errorf("service disabled; enable it in Settings before using it")
	}
	return nil
}
