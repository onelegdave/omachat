package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"google.golang.org/protobuf/proto"

	"github.com/onelegdave/omachat/internal/wire"
)

// Helper to create a dummy MediaContent for testing.
func newTestMediaContent() *gmproto.MediaContent {
	return &gmproto.MediaContent{
		MediaID:   "media-uuid-12345",
		MediaName: "photo.jpg",
		MimeType:  "image/jpeg",
		Size:      1024,
	}
}

// verifyProtoRoundTrip verifies that a SendMessageRequest correctly marshals and unmarshals.
func verifyProtoRoundTrip(t *testing.T, req *gmproto.SendMessageRequest) {
	t.Helper()
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal failed: %v", err)
	}
	var roundTripped gmproto.SendMessageRequest
	if err := proto.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("proto.Unmarshal failed: %v", err)
	}
	if !proto.Equal(req, &roundTripped) {
		t.Error("serialized request did not preserve the full payload")
	}
}

// TestSendMediaMessages_ImageOnly_NoCaption tests sending media without a caption.
// Verifies:
// 1. Exactly one SendMessageRequest is issued containing media MessageInfo and no text.
// 2. Matching top tmpID and both payload tmp IDs.
// 3. Provisional Message returned with attachments, no text, and nil CaptionMessage.
func TestSendMediaMessages_ImageOnly_NoCaption(t *testing.T) {
	ctx := context.Background()
	convID := "conv-100"
	participantID := "part-200"
	imageTmpID := uuid.NewString()
	media := newTestMediaContent()

	params := wire.SendMediaParams{
		ConversationID: convID,
		Path:           "/tmp/photo.jpg",
		Caption:        "",
		TmpID:          imageTmpID,
	}

	var captured []*gmproto.SendMessageRequest
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		captured = append(captured, req)
		return &gmproto.SendMessageResponse{
			Status: gmproto.SendMessageResponse_SUCCESS,
		}, nil
	}

	res, err := sendMediaMessages(ctx, params, participantID, imageTmpID, media, mockSend)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil SendMediaResult")
	}
	if len(captured) != 1 {
		t.Fatalf("expected exactly 1 send call, got %d", len(captured))
	}

	req := captured[0]
	verifyProtoRoundTrip(t, req)

	if req.GetConversationID() != convID {
		t.Errorf("req ConversationID: got %q, want %q", req.GetConversationID(), convID)
	}
	if req.GetTmpID() != imageTmpID {
		t.Errorf("req TmpID: got %q, want %q", req.GetTmpID(), imageTmpID)
	}

	payload := req.GetMessagePayload()
	if payload == nil {
		t.Fatal("expected non-nil MessagePayload")
	}
	if payload.GetConversationID() != convID {
		t.Errorf("payload ConversationID: got %q, want %q", payload.GetConversationID(), convID)
	}
	if payload.GetParticipantID() != participantID {
		t.Errorf("payload ParticipantID: got %q, want %q", payload.GetParticipantID(), participantID)
	}
	if payload.GetTmpID() != imageTmpID {
		t.Errorf("payload TmpID: got %q, want %q", payload.GetTmpID(), imageTmpID)
	}
	if payload.GetTmpID2() != imageTmpID {
		t.Errorf("payload TmpID2: got %q, want %q", payload.GetTmpID2(), imageTmpID)
	}
	if len(payload.GetMessageInfo()) != 1 {
		t.Fatalf("payload MessageInfo count: got %d, want 1", len(payload.GetMessageInfo()))
	}
	info := payload.GetMessageInfo()[0]
	if info.GetMediaContent() == nil {
		t.Error("expected MediaContent in MessageInfo")
	}
	if info.GetMessageContent() != nil {
		t.Error("expected no MessageContent in image-only request")
	}

	// Verify provisional message
	if res.Message == nil {
		t.Fatal("expected non-nil res.Message")
	}
	if res.Message.ID != imageTmpID || res.Message.TmpID != imageTmpID {
		t.Errorf("res.Message ID/TmpID: got ID=%q TmpID=%q, want %q", res.Message.ID, res.Message.TmpID, imageTmpID)
	}
	if res.Message.Text != "" {
		t.Errorf("res.Message Text: got %q, want empty", res.Message.Text)
	}
	if len(res.Message.Attachments) != 1 || res.Message.Attachments[0].MediaID != media.GetMediaID() {
		t.Fatal("image acknowledgement lost its attachment")
	}
	if !res.Message.Provisional || !res.Message.Pending {
		t.Errorf("res.Message Provisional=%v Pending=%v, want both true", res.Message.Provisional, res.Message.Pending)
	}
	if res.CaptionMessage != nil {
		t.Errorf("expected nil CaptionMessage, got %+v", res.CaptionMessage)
	}
	if res.CaptionError != "" {
		t.Errorf("expected empty CaptionError, got %q", res.CaptionError)
	}
}

