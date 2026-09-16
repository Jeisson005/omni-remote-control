package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Jeisson005/omni-remote-control/server/internal/models"
	_ "github.com/lib/pq"
)

type DB struct {
	conn *sql.DB
}

func Connect(dsn string) (*DB, error) {
	var conn *sql.DB
	var err error

	// Retry connection if PostgreSQL is still starting up
	for i := 0; i < 15; i++ {
		conn, err = sql.Open("postgres", dsn)
		if err == nil {
			if pingErr := conn.Ping(); pingErr == nil {
				log.Println("Successfully connected to PostgreSQL database")
				break
			} else {
				err = pingErr
			}
		}
		log.Printf("Waiting for database to be ready (%d/15)...: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("could not connect to database: %w", err)
	}

	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return d, nil
}

func (d *DB) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS devices (
			id VARCHAR(128) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			hostname VARCHAR(255) NOT NULL,
			os VARCHAR(64) NOT NULL,
			platform VARCHAR(128) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'offline',
			first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS device_system_info (
			device_id VARCHAR(128) PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			cpu_model VARCHAR(255),
			cpu_cores INT,
			ram_total_bytes BIGINT,
			disk_total_bytes BIGINT,
			os_version VARCHAR(255),
			kernel_version VARCHAR(255),
			arch VARCHAR(64),
			ip_address VARCHAR(128),
			mac_address VARCHAR(128),
			timezone VARCHAR(64),
			agent_version VARCHAR(64),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS telemetry_metrics (
			id BIGSERIAL PRIMARY KEY,
			device_id VARCHAR(128) REFERENCES devices(id) ON DELETE CASCADE,
			cpu_usage_pct NUMERIC(5,2),
			ram_usage_pct NUMERIC(5,2),
			ram_used_bytes BIGINT,
			disk_usage_pct NUMERIC(5,2),
			open_windows JSONB,
			top_processes JSONB,
			recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_telemetry_device_time ON telemetry_metrics(device_id, recorded_at DESC);`,
		`CREATE TABLE IF NOT EXISTS commands (
			id VARCHAR(64) PRIMARY KEY,
			device_id VARCHAR(128) REFERENCES devices(id) ON DELETE CASCADE,
			type VARCHAR(64) NOT NULL,
			payload JSONB NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			exit_code INT DEFAULT 0,
			output TEXT DEFAULT '',
			error TEXT DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ
		);`,
		`CREATE INDEX IF NOT EXISTS idx_commands_device_created ON commands(device_id, created_at DESC);`,
	}

	for _, q := range queries {
		if _, err := d.conn.Exec(q); err != nil {
			return err
		}
	}
	log.Println("Database schema migrated successfully")
	return nil
}

func (d *DB) UpsertDevice(dev *models.Device) error {
	query := `
		INSERT INTO devices (id, name, hostname, os, platform, status, first_seen_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			hostname = EXCLUDED.hostname,
			os = EXCLUDED.os,
			platform = EXCLUDED.platform,
			status = EXCLUDED.status,
			last_seen_at = NOW();
	`
	_, err := d.conn.Exec(query, dev.ID, dev.Name, dev.Hostname, dev.OS, dev.Platform, dev.Status)
	return err
}

func (d *DB) UpdateDeviceStatus(deviceID, status string) error {
	query := `UPDATE devices SET status = $1, last_seen_at = NOW() WHERE id = $2`
	_, err := d.conn.Exec(query, status, deviceID)
	return err
}

func (d *DB) UpsertSystemInfo(info *models.DeviceSystemInfo) error {
	query := `
		INSERT INTO device_system_info (
			device_id, cpu_model, cpu_cores, ram_total_bytes, disk_total_bytes,
			os_version, kernel_version, arch, ip_address, mac_address, timezone, agent_version, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		ON CONFLICT (device_id) DO UPDATE SET
			cpu_model = EXCLUDED.cpu_model,
			cpu_cores = EXCLUDED.cpu_cores,
			ram_total_bytes = EXCLUDED.ram_total_bytes,
			disk_total_bytes = EXCLUDED.disk_total_bytes,
			os_version = EXCLUDED.os_version,
			kernel_version = EXCLUDED.kernel_version,
			arch = EXCLUDED.arch,
			ip_address = EXCLUDED.ip_address,
			mac_address = EXCLUDED.mac_address,
			timezone = EXCLUDED.timezone,
			agent_version = EXCLUDED.agent_version,
			updated_at = NOW();
	`
	_, err := d.conn.Exec(query,
		info.DeviceID, info.CPUModel, info.CPUCores, info.RAMTotalBytes, info.DiskTotalBytes,
		info.OSVersion, info.KernelVersion, info.Arch, info.IPAddress, info.MACAddress,
		info.Timezone, info.AgentVersion,
	)
	return err
}

func (d *DB) InsertTelemetry(m *models.TelemetryMetric) error {
	windowsJSON, err := json.Marshal(m.OpenWindows)
	if err != nil {
		windowsJSON = []byte("[]")
	}
	procJSON, err := json.Marshal(m.TopProcesses)
	if err != nil {
		procJSON = []byte("[]")
	}

	recordedAt := m.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now()
	}

	query := `
		INSERT INTO telemetry_metrics (
			device_id, cpu_usage_pct, ram_usage_pct, ram_used_bytes, disk_usage_pct,
			open_windows, top_processes, recorded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = d.conn.Exec(query,
		m.DeviceID, m.CPUUsagePct, m.RAMUsagePct, m.RAMUsedBytes, m.DiskUsagePct,
		windowsJSON, procJSON, recordedAt,
	)
	return err
}

func (d *DB) GetDevices() ([]models.Device, error) {
	query := `
		SELECT d.id, d.name, d.hostname, d.os, d.platform, d.status, d.first_seen_at, d.last_seen_at,
		       s.cpu_model, s.cpu_cores, s.ram_total_bytes, s.disk_total_bytes,
		       s.os_version, s.kernel_version, s.arch, s.ip_address, s.mac_address, s.timezone, s.agent_version, s.updated_at
		FROM devices d
		LEFT JOIN device_system_info s ON d.id = s.device_id
		ORDER BY d.last_seen_at DESC
	`
	rows, err := d.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []models.Device
	for rows.Next() {
		var dev models.Device
		var s models.DeviceSystemInfo
		var (
			cpuModel, osVersion, kernelVersion, arch, ip, mac, timezone, agentVer sql.NullString
			cpuCores                                                              sql.NullInt64
			ramTotal, diskTotal                                                   sql.NullInt64
			sysUpdatedAt                                                          sql.NullTime
		)

		err := rows.Scan(
			&dev.ID, &dev.Name, &dev.Hostname, &dev.OS, &dev.Platform, &dev.Status, &dev.FirstSeenAt, &dev.LastSeenAt,
			&cpuModel, &cpuCores, &ramTotal, &diskTotal,
			&osVersion, &kernelVersion, &arch, &ip, &mac, &timezone, &agentVer, &sysUpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if cpuModel.Valid {
			s.DeviceID = dev.ID
			s.CPUModel = cpuModel.String
			s.CPUCores = int(cpuCores.Int64)
			s.RAMTotalBytes = uint64(ramTotal.Int64)
			s.DiskTotalBytes = uint64(diskTotal.Int64)
			s.OSVersion = osVersion.String
			s.KernelVersion = kernelVersion.String
			s.Arch = arch.String
			s.IPAddress = ip.String
			s.MACAddress = mac.String
			s.Timezone = timezone.String
			s.AgentVersion = agentVer.String
			s.UpdatedAt = sysUpdatedAt.Time
			dev.SystemInfo = &s
		}

		devices = append(devices, dev)
	}

	return devices, nil
}

func (d *DB) GetDevice(id string) (*models.Device, error) {
	query := `
		SELECT d.id, d.name, d.hostname, d.os, d.platform, d.status, d.first_seen_at, d.last_seen_at,
		       s.cpu_model, s.cpu_cores, s.ram_total_bytes, s.disk_total_bytes,
		       s.os_version, s.kernel_version, s.arch, s.ip_address, s.mac_address, s.timezone, s.agent_version, s.updated_at
		FROM devices d
		LEFT JOIN device_system_info s ON d.id = s.device_id
		WHERE d.id = $1
	`
	row := d.conn.QueryRow(query, id)

	var dev models.Device
	var s models.DeviceSystemInfo
	var (
		cpuModel, osVersion, kernelVersion, arch, ip, mac, timezone, agentVer sql.NullString
		cpuCores                                                              sql.NullInt64
		ramTotal, diskTotal                                                   sql.NullInt64
		sysUpdatedAt                                                          sql.NullTime
	)

	err := row.Scan(
		&dev.ID, &dev.Name, &dev.Hostname, &dev.OS, &dev.Platform, &dev.Status, &dev.FirstSeenAt, &dev.LastSeenAt,
		&cpuModel, &cpuCores, &ramTotal, &diskTotal,
		&osVersion, &kernelVersion, &arch, &ip, &mac, &timezone, &agentVer, &sysUpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if cpuModel.Valid {
		s.DeviceID = dev.ID
		s.CPUModel = cpuModel.String
		s.CPUCores = int(cpuCores.Int64)
		s.RAMTotalBytes = uint64(ramTotal.Int64)
		s.DiskTotalBytes = uint64(diskTotal.Int64)
		s.OSVersion = osVersion.String
		s.KernelVersion = kernelVersion.String
		s.Arch = arch.String
		s.IPAddress = ip.String
		s.MACAddress = mac.String
		s.Timezone = timezone.String
		s.AgentVersion = agentVer.String
		s.UpdatedAt = sysUpdatedAt.Time
		dev.SystemInfo = &s
	}

	return &dev, nil
}

func (d *DB) GetTelemetry(deviceID string, limit int) ([]models.TelemetryMetric, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT id, device_id, cpu_usage_pct, ram_usage_pct, ram_used_bytes, disk_usage_pct,
		       open_windows, top_processes, recorded_at
		FROM telemetry_metrics
		WHERE device_id = $1
		ORDER BY recorded_at DESC
		LIMIT $2
	`
	rows, err := d.conn.Query(query, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.TelemetryMetric
	for rows.Next() {
		var m models.TelemetryMetric
		var windowsJSON, procJSON []byte

		err := rows.Scan(
			&m.ID, &m.DeviceID, &m.CPUUsagePct, &m.RAMUsagePct, &m.RAMUsedBytes, &m.DiskUsagePct,
			&windowsJSON, &procJSON, &m.RecordedAt,
		)
		if err != nil {
			return nil, err
		}

		_ = json.Unmarshal(windowsJSON, &m.OpenWindows)
		_ = json.Unmarshal(procJSON, &m.TopProcesses)
		list = append(list, m)
	}

	return list, nil
}

func (d *DB) InsertCommand(cmd *models.Command) error {
	payloadJSON, err := json.Marshal(cmd.Payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}

	query := `
		INSERT INTO commands (id, device_id, type, payload, status, exit_code, output, error, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`
	_, err = d.conn.Exec(query, cmd.ID, cmd.DeviceID, cmd.Type, payloadJSON, cmd.Status, cmd.ExitCode, cmd.Output, cmd.Error)
	return err
}

func (d *DB) UpdateCommand(cmd *models.Command) error {
	query := `
		UPDATE commands
		SET status = $1, exit_code = $2, output = $3, error = $4, completed_at = NOW()
		WHERE id = $5
	`
	_, err := d.conn.Exec(query, cmd.Status, cmd.ExitCode, cmd.Output, cmd.Error, cmd.ID)
	return err
}

func (d *DB) GetCommand(id string) (*models.Command, error) {
	query := `
		SELECT id, device_id, type, payload, status, exit_code, output, error, created_at, completed_at
		FROM commands
		WHERE id = $1
	`
	row := d.conn.QueryRow(query, id)

	var cmd models.Command
	var payloadBytes []byte
	var completedAt sql.NullTime

	err := row.Scan(
		&cmd.ID, &cmd.DeviceID, &cmd.Type, &payloadBytes, &cmd.Status,
		&cmd.ExitCode, &cmd.Output, &cmd.Error, &cmd.CreatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(payloadBytes, &cmd.Payload)
	if completedAt.Valid {
		cmd.CompletedAt = &completedAt.Time
	}

	return &cmd, nil
}
