package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PairedDevice represents a single paired remote device.
type PairedDevice struct {
	DeviceID   string
	DeviceName string
	TokenHash  string // hex(sha256(raw_token)) — plaintext never stored
	IPAddress  string
	UserAgent  string
	PairedAt   time.Time
	LastSeenAt *time.Time
}

// InsertDevice stores a newly paired device.
func (s *Store) InsertDevice(ctx context.Context, d PairedDevice) error {
	const q = `INSERT INTO paired_devices
		(device_id, device_name, token_hash, ip_address, user_agent, paired_at)
		VALUES (?,?,?,?,?,?)`
	_, err := s.db.ExecContext(ctx, q,
		d.DeviceID, d.DeviceName, d.TokenHash, d.IPAddress, d.UserAgent,
		d.PairedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// GetDeviceByTokenHash looks up a device by the SHA-256 hash of its bearer token.
func (s *Store) GetDeviceByTokenHash(ctx context.Context, hash string) (*PairedDevice, error) {
	const q = `SELECT device_id, device_name, token_hash, ip_address, user_agent,
		paired_at, last_seen_at
		FROM paired_devices WHERE token_hash = ? LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, hash)
	return scanDevice(row)
}

// ListDevices returns all paired devices ordered by paired_at desc.
func (s *Store) ListDevices(ctx context.Context) ([]PairedDevice, error) {
	const q = `SELECT device_id, device_name, token_hash, ip_address, user_agent,
		paired_at, last_seen_at
		FROM paired_devices ORDER BY paired_at DESC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []PairedDevice
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// DeleteDevice removes a paired device by device_id.
func (s *Store) DeleteDevice(ctx context.Context, deviceID string) error {
	const q = `DELETE FROM paired_devices WHERE device_id = ?`
	_, err := s.db.ExecContext(ctx, q, deviceID)
	return err
}

// UpdateDeviceLastSeen updates the last_seen_at timestamp for a device.
func (s *Store) UpdateDeviceLastSeen(ctx context.Context, deviceID string, at time.Time) error {
	const q = `UPDATE paired_devices SET last_seen_at = ? WHERE device_id = ?`
	_, err := s.db.ExecContext(ctx, q, at.UTC().Format(time.RFC3339), deviceID)
	return err
}

// scanner abstracts *sql.Row and *sql.Rows for scanDevice.
type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(s scanner) (*PairedDevice, error) {
	var d PairedDevice
	var pairedStr string
	var lastSeenStr sql.NullString
	if err := s.Scan(
		&d.DeviceID, &d.DeviceName, &d.TokenHash,
		&d.IPAddress, &d.UserAgent,
		&pairedStr, &lastSeenStr,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if t, err := time.Parse(time.RFC3339, pairedStr); err == nil {
		d.PairedAt = t
	}
	if lastSeenStr.Valid {
		if t, err := time.Parse(time.RFC3339, lastSeenStr.String); err == nil {
			d.LastSeenAt = &t
		}
	}
	return &d, nil
}