// TestSendMediaMessages_WhitespaceCaption_NoCaptionSent verifies whitespace-only captions
// do not result in a second request.
func TestSendMediaMessages_WhitespaceCaption_NoCaptionSent(t *testing.T) {
	for _, caption := range []string{" ", "   ", "\t\n\r", "  \n  \t "} {
		t.Run(caption, func(t *testing.T) {
			ctx := context.Background()
			imageTmpID := uuid.NewString()
			params := wire.SendMediaParams{
				ConversationID: "conv-1",
				Path:           "/tmp/img.png",
				Caption:        caption,
				TmpID:          imageTmpID,
			}
			var sendCount int
			mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
				sendCount++
				return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil
			}
			res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, newTestMediaContent(), mockSend)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sendCount != 1 {
				t.Errorf("sendCount: got %d, want 1 for whitespace caption %q", sendCount, caption)
			}
			if res.CaptionMessage != nil {
				t.Errorf("expected nil CaptionMessage for whitespace caption")
			}
			if res.CaptionError != "" {
				t.Errorf("expected empty CaptionError, got %q", res.CaptionError)
			}
		})
	}
}

// TestSendMediaMessages_ImageAndCaption_Success verifies successful 2-step delivery:
// 1. Image sent first with media-only MessageInfo, no caption text.
// 2. Caption sent second with trimmed text MessageInfo and distinct deterministic valid UUID transaction ID.
// 3. Two provisional pending messages returned with matching distinct IDs.
func TestSendMediaMessages_ImageAndCaption_Success(t *testing.T) {
	ctx := context.Background()
	convID := "conv-success"
	participantID := "part-success"
	imageTmpID := uuid.NewString()
	media := newTestMediaContent()
	rawCaption := "  Sunset over the Pacific ocean  \n"
	expectedCaption := "Sunset over the Pacific ocean"

	params := wire.SendMediaParams{
		ConversationID: convID,
		Path:           "/tmp/sunset.jpg",
		Caption:        rawCaption,
		TmpID:          imageTmpID,
	}

	var captured []*gmproto.SendMessageRequest
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		captured = append(captured, req)
		return &gmproto.SendMessageResponse{
			Status: gmproto.SendMessageResponse_SUCCESS,
		}, nil
	}

	res, err := sendMediaMessages(ctx, params, participantID, imageTmpID, media, mockSend)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil SendMediaResult")
	}
	if len(captured) != 2 {
		t.Fatalf("expected exactly 2 send calls, got %d", len(captured))
	}

	// Request 1: Image
	imgReq := captured[0]
	verifyProtoRoundTrip(t, imgReq)
	if imgReq.GetTmpID() != imageTmpID {
		t.Errorf("imgReq TmpID: got %q, want %q", imgReq.GetTmpID(), imageTmpID)
	}
	if imgReq.GetConversationID() != convID {
		t.Errorf("imgReq ConversationID: got %q, want %q", imgReq.GetConversationID(), convID)
	}
	imgPayload := imgReq.GetMessagePayload()
	if imgPayload.GetTmpID() != imageTmpID || imgPayload.GetTmpID2() != imageTmpID {
		t.Errorf("imgPayload TmpID mismatch: got %q / %q, want %q",
			imgPayload.GetTmpID(), imgPayload.GetTmpID2(), imageTmpID)
	}
	if imgPayload.GetParticipantID() != participantID {
		t.Errorf("imgPayload ParticipantID: got %q, want %q", imgPayload.GetParticipantID(), participantID)
	}
	if len(imgPayload.GetMessageInfo()) != 1 {
		t.Fatalf("imgPayload MessageInfo length: got %d, want 1", len(imgPayload.GetMessageInfo()))
	}
	if imgPayload.GetMessageInfo()[0].GetMediaContent() == nil {
		t.Error("expected MediaContent in request 1")
	}
	if imgPayload.GetMessageInfo()[0].GetMessageContent() != nil {
		t.Error("expected NO MessageContent in request 1")
	}

	// Request 2: Caption
	capReq := captured[1]
	verifyProtoRoundTrip(t, capReq)
	captionTmpID := capReq.GetTmpID()
	if captionTmpID == "" {
		t.Fatal("expected nonempty caption TmpID")
	}
	if captionTmpID == imageTmpID {
		t.Errorf("caption TmpID %q must be distinct from image TmpID %q", captionTmpID, imageTmpID)
	}
	if _, err := uuid.Parse(captionTmpID); err != nil {
		t.Errorf("caption TmpID %q is not a valid UUID: %v", captionTmpID, err)
	}
	if capReq.GetConversationID() != convID {
		t.Errorf("capReq ConversationID: got %q, want %q", capReq.GetConversationID(), convID)
	}
	capPayload := capReq.GetMessagePayload()
	if capPayload.GetTmpID() != captionTmpID || capPayload.GetTmpID2() != captionTmpID {
		t.Errorf("capPayload TmpIDs mismatch: got %q / %q, want %q",
			capPayload.GetTmpID(), capPayload.GetTmpID2(), captionTmpID)
	}
	if capPayload.GetParticipantID() != participantID {
		t.Errorf("capPayload ParticipantID: got %q, want %q", capPayload.GetParticipantID(), participantID)
	}
	if len(capPayload.GetMessageInfo()) != 1 {
		t.Fatalf("capPayload MessageInfo length: got %d, want 1", len(capPayload.GetMessageInfo()))
	}
	if capPayload.GetMessageInfo()[0].GetMediaContent() != nil {
		t.Error("expected NO MediaContent in request 2")
	}
	msgContent := capPayload.GetMessageInfo()[0].GetMessageContent()
	if msgContent == nil {
		t.Fatal("expected MessageContent in request 2")
	}
	if msgContent.GetContent() != expectedCaption {
		t.Errorf("caption content: got %q, want %q", msgContent.GetContent(), expectedCaption)
	}

	// Verify provisional messages
	if res.Message == nil {
		t.Fatal("expected non-nil res.Message")
	}
	if res.Message.ID != imageTmpID || res.Message.TmpID != imageTmpID {
		t.Errorf("res.Message ID/TmpID: got ID=%q TmpID=%q, want %q",
			res.Message.ID, res.Message.TmpID, imageTmpID)
	}
	if res.Message.Text != "" {
		t.Errorf("res.Message.Text must not contain caption, got %q", res.Message.Text)
	}
	if !res.Message.Provisional || !res.Message.Pending {
		t.Errorf("res.Message Provisional=%v Pending=%v, want true/true",
			res.Message.Provisional, res.Message.Pending)
	}

	if res.CaptionMessage == nil {
		t.Fatal("expected non-nil res.CaptionMessage")
	}
	if res.CaptionMessage.ID != captionTmpID || res.CaptionMessage.TmpID != captionTmpID {
		t.Errorf("res.CaptionMessage ID/TmpID: got ID=%q TmpID=%q, want %q",
			res.CaptionMessage.ID, res.CaptionMessage.TmpID, captionTmpID)
	}
	if res.CaptionMessage.Text != expectedCaption {
		t.Errorf("res.CaptionMessage.Text: got %q, want %q",
			res.CaptionMessage.Text, expectedCaption)
	}
	if !res.CaptionMessage.Provisional || !res.CaptionMessage.Pending {
		t.Errorf("res.CaptionMessage Provisional=%v Pending=%v, want true/true",
			res.CaptionMessage.Provisional, res.CaptionMessage.Pending)
	}
	if !res.CaptionMessage.FromMe {
		t.Errorf("res.CaptionMessage FromMe: got false, want true")
	}
	if res.CaptionError != "" {
		t.Errorf("res.CaptionError: got %q, want empty", res.CaptionError)
	}
}

