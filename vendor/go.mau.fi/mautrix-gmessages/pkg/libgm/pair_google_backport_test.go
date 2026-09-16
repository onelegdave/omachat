package libgm

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"google.golang.org/protobuf/proto"
)

func TestGaiaPairingBackport(t *testing.T) {
	// 1. Verify enum32 recognizable
	if name, ok := gmproto.GaiaPairingErrorCode_name[32]; !ok || name != "CLIENT_ATTESTATION_MISSING" {
		t.Errorf("Expected enum 32 to be CLIENT_ATTESTATION_MISSING, got %v", name)
	}
	if val, ok := gmproto.GaiaPairingErrorCode_value["CLIENT_ATTESTATION_MISSING"]; !ok || val != 32 {
		t.Errorf("Expected CLIENT_ATTESTATION_MISSING to be 32, got %v", val)
	}

	sess := &PairingSession{
		UUID:  uuid.New(),
		Start: time.Now(),
	}

	// 2. Use the extracted builder logic
	// INIT test
	initReq, msgTypeInit := buildGaiaPairingRequest(sess, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT, []byte("init"))
	if initReq.PrivateAPIConfirmation != "" {
		t.Errorf("Init request should not have PrivateAPIConfirmation set")
	}
	if initReq.ProposedVerificationCodeVersion != 1 {
		t.Errorf("Init request missing ProposedVerificationCodeVersion")
	}
	if initReq.ProposedKeyDerivationVersion != 1 {
		t.Error("Init request missing ProposedKeyDerivationVersion")
	}
	if msgTypeInit != gmproto.MessageType_GAIA_2 {
		t.Errorf("Init request should have msgType GAIA_2")
	}

	// FINISH test
	finishReq, msgTypeFinish := buildGaiaPairingRequest(sess, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED, []byte("finish"))
	const wantConfirmation = "This is an undocumented API. Use or access of undocumented Google APIs without express authorization is prohibited per the Google API Terms of Service (https://developers.google.com/terms)."
	if finishReq.PrivateAPIConfirmation != wantConfirmation {
		t.Errorf("Finished request must have the exact upstream PrivateAPIConfirmation")
	}
	if finishReq.ProposedVerificationCodeVersion != 0 {
		t.Errorf("Finished request should not have ProposedVerificationCodeVersion set")
	}
	if msgTypeFinish != gmproto.MessageType_BUGLE_MESSAGE {
		t.Errorf("Finished request should have msgType BUGLE_MESSAGE")
	}

	// Verify serialization includes field 8
	finishBytes, err := proto.Marshal(finishReq)
	if err != nil {
		t.Fatal(err)
	}

	field := finishReq.ProtoReflect().Descriptor().Fields().ByName("privateAPIConfirmation")
	if field == nil || field.Number() != 8 {
		t.Fatal("PrivateAPIConfirmation must encode as field 8")
	}
	var decoded gmproto.GaiaPairingRequestContainer
	if err := proto.Unmarshal(finishBytes, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PrivateAPIConfirmation != wantConfirmation {
		t.Fatal("confirmation was not preserved on the wire")
	}
}
