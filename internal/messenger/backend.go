package messenger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/onelegdave/omachat/internal/browser"
	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"

	_ "github.com/mattn/go-sqlite3"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var ErrNotConfigured = errors.New("Messenger client not configured")

type Backend struct {
	log     zerolog.Logger
	paths   *appStore.Paths
	publish func(wire.Event)
	config  *appStore.ConfigStore

	mu        sync.RWMutex
	status    wire.Status
	paired    bool

	client     *messagix.Client
	e2eeClient *whatsmeow.Client
	waStore    *sqlstore.Container

	ctx    context.Context
	cancel context.CancelFunc
}

func New(log zerolog.Logger, paths *appStore.Paths, publish func(wire.Event)) *Backend {
	return &Backend{
		log:     log.With().Str("svc", "messenger").Logger(),
		paths:   paths,
		publish: publish,
		status: wire.Status{
			State: wire.StateUnpaired,
		},
	}
}

func (b *Backend) SetConfig(cs *appStore.ConfigStore) {
	b.config = cs
}

func (b *Backend) Status() wire.Status {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

func (b *Backend) setState(state wire.ConnState, errStr string) {
	b.mu.Lock()
	if b.status.State != state || b.status.Error != errStr {
		b.status.State = state
		b.status.Error = errStr
		st := b.status
		b.mu.Unlock()
		b.publish(wire.Event{Event: wire.EventStatus, Data: st})
	} else {
		b.mu.Unlock()
	}
}

func (b *Backend) setPaired(paired bool) {
	b.mu.Lock()
	b.paired = paired
	b.mu.Unlock()
}

func (b *Backend) saveCookies(cooks map[string]string) error {
	data, err := json.Marshal(cooks)
	if err != nil {
		return err
	}
	return appStore.WritePrivateJSON(b.paths.MessengerSessionFile(), data)
}

func (b *Backend) loadCookies() (map[string]string, error) {
	data, err := os.ReadFile(b.paths.MessengerSessionFile())
	if err != nil {
		return nil, err
	}
	var cooks map[string]string
	if err := json.Unmarshal(data, &cooks); err != nil {
		return nil, err
	}
	return cooks, nil
}

func (b *Backend) PairFromBrowser(ctx context.Context) error {
	b.setState(wire.StateConnecting, "Finding browser profile...")
	profiles := browser.DiscoverProfiles()
	if len(profiles) == 0 {
		err := errors.New("no browser profile found")
		b.setState(wire.StateUnpaired, err.Error())
		return err
	}

	var selected *browser.Profile
	cfgName := ""
	if b.config != nil {
		cfgName = b.config.Get().BrowserProfile
	}
	if cfgName != "" {
		for i := range profiles {
			if profiles[i].Name == cfgName {
				selected = &profiles[i]
				break
			}
		}
	}
	if selected == nil {
		selected = &profiles[0]
	}

	cooks, err := browser.ExtractMessengerCookiesContext(ctx, *selected)
	if err != nil {
		b.setState(wire.StateUnpaired, "Failed to read Messenger cookies: " + err.Error())
		return fmt.Errorf("read messenger cookies: %w", err)
	}
	
	mc := &cookies.Cookies{
		Platform: types.Facebook,
	}
	metaCookies := make(map[cookies.MetaCookieName]string)
	for k, v := range cooks {
		metaCookies[cookies.MetaCookieName(k)] = v
	}
	mc.UpdateValues(metaCookies)
	
	b.setState(wire.StateConnecting, "Authenticating with Messenger...")
	
	if err := b.initClient(ctx, mc); err != nil {
		b.setState(wire.StateUnpaired, "Authentication failed: " + err.Error())
		return err
	}
	
	if err := b.saveCookies(cooks); err != nil {
		b.log.Err(err).Msg("Failed to persist messenger cookies")
	}
	
	b.setPaired(true)
	b.setState(wire.StateConnected, "")
	return nil
}

func (b *Backend) initClient(ctx context.Context, mc *cookies.Cookies) error {
	if b.waStore == nil {
		dbLog := waLog.Zerolog(b.log.With().Str("component", "db").Logger())
		container, err := sqlstore.New(context.Background(), "sqlite3", "file:"+b.paths.MessengerDBFile()+"?_foreign_keys=on", dbLog)
		if err != nil {
			return fmt.Errorf("open sqlite store: %w", err)
		}
		b.waStore = container
	}
	
	device, err := b.waStore.GetFirstDevice(context.Background())
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}
	if device == nil {
		device = b.waStore.NewDevice()
	}

	cli := messagix.NewClient(
		mc,
		b.log.With().Str("component", "messagix").Logger(),
		&messagix.Config{},
	)
	
	// Wait, messagix client might need to be connected first before setting device?
	// According to connector/client.go, it sets device then Connect() or Connect() then E2EE connect.
	
	cli.SetDevice(device)
	
	err = cli.Connect(ctx)
	if err != nil {
		return fmt.Errorf("messagix connect: %w", err)
	}
	
	// Ensure E2EE device registration if new
	// Actually we probably just need to Connect e2eeClient
	e2ee, err := cli.PrepareE2EEClient()
	if err != nil {
		return fmt.Errorf("prepare e2ee: %w", err)
	}
	
	err = e2ee.Connect()
	if err != nil {
		return fmt.Errorf("e2ee connect: %w", err)
	}
	
	b.mu.Lock()
	b.client = cli
	b.e2eeClient = e2ee
	b.mu.Unlock()
	
	return nil
}