// TestSendMediaMessages_Regression_NeverSharePayload explicitly proves that image and caption
// NEVER share a payload across any request. Combining media and caption in a single MessageInfo
// list drops the image on OmaChat.
func TestSendMediaMessages_Regression_NeverSharePayload(t *testing.T) {
	ctx := context.Background()
	imageTmpID := uuid.NewString()
	media := newTestMediaContent()
	params := wire.SendMediaParams{
		ConversationID: "conv-regression",
		Path:           "/tmp/test.png",
		Caption:        "A photo caption that must not share payload with image",
		TmpID:          imageTmpID,
	}

	var captured []*gmproto.SendMessageRequest
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		captured = append(captured, req)
		return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil
	}

	res, err := sendMediaMessages(ctx, params, "part-reg", imageTmpID, media, mockSend)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected result")
	}

	for i, req := range captured {
		payload := req.GetMessagePayload()
		if payload == nil {
			t.Fatalf("request %d: nil MessagePayload", i)
		}
		if len(payload.GetMessageInfo()) > 1 {
			t.Errorf("REGRESSION DETECTED: request %d has %d MessageInfo items; media and caption must not be bundled in one request",
				i, len(payload.GetMessageInfo()))
		}
		hasMedia := false
		hasText := false
		for _, info := range payload.GetMessageInfo() {
			if info.GetMediaContent() != nil {
				hasMedia = true
			}
			if info.GetMessageContent() != nil && strings.TrimSpace(info.GetMessageContent().GetContent()) != "" {
				hasText = true
			}
		}
		if hasMedia && hasText {
			t.Errorf("REGRESSION DETECTED: request %d shares media and text in the same payload!", i)
		}
	}
}

