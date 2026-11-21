package telegram

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// fakeSignIn records attempts and answers from a fixed password.
type fakeSignIn struct {
	mu       sync.Mutex
	correct  string
	attempts []string
	seen     [][]byte
	err      error
}

func (f *fakeSignIn) signIn(ctx context.Context, password []byte) (*tg.AuthAuthorization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, string(password))
	f.seen = append(f.seen, password)
	if f.err != nil {
		return nil, f.err
	}
	if string(password) != f.correct {
		return nil, auth.ErrPasswordInvalid
	}
	return &tg.AuthAuthorization{User: &tg.User{ID: 42}}, nil
}

func startPasswordWait(t *testing.T, ctx context.Context, f *fakeSignIn, wait time.Duration) (QRChannelItem, chan error) {
	t.Helper()
	out := make(chan QRChannelItem, 1)
	done := make(chan error, 1)
	go func() {
		res, err := awaitCloudPassword(ctx, out, "first pet", wait, f.signIn)
		if err == nil && (res == nil || res.User == nil) {
			err = errors.New("success returned no authorization")
		}
		done <- err
	}()
	select {
	case item := <-out:
		if item.Event != QRChannelEventPassword || item.SubmitPassword == nil {
			t.Fatalf("expected a password prompt, got %+v", item)
		}
		return item, done
	case <-time.After(2 * time.Second):
		t.Fatal("no password prompt")
	}
	return QRChannelItem{}, nil
}