func (b *Backend) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	b.ctx = ctx
	b.cancel = cancel
	b.mu.Unlock()

	cooks, err := b.loadCookies()
	if err != nil {
		if os.IsNotExist(err) {
			b.setPaired(false)
			b.setState(wire.StateUnpaired, "")
			return nil // paired false, wait for PairFromBrowser
		}
		b.setState(wire.StateError, "Failed to read session: "+err.Error())
		return err
	}
	
	mc := &cookies.Cookies{Platform: types.Facebook}
	metaCookies := make(map[cookies.MetaCookieName]string)
	for k, v := range cooks {
		metaCookies[cookies.MetaCookieName(k)] = v
	}
	mc.UpdateValues(metaCookies)
	
	b.setState(wire.StateConnecting, "")
	
	if err := b.initClient(ctx, mc); err != nil {
		b.setState(wire.StateError, "Connect failed: "+err.Error())
		return err
	}
	
	b.setPaired(true)
	b.setState(wire.StateConnected, "")
	return nil
}

func (b *Backend) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.e2eeClient != nil {
		b.e2eeClient.Disconnect()
		b.e2eeClient = nil
	}
	if b.client != nil {
		b.client.Disconnect()
		b.client = nil
	}
}

func (b *Backend) Unpair(ctx context.Context) error {
	b.Stop()
	b.setPaired(false)
	b.setState(wire.StateUnpaired, "")
	return b.paths.ClearMessengerSession()
}

func (b *Backend) Conversations(count int) []wire.Conversation {
	// Concrete blocker: mautrix-meta exposes threads via raw table.LSTable (LightSpeed) structures
	// and E2EE messages via whatsmeow WhatsApp-JID structures. Mapping Facebook Thread IDs to WA JIDs
	// requires porting the bridge's extensive SQL database and state-machine logic.
	return nil
}

func (b *Backend) Messages(ctx context.Context, p wire.MessagesParams) (wire.MessagesResult, error) {
	return wire.MessagesResult{}, errors.New("exact extraction of E2EE internals infeasible: mapping Meta thread IDs to Labyrinth WA JIDs requires porting the bridge database schema and sync state machine")
}

func (b *Backend) Send(ctx context.Context, p wire.SendParams) (*wire.Message, error) {
	return nil, errors.New("exact extraction of E2EE internals infeasible: cannot construct e2eeClient.SendMessage without Labyrinth JID and crypto state management from mautrix-meta")
}

func (b *Backend) MarkRead(ctx context.Context, p wire.MarkReadParams) error {
	return errors.New("exact extraction of E2EE internals infeasible: MarkRead requires WA JID mapping")
}

func (b *Backend) Refresh(ctx context.Context) error {
	return errors.New("exact extraction of E2EE internals infeasible: LSTable parsing is internal to mautrix-meta")
}
