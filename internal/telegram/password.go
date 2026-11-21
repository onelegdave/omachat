package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// Cloud password (two-step verification) sign-in.
//
// When a QR login reports SESSION_PASSWORD_NEEDED, the MTProto connection that
// accepted the QR code stays open and waits for the user's cloud password. The
// helper never stores the password or writes it to logs. It is held only for
// the duration of one SRP check, and the byte copy used for hashing is cleared
// afterwards. Telegram receives only the SRP proof, never the password itself.

const (
	// MaxCloudPasswordBytes bounds one submitted password. Telegram does not
	// publish a limit; real passwords are far shorter than this.
	MaxCloudPasswordBytes = 256
	// maxCloudPasswordHintRunes bounds the account-defined hint shown in the panel.
	maxCloudPasswordHintRunes = 128
	// cloudPasswordWait is how long an unanswered password prompt keeps the
	// pairing connection open before the attempt ends.
	cloudPasswordWait = 10 * time.Minute
)

var (
	// ErrCloudPasswordInvalid reports a wrong password. The prompt stays open.
	ErrCloudPasswordInvalid = errors.New("That password is incorrect. Try again.")
	// ErrCloudPasswordEmpty reports an empty submission.
	ErrCloudPasswordEmpty = errors.New("Enter your Telegram cloud password.")
	// ErrCloudPasswordTooLong reports a submission over MaxCloudPasswordBytes.
	ErrCloudPasswordTooLong = fmt.Errorf("The password is longer than %d bytes.", MaxCloudPasswordBytes)
	// ErrCloudPasswordNotRequested reports a submission with no open prompt.
	ErrCloudPasswordNotRequested = errors.New("Telegram is not waiting for a cloud password. Select Pair with Telegram to start again.")
	// ErrCloudPasswordTimedOut ends a prompt that was not answered in time.
	ErrCloudPasswordTimedOut = errors.New("The cloud password prompt timed out. Select Pair with Telegram to start again.")
)

// CloudPasswordSubmitter checks one password attempt against the open
// sign-in. It returns nil on success, ErrCloudPasswordInvalid when the
// password is wrong and the prompt stays open, or another error. It takes
// ownership of password and clears it; callers must not reuse the slice.
type CloudPasswordSubmitter func(ctx context.Context, password []byte) error

type cloudPasswordAttempt struct {
	password []byte
	reply    chan error
}

// isPasswordNeeded reports whether a QR login stopped for two-step verification.
func isPasswordNeeded(err error) bool {
	return err != nil && (tgerr.Is(err, "SESSION_PASSWORD_NEEDED") || errors.Is(err, auth.ErrPasswordAuthNeeded))
}

// ValidateCloudPassword applies the local bounds before a password is sent on.
func ValidateCloudPassword(password []byte) error {
	if len(password) == 0 {
		return ErrCloudPasswordEmpty
	}
	if len(password) > MaxCloudPasswordBytes {
		return ErrCloudPasswordTooLong
	}
	return nil
}

// boundCloudPasswordHint keeps the account-defined hint short, single-line
// plain text.
func boundCloudPasswordHint(hint string) string {
	hint = strings.ToValidUTF8(hint, "")
	hint = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, hint)
	hint = strings.Join(strings.Fields(hint), " ")
	if utf8.RuneCountInString(hint) <= maxCloudPasswordHintRunes {
		return hint
	}
	runes := []rune(hint)
	return string(runes[:maxCloudPasswordHintRunes]) + "…"
}

// clear overwrites a password copy once it is no longer needed.
func clearPassword(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// awaitCloudPassword publishes a password prompt on out and answers attempts
// until one succeeds, a non-retryable error occurs, the wait expires, or ctx
// ends. signIn performs the SRP check for one password.
func awaitCloudPassword(
	ctx context.Context,
	out chan<- QRChannelItem,
	hint string,
	wait time.Duration,
	signIn func(ctx context.Context, password []byte) (*tg.AuthAuthorization, error),
) (*tg.AuthAuthorization, error) {
	attempts := make(chan cloudPasswordAttempt)
	// closed ends late submissions as soon as this wait returns.
	closed := make(chan struct{})
	defer close(closed)
	submit := func(callCtx context.Context, password []byte) error {
		if err := ValidateCloudPassword(password); err != nil {
			clearPassword(password)
			return err
		}
		reply := make(chan error, 1)
		select {
		case attempts <- cloudPasswordAttempt{password: password, reply: reply}:
			// The sign-in loop now owns password and clears it.
		case <-callCtx.Done():
			clearPassword(password)
			return callCtx.Err()
		case <-ctx.Done():
			clearPassword(password)
			return ErrCloudPasswordNotRequested
		case <-closed:
			clearPassword(password)
			return ErrCloudPasswordNotRequested
		}
		select {
		case err := <-reply:
			return err
		case <-callCtx.Done():
			return callCtx.Err()
		case <-ctx.Done():
			return ErrCloudPasswordNotRequested
		case <-closed:
			// The loop always replies before returning, so prefer its answer.
			select {
			case err := <-reply:
				return err
			default:
				return ErrCloudPasswordNotRequested
			}
		}
	}

	select {
	case out <- QRChannelItem{Event: QRChannelEventPassword, Hint: boundCloudPasswordHint(hint), SubmitPassword: submit}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case attempt := <-attempts:
			res, err := signIn(ctx, attempt.password)
			clearPassword(attempt.password)
			if err == nil {
				attempt.reply <- nil
				return res, nil
			}
			if errors.Is(err, auth.ErrPasswordInvalid) || tg.IsPasswordHashInvalid(err) {
				attempt.reply <- ErrCloudPasswordInvalid
				continue
			}
			if wait, ok := tgerr.AsFloodWait(err); ok {
				attempt.reply <- fmt.Errorf("Telegram asked to wait %s before the next password attempt.", wait.Round(time.Second))
				continue
			}
			attempt.reply <- err
			return nil, err
		case <-timer.C:
			return nil, ErrCloudPasswordTimedOut
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