// TestSendMediaMessages_ImageTransportError verifies that transport errors on the image
// prevent caption from being sent and return an error.
func TestSendMediaMessages_ImageTransportError(t *testing.T) {
	ctx := context.Background()
	imageTmpID := uuid.NewString()
	params := wire.SendMediaParams{
		ConversationID: "conv-err",
		Path:           "/tmp/img.png",
		Caption:        "Valid caption",
		TmpID:          imageTmpID,
	}

	var sendCount int
	expectedErr := errors.New("network connection reset")
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		sendCount++
		return nil, expectedErr
	}

	res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, newTestMediaContent(), mockSend)
	if err == nil {
		t.Fatal("expected error on image transport failure, got nil")
	}
	if sendCount != 1 {
		t.Errorf("sendCount: got %d, want exactly 1 (caption must not be sent if image fails)", sendCount)
	}
	if res != nil && res.Message != nil {
		t.Errorf("expected no provisional message on image failure, got %+v", res.Message)
	}
}

// TestSendMediaMessages_ImageNonSuccessStatus verifies that a non-SUCCESS status on the image
// returns an error and prevents caption from being sent.
func TestSendMediaMessages_ImageNonSuccessStatus(t *testing.T) {
	ctx := context.Background()
	imageTmpID := uuid.NewString()
	params := wire.SendMediaParams{
		ConversationID: "conv-reject",
		Path:           "/tmp/img.png",
		Caption:        "Valid caption",
		TmpID:          imageTmpID,
	}

	var sendCount int
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		sendCount++
		return &gmproto.SendMessageResponse{
			Status: gmproto.SendMessageResponse_FAILURE_2,
		}, nil
	}

	res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, newTestMediaContent(), mockSend)
	if err == nil {
		t.Fatal("expected error on image non-SUCCESS status, got nil")
	}
	if sendCount != 1 {
		t.Errorf("sendCount: got %d, want exactly 1 (caption must not be sent if image is rejected)", sendCount)
	}
	if res != nil && res.Message != nil {
		t.Errorf("expected no provisional message on image rejection, got %+v", res.Message)
	}
}

