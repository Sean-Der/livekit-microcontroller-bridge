package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"
)

// setupTestGlobals initializes global variables for testing and returns a cleanup function
func setupTestGlobals(t *testing.T) func() {
	// Save original global variable values
	origLog := log
	origLivekitTrack := livekitTrack
	origEmbeddedTrack := embeddedTrack
	
	// Initialize required global variables for the test
	var err error
	
	// Initialize logger
	logger.InitFromConfig(&logger.Config{Level: "error"}, "test")
	log = logger.GetLogger()
	
	// Initialize livekitTrack
	livekitTrack, err = webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "pion",
	)
	require.NoError(t, err, "Failed to create test livekitTrack")
	
	// Initialize embeddedTrack
	embeddedTrack, err = lksdk.NewLocalTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus})
	require.NoError(t, err, "Failed to create test embeddedTrack")
	
	// Return cleanup function
	return func() {
		log = origLog
		livekitTrack = origLivekitTrack
		embeddedTrack = origEmbeddedTrack
	}
}

func TestNewAccessToken(t *testing.T) {
	apiKey := "test_api_key"
	apiSecret := "test_api_secret"
	roomName := "test_room"
	participantIdentity := "test_participant"

	token, err := newAccessToken(apiKey, apiSecret, roomName, participantIdentity)
	require.NoError(t, err, "newAccessToken should not return an error")
	require.NotEmpty(t, token, "newAccessToken should return a non-empty token string")

	// Verify the token
	verifier, err := auth.ParseAPIToken(token)
	require.NoError(t, err, "Failed to parse token")
	require.Equal(t, apiKey, verifier.APIKey(), "API key in token does not match")

	claims, err := verifier.Verify(apiSecret)
	require.NoError(t, err, "Failed to verify token")

	require.NotNil(t, claims.Video, "VideoGrant in token should not be nil")
	require.Equal(t, roomName, claims.Video.Room, "Room name in token does not match")
	require.Equal(t, participantIdentity, claims.Identity, "Participant identity in token does not match")
	require.True(t, claims.Video.RoomJoin, "RoomJoin permission should be true")
}

func TestConnectHandler(t *testing.T) {
	cleanup := setupTestGlobals(t)
	defer cleanup()

	// Setup test app
	app := &App{
		peerConns: make(map[string]*webrtc.PeerConnection),
		ctx:       context.Background(),
	}

	// Create test offer (valid SDP)
	testOffer := `v=0
o=- 0 0 IN IP4 127.0.0.1
s=-
t=0 0
m=audio 9 UDP/TLS/RTP/SAVPF 111
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:test
a=ice-pwd:testpassword
a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
a=setup:actpass
a=mid:0
a=sendrecv
a=rtcp-mux
a=rtpmap:111 opus/48000/2
a=fmtp:111 minptime=10;useinbandfec=1
`

	// Create test request
	req := httptest.NewRequest("POST", "/connect", strings.NewReader(testOffer))
	rec := httptest.NewRecorder()

	// Call handler
	app.connectHandler(rec, req)

	// Verify response
	require.Equal(t, http.StatusCreated, rec.Code)
	require.NotEmpty(t, rec.Body.String())

	// Verify peer connection was created and stored
	app.peerConnMu.RLock()
	require.NotEmpty(t, app.peerConns)
	app.peerConnMu.RUnlock()
}

func TestConnectHandlerInvalidMethod(t *testing.T) {
	cleanup := setupTestGlobals(t)
	defer cleanup()

	app := &App{
		peerConns: make(map[string]*webrtc.PeerConnection),
		ctx:       context.Background(),
	}

	req := httptest.NewRequest("GET", "/connect", nil)
	rec := httptest.NewRecorder()

	app.connectHandler(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestConnectHandlerInvalidOffer(t *testing.T) {
	cleanup := setupTestGlobals(t)
	defer cleanup()

	app := &App{
		peerConns: make(map[string]*webrtc.PeerConnection),
		ctx:       context.Background(),
	}

	req := httptest.NewRequest("POST", "/connect", strings.NewReader("invalid"))
	rec := httptest.NewRecorder()

	app.connectHandler(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestValidateFlags(t *testing.T) {
	// Save original values
	origHost := host
	origAPIKey := apiKey
	origAPISecret := apiSecret
	origRoomName := roomName
	origIdentity := identity

	// Restore original values after test
	defer func() {
		host = origHost
		apiKey = origAPIKey
		apiSecret = origAPISecret
		roomName = origRoomName
		identity = origIdentity
	}()

	// Test missing host
	host = ""
	apiKey = "test"
	apiSecret = "test"
	roomName = "test"
	identity = "test"
	err := validateFlags()
	require.Error(t, err)
	require.Contains(t, err.Error(), "host is required")

	// Test missing api-key
	host = "test"
	apiKey = ""
	err = validateFlags()
	require.Error(t, err)
	require.Contains(t, err.Error(), "api-key is required")

	// Test missing api-secret
	apiKey = "test"
	apiSecret = ""
	err = validateFlags()
	require.Error(t, err)
	require.Contains(t, err.Error(), "api-secret is required")

	// Test missing room-name
	apiSecret = "test"
	roomName = ""
	err = validateFlags()
	require.Error(t, err)
	require.Contains(t, err.Error(), "room-name is required")

	// Test missing identity
	roomName = "test"
	identity = ""
	err = validateFlags()
	require.Error(t, err)
	require.Contains(t, err.Error(), "identity is required")

	// Test all flags present
	identity = "test"
	err = validateFlags()
	require.NoError(t, err)
}

func TestAppCleanupPeerConnection(t *testing.T) {
	app := &App{
		peerConns: make(map[string]*webrtc.PeerConnection),
	}

	// Create a mock peer connection (we can't easily test the actual cleanup without WebRTC setup)
	connID := "test-connection"
	
	// Test cleanup of non-existent connection (should not panic)
	app.cleanupPeerConnection(connID)
	
	// Verify no connections exist
	app.peerConnMu.RLock()
	require.Empty(t, app.peerConns)
	app.peerConnMu.RUnlock()
}