func TestCloudPasswordWrongThenRightSignsIn(t *testing.T) {
	f := &fakeSignIn{correct: "correct horse"}
	item, done := startPasswordWait(t, context.Background(), f, time.Minute)
	if item.Hint != "first pet" {
		t.Errorf("hint = %q, want first pet", item.Hint)
	}

	wrong := []byte("wrong")
	if err := item.SubmitPassword(context.Background(), wrong); !errors.Is(err, ErrCloudPasswordInvalid) {
		t.Fatalf("wrong password error = %v, want ErrCloudPasswordInvalid", err)
	}
	if err := item.SubmitPassword(context.Background(), []byte("correct horse")); err != nil {
		t.Fatalf("correct password error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("sign-in ended with %v", err)
	}
	if strings.Join(f.attempts, ",") != "wrong,correct horse" {
		t.Errorf("attempts = %q", f.attempts)
	}
	for i, b := range f.seen {
		if !bytes.Equal(b, make([]byte, len(b))) {
			t.Errorf("attempt %d password bytes were not cleared", i)
		}
	}
	// The prompt closes once sign-in completes; a late submission must not block.
	late := make(chan error, 1)
	go func() { late <- item.SubmitPassword(context.Background(), []byte("late")) }()
	select {
	case err := <-late:
		if !errors.Is(err, ErrCloudPasswordNotRequested) {
			t.Fatalf("late submission = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a late submission blocked after sign-in completed")
	}
}

func TestCloudPasswordValidationNeverReachesTelegram(t *testing.T) {
	f := &fakeSignIn{correct: "x"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	item, _ := startPasswordWait(t, ctx, f, time.Minute)

	if err := item.SubmitPassword(context.Background(), nil); !errors.Is(err, ErrCloudPasswordEmpty) {
		t.Errorf("empty password error = %v", err)
	}
	long := bytes.Repeat([]byte("a"), MaxCloudPasswordBytes+1)
	if err := item.SubmitPassword(context.Background(), long); !errors.Is(err, ErrCloudPasswordTooLong) {
		t.Errorf("long password error = %v", err)
	}
	if !bytes.Equal(long, make([]byte, len(long))) {
		t.Error("a rejected password was not cleared")
	}
	if len(f.attempts) != 0 {
		t.Errorf("invalid submissions reached Telegram: %d", len(f.attempts))
	}
}

func TestCloudPasswordRetryableAndFatalErrors(t *testing.T) {
	f := &fakeSignIn{err: tgerr.New(420, "FLOOD_WAIT_30")}
	item, done := startPasswordWait(t, context.Background(), f, time.Minute)
	err := item.SubmitPassword(context.Background(), []byte("pw"))
	if err == nil || !strings.Contains(err.Error(), "wait 30s") {
		t.Fatalf("flood wait error = %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("a flood wait ended the prompt: %v", err)
	default:
	}

	f.mu.Lock()
	f.err = errors.New("connection lost")
	f.mu.Unlock()
	if err := item.SubmitPassword(context.Background(), []byte("pw")); err == nil || err.Error() != "connection lost" {
		t.Fatalf("fatal error = %v", err)
	}
	if err := <-done; err == nil || err.Error() != "connection lost" {
		t.Fatalf("sign-in ended with %v, want connection lost", err)
	}
}

func TestCloudPasswordPromptTimesOutAndCancels(t *testing.T) {
	f := &fakeSignIn{correct: "x"}
	_, done := startPasswordWait(t, context.Background(), f, 20*time.Millisecond)
	if err := <-done; !errors.Is(err, ErrCloudPasswordTimedOut) {
		t.Fatalf("timeout error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	item, done := startPasswordWait(t, ctx, f, time.Minute)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if err := item.SubmitPassword(context.Background(), []byte("x")); !errors.Is(err, ErrCloudPasswordNotRequested) {
		t.Fatalf("submission after cancel = %v", err)
	}
}

func TestCloudPasswordHintIsBoundedPlainText(t *testing.T) {
	if got := boundCloudPasswordHint("  two\nlines\tand\x00 more \xff"); got != "two lines and more" {
		t.Errorf("hint = %q", got)
	}
	long := strings.Repeat("é", maxCloudPasswordHintRunes+20)
	got := boundCloudPasswordHint(long)
	if n := len([]rune(got)); n != maxCloudPasswordHintRunes+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("long hint has %d runes: %q", n, got)
	}
}

func TestBackendCloudPasswordPrompt(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	if err := b.SubmitCloudPassword(context.Background(), []byte("pw")); !errors.Is(err, ErrCloudPasswordNotRequested) {
		t.Fatalf("submission without a prompt = %v", err)
	}

	qrChan := make(chan QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) { return qrChan, nil }
	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=sample"}
	if _, err := b.StartPairing(context.Background()); err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}

	var got []string
	var mu sync.Mutex
	qrChan <- QRChannelItem{Event: QRChannelEventPassword, Hint: "first pet", SubmitPassword: func(ctx context.Context, password []byte) error {
		mu.Lock()
		got = append(got, string(password))
		mu.Unlock()
		if string(password) != "right" {
			return ErrCloudPasswordInvalid
		}
		qrChan <- QRChannelItem{Event: QRChannelEventSuccess}
		return nil
	}}
	waitFor(t, func() bool { return b.Status().PasswordRequired })
	st := b.Status()
	if st.State != wire.StatePairing || st.QRURL != "" || st.PasswordHint != "first pet" || st.Hint != hintCloudPasswordRequired {
		t.Fatalf("password prompt status = %+v", st)
	}

	if err := b.SubmitCloudPassword(context.Background(), []byte("wrong")); !errors.Is(err, ErrCloudPasswordInvalid) {
		t.Fatalf("wrong password = %v", err)
	}
	if !b.Status().PasswordRequired {
		t.Fatal("a wrong password closed the prompt")
	}
	if err := b.SubmitCloudPassword(context.Background(), []byte("right")); err != nil {
		t.Fatalf("right password = %v", err)
	}
	waitFor(t, func() bool { return b.Status().State == wire.StateConnected })
	st = b.Status()
	if st.PasswordRequired || st.PasswordHint != "" {
		t.Fatalf("connected status still shows the prompt: %+v", st)
	}
	if err := b.SubmitCloudPassword(context.Background(), []byte("right")); !errors.Is(err, ErrCloudPasswordNotRequested) {
		t.Fatalf("submission after success = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != "wrong,right" {
		t.Errorf("forwarded passwords = %q", got)
	}
}

func TestBackendUnpairClosesCloudPasswordPrompt(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) { return qrChan, nil }
	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=sample"}
	if _, err := b.StartPairing(context.Background()); err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}
	called := false
	qrChan <- QRChannelItem{Event: QRChannelEventPassword, SubmitPassword: func(context.Context, []byte) error {
		called = true
		return nil
	}}
	waitFor(t, func() bool { return b.Status().PasswordRequired })

	if err := b.Unpair(context.Background()); err != nil {
		t.Fatalf("Unpair failed: %v", err)
	}
	if st := b.Status(); st.PasswordRequired || st.PasswordHint != "" {
		t.Fatalf("unpair left the prompt open: %+v", st)
	}
	if err := b.SubmitCloudPassword(context.Background(), []byte("pw")); !errors.Is(err, ErrCloudPasswordNotRequested) {
		t.Fatalf("submission after unpair = %v", err)
	}
	if called {
		t.Error("a submission after unpair reached the old sign-in")
	}
}

func TestBackendCloudPasswordTimeoutReturnsToUnpaired(t *testing.T) {
	b, paths, _, mock := setupTestTelegramWithMock(t)
	cs := store.NewConfigStore(paths.ConfigFile())
	_ = cs.SetTelegramCredentials(1234567, "0123456789abcdef0123456789abcdef")
	b.SetConfig(cs)

	qrChan := make(chan QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan QRChannelItem, error) { return qrChan, nil }
	qrChan <- QRChannelItem{Event: QRChannelEventCode, Code: "tg://login?token=sample"}
	if _, err := b.StartPairing(context.Background()); err != nil {
		t.Fatalf("StartPairing failed: %v", err)
	}
	qrChan <- QRChannelItem{Event: QRChannelEventPassword, SubmitPassword: func(context.Context, []byte) error { return nil }}
	waitFor(t, func() bool { return b.Status().PasswordRequired })
	qrChan <- QRChannelItem{Event: QRChannelEventError, Error: ErrCloudPasswordTimedOut}
	waitFor(t, func() bool { return b.Status().State == wire.StateUnpaired })
	st := b.Status()
	if st.PasswordRequired || st.Error != ErrCloudPasswordTimedOut.Error() {
		t.Fatalf("timed-out status = %+v", st)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