// TestSendMediaMessages_CaptionTransportError_PreservesImage verifies the partial failure contract:
// If image succeeds but caption encounters a transport error:
// 1. Overall error is nil (client must NOT be asked to resend the accepted image!).
// 2. res.Message is preserved and valid.
// 3. res.CaptionMessage is nil.
// 4. res.CaptionError is non-empty.
func TestSendMediaMessages_CaptionTransportError_PreservesImage(t *testing.T) {
	ctx := context.Background()
	imageTmpID := uuid.NewString()
	params := wire.SendMediaParams{
		ConversationID: "conv-partial",
		Path:           "/tmp/img.png",
		Caption:        "A caption that will fail",
		TmpID:          imageTmpID,
	}

	var sendCount int
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		sendCount++
		if sendCount == 1 {
			// Image succeeds
			return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil
		}
		// Caption transport failure
		return nil, errors.New("caption pipe broken")
	}

	res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, newTestMediaContent(), mockSend)
	if err != nil {
		t.Fatalf("partial failure contract violated: overall error must be nil when image was accepted, got %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil SendMediaResult")
	}
	if sendCount != 2 {
		t.Fatalf("sendCount: got %d, want 2", sendCount)
	}
	if res.Message == nil {
		t.Fatal("res.Message (image provisional) must be preserved")
	}
	if res.Message.ID != imageTmpID || res.Message.TmpID != imageTmpID {
		t.Errorf("res.Message ID/TmpID: got %q, want %q", res.Message.ID, imageTmpID)
	}
	if res.CaptionMessage != nil {
		t.Errorf("res.CaptionMessage must be nil on caption failure, got %+v", res.CaptionMessage)
	}
	if res.CaptionError == "" {
		t.Error("res.CaptionError must be non-empty when caption fails")
	}
}

// TestSendMediaMessages_CaptionNonSuccessStatus_PreservesImage verifies that when caption send
// returns a non-SUCCESS status, the image provisional message is preserved and CaptionError is set.
func TestSendMediaMessages_CaptionNonSuccessStatus_PreservesImage(t *testing.T) {
	ctx := context.Background()
	imageTmpID := uuid.NewString()
	params := wire.SendMediaParams{
		ConversationID: "conv-caption-reject",
		Path:           "/tmp/img.png",
		Caption:        "Rejected caption",
		TmpID:          imageTmpID,
	}

	var sendCount int
	mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		sendCount++
		if sendCount == 1 {
			return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil
		}
		return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_FAILURE_2}, nil
	}

	res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, newTestMediaContent(), mockSend)
	if err != nil {
		t.Fatalf("expected nil overall error, got %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil SendMediaResult")
	}
	if res.Message == nil {
		t.Fatal("res.Message must be preserved")
	}
	if res.CaptionMessage != nil {
		t.Errorf("res.CaptionMessage must be nil on rejected caption, got %+v", res.CaptionMessage)
	}
	if res.CaptionError == "" {
		t.Error("res.CaptionError must be non-empty when caption is rejected")
	}
}

