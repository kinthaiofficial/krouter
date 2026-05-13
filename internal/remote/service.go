package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

const pairingCodeTTL = 5 * time.Minute

// Status holds the current remote access state.
type Status struct {
	Enabled    bool
	Token      string // "KR-…" — empty when disabled
	PairingExp time.Time
	Code       string // 6-digit string — empty when expired or disabled
}

// Service manages the remote-access state machine.
// Thread-safe.
type Service struct {
	store *storage.Store

	mu         sync.RWMutex
	enabled    bool
	token      string
	code       string
	pairingExp time.Time
	enabledCh  chan struct{} // closed and replaced each time enable state changes
}

// New creates a remote-access Service.
func New(store *storage.Store) *Service {
	return &Service{
		store:     store,
		enabledCh: make(chan struct{}),
	}
}

// EnabledCh returns a channel that is closed whenever the remote-enabled state changes.
// Callers should re-read GetStatus() after receiving on this channel.
func (s *Service) EnabledCh() <-chan struct{} {
	s.mu.RLock()
	ch := s.enabledCh
	s.mu.RUnlock()
	return ch
}

// Enable generates a fresh pairing token and activates remote access.
// Returns the generated token string.
func (s *Service) Enable(ctx context.Context, localIP net.IP, port uint16) (string, error) {
	code, err := GeneratePairingCode()
	if err != nil {
		return "", fmt.Errorf("remote: generate pairing code: %w", err)
	}

	tok := PairingToken{
		IP:          localIP,
		Port:        port,
		PairingCode: code,
	}
	tokenStr, err := EncodeToken(tok)
	if err != nil {
		return "", fmt.Errorf("remote: encode token: %w", err)
	}

	s.mu.Lock()
	s.enabled = true
	s.token = tokenStr
	s.code = PairingCodeString(code)
	s.pairingExp = time.Now().Add(pairingCodeTTL)
	old := s.enabledCh
	s.enabledCh = make(chan struct{})
	s.mu.Unlock()
	close(old) // notify listeners

	return tokenStr, nil
}

// Disable deactivates remote access and clears the pairing state.
func (s *Service) Disable() {
	s.mu.Lock()
	s.enabled = false
	s.token = ""
	s.code = ""
	s.pairingExp = time.Time{}
	old := s.enabledCh
	s.enabledCh = make(chan struct{})
	s.mu.Unlock()
	close(old) // notify listeners
}

// GetStatus returns the current remote-access state.
func (s *Service) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := Status{
		Enabled:    s.enabled,
		Token:      s.token,
		PairingExp: s.pairingExp,
	}
	// Only expose the pairing code while it is within the TTL.
	if s.enabled && time.Now().Before(s.pairingExp) {
		st.Code = s.code
	}
	return st
}

// ExchangePairingCode validates the 6-digit code and returns a new bearer token
// for the paired device. The token is stored as a hash only.
func (s *Service) ExchangePairingCode(ctx context.Context, code, deviceName, ipAddr, ua string) (string, error) {
	s.mu.RLock()
	enabled := s.enabled
	storedCode := s.code
	exp := s.pairingExp
	s.mu.RUnlock()

	if !enabled {
		return "", fmt.Errorf("remote: remote access is not enabled")
	}
	if time.Now().After(exp) {
		return "", fmt.Errorf("remote: pairing code has expired")
	}
	if code != storedCode {
		return "", fmt.Errorf("remote: invalid pairing code")
	}

	// Generate a 32-byte random device bearer token.
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", fmt.Errorf("remote: generate device token: %w", err)
	}
	tokenStr := hex.EncodeToString(rawToken)

	hash := sha256.Sum256(rawToken)
	tokenHash := hex.EncodeToString(hash[:])

	if s.store != nil {
		d := storage.PairedDevice{
			DeviceID:   generateDeviceID(),
			DeviceName: deviceName,
			TokenHash:  tokenHash,
			IPAddress:  ipAddr,
			UserAgent:  ua,
			PairedAt:   time.Now().UTC(),
		}
		if err := s.store.InsertDevice(ctx, d); err != nil {
			return "", fmt.Errorf("remote: store device: %w", err)
		}
	}

	return tokenStr, nil
}

// ValidateDeviceToken checks a bearer token against stored paired devices.
// Returns the device on success, nil on failure.
func (s *Service) ValidateDeviceToken(ctx context.Context, rawToken string) (*storage.PairedDevice, error) {
	if s.store == nil {
		return nil, nil
	}
	b, err := hex.DecodeString(rawToken)
	if err != nil {
		return nil, nil
	}
	hash := sha256.Sum256(b)
	tokenHash := hex.EncodeToString(hash[:])

	d, err := s.store.GetDeviceByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("remote: validate token: %w", err)
	}
	if d != nil {
		go func() {
			_ = s.store.UpdateDeviceLastSeen(ctx, d.DeviceID, time.Now().UTC())
		}()
	}
	return d, nil
}

// ListDevices returns all paired devices.
func (s *Service) ListDevices(ctx context.Context) ([]storage.PairedDevice, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.ListDevices(ctx)
}

// RevokeDevice removes a paired device by device_id.
func (s *Service) RevokeDevice(ctx context.Context, deviceID string) error {
	if s.store == nil {
		return nil
	}
	return s.store.DeleteDevice(ctx, deviceID)
}

func generateDeviceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "dev_" + hex.EncodeToString(b)
}