// TestSendMediaMessages_ContextCancelled_AfterImageAck verifies that if the context is cancelled
// after image acknowledgement:
// 1. Caption is aborted / fails.
// 2. Image provisional result is preserved.
// 3. Overall error is nil.
// 4. CaptionError is non-empty.
func TestSendMediaMessages_ContextCancelled_AfterImageAck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	imageTmpID := uuid.NewString()
	params := wire.SendMediaParams{
		ConversationID: "conv-cancel",
		Path:           "/tmp/img.png",
		Caption:        "Caption during cancel",
		TmpID:          imageTmpID,
	}

	mockSend := func(sendCtx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		// If request is the image, acknowledge it and cancel the context immediately
		if req.GetTmpID() == imageTmpID {
			cancel()
			return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil
		}
		t.Fatal("caption must not be issued after cancellation")
		return nil, sendCtx.Err()
	}

	res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, newTestMediaContent(), mockSend)
	if err != nil {
		t.Fatalf("expected nil overall error after image ack, got %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil SendMediaResult")
	}
	if res.Message == nil {
		t.Fatal("res.Message must be preserved after image ack")
	}
	if res.CaptionMessage != nil {
		t.Errorf("res.CaptionMessage must be nil on cancellation, got %+v", res.CaptionMessage)
	}
	if res.CaptionError == "" {
		t.Error("res.CaptionError must report failure when caption is cancelled")
	}
}

// TestSendMediaMessages_DeterministicUUID_StableDistinctValid verifies:
// 1. Deterministically derived caption UUID: repeated calls with same image tmpID yield identical caption ID.
// 2. Stable, distinct: caption ID != image ID.
// 3. Valid UUID: passes uuid.Parse without error.
// 4. Different image IDs produce different caption IDs.
func TestSendMediaMessages_DeterministicUUID_StableDistinctValid(t *testing.T) {
	ctx := context.Background()
	media := newTestMediaContent()

	runTest := func(imageTmpID string) string {
		var captionTmpID string
		mockSend := func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
			if req.GetTmpID() != imageTmpID {
				captionTmpID = req.GetTmpID()
			}
			return &gmproto.SendMessageResponse{Status: gmproto.SendMessageResponse_SUCCESS}, nil
		}
		params := wire.SendMediaParams{
			ConversationID: "conv-uuid",
			Path:           "/tmp/img.png",
			Caption:        "Testing deterministic UUID",
			TmpID:          imageTmpID,
		}
		res, err := sendMediaMessages(ctx, params, "part-1", imageTmpID, media, mockSend)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.CaptionMessage == nil {
			t.Fatal("expected non-nil CaptionMessage")
		}
		if res.CaptionMessage.TmpID != captionTmpID {
			t.Errorf("CaptionMessage TmpID mismatch: got %q, want %q",
				res.CaptionMessage.TmpID, captionTmpID)
		}
		return captionTmpID
	}

	idA := uuid.NewString()
	capA1 := runTest(idA)
	capA2 := runTest(idA)

	if capA1 != capA2 {
		t.Errorf("caption ID must be deterministic: run 1 got %q, run 2 got %q", capA1, capA2)
	}
	if capA1 == idA {
		t.Errorf("caption ID %q must be distinct from image ID %q", capA1, idA)
	}
	if _, err := uuid.Parse(capA1); err != nil {
		t.Errorf("caption ID %q is not a valid UUID: %v", capA1, err)
	}

	idB := uuid.NewString()
	capB := runTest(idB)
	if capB == capA1 {
		t.Errorf("different image IDs must produce different caption IDs: got %q for both", capB)
	}
	if _, err := uuid.Parse(capB); err != nil {
		t.Errorf("caption ID %q is not a valid UUID: %v", capB, err)
	}
}
