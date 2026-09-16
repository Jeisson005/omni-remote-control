package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
		`CREATE TABLE IF NOT EXISTS device_events (
			id BIGSERIAL PRIMARY KEY,
			device_id VARCHAR(128) REFERENCES devices(id) ON DELETE CASCADE,
			event_type VARCHAR(64) NOT NULL,
			severity VARCHAR(32) NOT NULL DEFAULT 'info',
			message TEXT NOT NULL DEFAULT '',
			metadata JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`,
		`CREATE INDEX IF NOT EXISTS idx_device_events_device_time ON device_events(device_id, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id BIGSERIAL PRIMARY KEY,
			device_id VARCHAR(128) REFERENCES devices(id) ON DELETE CASCADE,
			external_id VARCHAR(512) NOT NULL,
			package_name VARCHAR(255) NOT NULL DEFAULT '',
			app_name VARCHAR(255),
			title TEXT,
			text TEXT,
			sub_text TEXT,
			category VARCHAR(128),
			is_ongoing BOOLEAN NOT NULL DEFAULT FALSE,
			is_clearable BOOLEAN NOT NULL DEFAULT TRUE,
			actions JSONB,
			posted_at TIMESTAMPTZ,
			received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(device_id, external_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_device_time ON notifications(device_id, received_at DESC);`,
		`CREATE TABLE IF NOT EXISTS sms_messages (
			id BIGSERIAL PRIMARY KEY,
			device_id VARCHAR(128) REFERENCES devices(id) ON DELETE CASCADE,
			external_id VARCHAR(512) NOT NULL,
			direction VARCHAR(16) NOT NULL DEFAULT 'inbound',
			address VARCHAR(128) NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			person VARCHAR(255),
			read BOOLEAN NOT NULL DEFAULT FALSE,
			sms_timestamp TIMESTAMPTZ,
			received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(device_id, external_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sms_messages_device_time ON sms_messages(device_id, received_at DESC);`,
		`ALTER TABLE devices ADD COLUMN IF NOT EXISTS fcm_token VARCHAR(512);`,
		`ALTER TABLE telemetry_metrics ADD COLUMN IF NOT EXISTS battery_pct NUMERIC(5,2);`,
		`ALTER TABLE telemetry_metrics ADD COLUMN IF NOT EXISTS is_charging BOOLEAN;`,
		`ALTER TABLE telemetry_metrics ADD COLUMN IF NOT EXISTS network_name VARCHAR(128);`,
		`ALTER TABLE telemetry_metrics ADD COLUMN IF NOT EXISTS public_ip VARCHAR(64);`,
		`ALTER TABLE telemetry_metrics ADD COLUMN IF NOT EXISTS uptime_seconds BIGINT;`,
		`ALTER TABLE device_system_info ADD COLUMN IF NOT EXISTS network_name VARCHAR(128);`,
		`ALTER TABLE device_system_info ADD COLUMN IF NOT EXISTS public_ip VARCHAR(64);`,
		`ALTER TABLE device_system_info ADD COLUMN IF NOT EXISTS uptime_seconds BIGINT;`,
		`ALTER TABLE device_system_info ADD COLUMN IF NOT EXISTS control_mode VARCHAR(16);`,
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

func (d *DB) UpdateDeviceFCMToken(deviceID, token string) error {
	query := `UPDATE devices SET fcm_token = $1, last_seen_at = NOW() WHERE id = $2`
	_, err := d.conn.Exec(query, token, deviceID)
	return err
}

func (d *DB) GetDeviceFCMToken(deviceID string) (string, error) {
	var token sql.NullString
	query := `SELECT fcm_token FROM devices WHERE id = $1`
	err := d.conn.QueryRow(query, deviceID).Scan(&token)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !token.Valid {
		return "", nil
	}
	return token.String, nil
}

func (d *DB) UpsertSystemInfo(info *models.DeviceSystemInfo) error {
	query := `
		INSERT INTO device_system_info (
			device_id, cpu_model, cpu_cores, ram_total_bytes, disk_total_bytes,
			os_version, kernel_version, arch, ip_address, mac_address, timezone, agent_version,
			public_ip, network_name, uptime_seconds, control_mode, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())
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
			public_ip = COALESCE(NULLIF(EXCLUDED.public_ip, ''), device_system_info.public_ip),
			network_name = COALESCE(NULLIF(EXCLUDED.network_name, ''), device_system_info.network_name),
			uptime_seconds = EXCLUDED.uptime_seconds,
			control_mode = COALESCE(NULLIF(EXCLUDED.control_mode, ''), device_system_info.control_mode),
			updated_at = NOW();
	`
	_, err := d.conn.Exec(query,
		info.DeviceID, info.CPUModel, info.CPUCores, info.RAMTotalBytes, info.DiskTotalBytes,
		info.OSVersion, info.KernelVersion, info.Arch, info.IPAddress, info.MACAddress,
		info.Timezone, info.AgentVersion, info.PublicIP, info.NetworkName, info.UptimeSeconds,
		info.ControlMode,
	)
	return err
}

func (d *DB) ensureDeviceExists(deviceID string) {
	if deviceID == "" {
		return
	}
	osType := "unknown"
	platform := "Generic"
	if strings.HasPrefix(deviceID, "android-") {
		osType = "android"
		platform = "Android Mobile"
	} else if strings.HasPrefix(deviceID, "win-") {
		osType = "windows"
		platform = "Windows Desktop"
	} else if strings.HasPrefix(deviceID, "linux-") {
		osType = "linux"
		platform = "Linux Machine"
	}

	_ = d.UpsertDevice(&models.Device{
		ID:       deviceID,
		Name:     deviceID,
		Hostname: deviceID,
		OS:       osType,
		Platform: platform,
		Status:   "online",
	})
}

func (d *DB) InsertTelemetry(m *models.TelemetryMetric) error {
	d.ensureDeviceExists(m.DeviceID)

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
			battery_pct, is_charging, network_name, public_ip, uptime_seconds,
			open_windows, top_processes, recorded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err = d.conn.Exec(query,
		m.DeviceID, m.CPUUsagePct, m.RAMUsagePct, m.RAMUsedBytes, m.DiskUsagePct,
		m.BatteryPct, m.IsCharging, m.NetworkName, m.PublicIP, m.UptimeSeconds,
		windowsJSON, procJSON, recordedAt,
	)
	return err
}

func (d *DB) InsertEvent(ev *models.DeviceEvent) error {
	d.ensureDeviceExists(ev.DeviceID)

	metadataJSON, err := json.Marshal(ev.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	createdAt := ev.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	query := `
		INSERT INTO device_events (device_id, event_type, severity, message, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = d.conn.Exec(query, ev.DeviceID, ev.EventType, ev.Severity, ev.Message, metadataJSON, createdAt)
	return err
}

func (d *DB) GetEvents(deviceID, eventType string, limit int) ([]models.DeviceEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var rows *sql.Rows
	var err error
	if eventType != "" {
		query := `
			SELECT id, device_id, event_type, severity, message, metadata, created_at
			FROM device_events
			WHERE device_id = $1 AND event_type = $2
			ORDER BY created_at DESC
			LIMIT $3
		`
		rows, err = d.conn.Query(query, deviceID, eventType, limit)
	} else {
		query := `
			SELECT id, device_id, event_type, severity, message, metadata, created_at
			FROM device_events
			WHERE device_id = $1
			ORDER BY created_at DESC
			LIMIT $2
		`
		rows, err = d.conn.Query(query, deviceID, limit)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.DeviceEvent
	for rows.Next() {
		var ev models.DeviceEvent
		var metaJSON []byte
		if err := rows.Scan(&ev.ID, &ev.DeviceID, &ev.EventType, &ev.Severity, &ev.Message, &metaJSON, &ev.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metaJSON, &ev.Metadata)
		events = append(events, ev)
	}
	return events, nil
}

func (d *DB) GetDevices() ([]models.Device, error) {
	query := `
		SELECT d.id, d.name, d.hostname, d.os, d.platform, d.status, d.first_seen_at, d.last_seen_at,
		       s.cpu_model, s.cpu_cores, s.ram_total_bytes, s.disk_total_bytes,
		       s.os_version, s.kernel_version, s.arch, s.ip_address, s.mac_address, s.timezone, s.agent_version,
		       s.public_ip, s.network_name, s.uptime_seconds, s.control_mode, s.updated_at
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
			cpuModel, osVersion, kernelVersion, arch, ip, mac, timezone, agentVer, pubIP, netName, controlMode sql.NullString
			cpuCores, ramTotal, diskTotal, uptimeSec                                                             sql.NullInt64
			sysUpdatedAt                                                                                         sql.NullTime
		)

		err := rows.Scan(
			&dev.ID, &dev.Name, &dev.Hostname, &dev.OS, &dev.Platform, &dev.Status, &dev.FirstSeenAt, &dev.LastSeenAt,
			&cpuModel, &cpuCores, &ramTotal, &diskTotal,
			&osVersion, &kernelVersion, &arch, &ip, &mac, &timezone, &agentVer,
			&pubIP, &netName, &uptimeSec, &controlMode, &sysUpdatedAt,
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
			s.PublicIP = pubIP.String
			s.NetworkName = netName.String
			s.UptimeSeconds = uint64(uptimeSec.Int64)
			s.ControlMode = controlMode.String
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
		       s.os_version, s.kernel_version, s.arch, s.ip_address, s.mac_address, s.timezone, s.agent_version,
		       s.public_ip, s.network_name, s.uptime_seconds, s.control_mode, s.updated_at
		FROM devices d
		LEFT JOIN device_system_info s ON d.id = s.device_id
		WHERE d.id = $1
	`
	row := d.conn.QueryRow(query, id)

	var dev models.Device
	var s models.DeviceSystemInfo
	var (
		cpuModel, osVersion, kernelVersion, arch, ip, mac, timezone, agentVer, pubIP, netName, controlMode sql.NullString
		cpuCores, ramTotal, diskTotal, uptimeSec                                                             sql.NullInt64
		sysUpdatedAt                                                                                         sql.NullTime
	)

	err := row.Scan(
		&dev.ID, &dev.Name, &dev.Hostname, &dev.OS, &dev.Platform, &dev.Status, &dev.FirstSeenAt, &dev.LastSeenAt,
		&cpuModel, &cpuCores, &ramTotal, &diskTotal,
		&osVersion, &kernelVersion, &arch, &ip, &mac, &timezone, &agentVer,
		&pubIP, &netName, &uptimeSec, &controlMode, &sysUpdatedAt,
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
		s.PublicIP = pubIP.String
		s.NetworkName = netName.String
		s.UptimeSeconds = uint64(uptimeSec.Int64)
		s.ControlMode = controlMode.String
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
		       battery_pct, is_charging, network_name, public_ip, uptime_seconds,
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
		var (
			batteryPct                  sql.NullFloat64
			isCharging                  sql.NullBool
			networkName, publicIP       sql.NullString
			uptimeSec                   sql.NullInt64
			windowsJSON, procJSON       []byte
		)

		err := rows.Scan(
			&m.ID, &m.DeviceID, &m.CPUUsagePct, &m.RAMUsagePct, &m.RAMUsedBytes, &m.DiskUsagePct,
			&batteryPct, &isCharging, &networkName, &publicIP, &uptimeSec,
			&windowsJSON, &procJSON, &m.RecordedAt,
		)
		if err != nil {
			return nil, err
		}

		if batteryPct.Valid {
			val := batteryPct.Float64
			m.BatteryPct = &val
		}
		if isCharging.Valid {
			val := isCharging.Bool
			m.IsCharging = &val
		}
		if networkName.Valid {
			m.NetworkName = networkName.String
		}
		if publicIP.Valid {
			m.PublicIP = publicIP.String
		}
		if uptimeSec.Valid {
			m.UptimeSeconds = uint64(uptimeSec.Int64)
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

func (d *DB) InsertNotification(n *models.NotificationRecord) error {
	d.ensureDeviceExists(n.DeviceID)

	if n.ExternalID == "" {
		n.ExternalID = fmt.Sprintf("%s-%.0f", n.PackageName, float64(time.Now().UnixNano()))
	}
	actionsJSON, err := json.Marshal(n.Actions)
	if err != nil {
		actionsJSON = []byte("[]")
	}
	postedAt := n.PostedAt
	if postedAt.IsZero() {
		postedAt = time.Now()
	}
	receivedAt := n.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	query := `
		INSERT INTO notifications (
			device_id, external_id, package_name, app_name, title, text, sub_text,
			category, is_ongoing, is_clearable, actions, posted_at, received_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (device_id, external_id) DO UPDATE SET
			title = EXCLUDED.title,
			text = EXCLUDED.text,
			sub_text = EXCLUDED.sub_text,
			category = EXCLUDED.category,
			is_ongoing = EXCLUDED.is_ongoing,
			is_clearable = EXCLUDED.is_clearable,
			actions = EXCLUDED.actions,
			received_at = EXCLUDED.received_at;
	`
	_, err = d.conn.Exec(query,
		n.DeviceID, n.ExternalID, n.PackageName, n.AppName, n.Title, n.Text, n.SubText,
		n.Category, n.IsOngoing, n.IsClearable, actionsJSON, postedAt, receivedAt,
	)
	return err
}

func (d *DB) GetNotifications(deviceID string, limit int) ([]models.NotificationRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := `
		SELECT id, device_id, external_id, package_name, app_name, title, text, sub_text,
		       category, is_ongoing, is_clearable, actions, posted_at, received_at
		FROM notifications
		WHERE device_id = $1
		ORDER BY received_at DESC
		LIMIT $2
	`
	rows, err := d.conn.Query(query, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.NotificationRecord
	for rows.Next() {
		var n models.NotificationRecord
		var (
			appName, title, text, subText, category sql.NullString
			actionsJSON                            []byte
			postedAt                               sql.NullTime
		)
		if err := rows.Scan(
			&n.ID, &n.DeviceID, &n.ExternalID, &n.PackageName, &appName, &title, &text, &subText,
			&category, &n.IsOngoing, &n.IsClearable, &actionsJSON, &postedAt, &n.ReceivedAt,
		); err != nil {
			return nil, err
		}
		n.AppName = appName.String
		n.Title = title.String
		n.Text = text.String
		n.SubText = subText.String
		n.Category = category.String
		n.PostedAt = postedAt.Time
		_ = json.Unmarshal(actionsJSON, &n.Actions)
		list = append(list, n)
	}
	return list, nil
}

func (d *DB) InsertSms(s *models.SmsMessage) error {
	d.ensureDeviceExists(s.DeviceID)

	if s.ExternalID == "" {
		s.ExternalID = fmt.Sprintf("sms-%s-%.0f", s.Address, float64(time.Now().UnixNano()))
	}
	if s.Direction == "" {
		s.Direction = "inbound"
	}
	timestamp := s.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	receivedAt := s.ReceivedAt
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}

	query := `
		INSERT INTO sms_messages (
			device_id, external_id, direction, address, body, person, read, sms_timestamp, received_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (device_id, external_id) DO UPDATE SET
			body = EXCLUDED.body,
			read = EXCLUDED.read,
			received_at = EXCLUDED.received_at;
	`
	_, err := d.conn.Exec(query,
		s.DeviceID, s.ExternalID, s.Direction, s.Address, s.Body, s.Person, s.Read, timestamp, receivedAt,
	)
	return err
}

func (d *DB) GetSmsMessages(deviceID string, limit int) ([]models.SmsMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := `
		SELECT id, device_id, external_id, direction, address, body, person, read, sms_timestamp, received_at
		FROM sms_messages
		WHERE device_id = $1
		ORDER BY COALESCE(sms_timestamp, received_at) DESC
		LIMIT $2
	`
	rows, err := d.conn.Query(query, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.SmsMessage
	for rows.Next() {
		var s models.SmsMessage
		var person sql.NullString
		var ts sql.NullTime
		if err := rows.Scan(
			&s.ID, &s.DeviceID, &s.ExternalID, &s.Direction, &s.Address, &s.Body, &person, &s.Read, &ts, &s.ReceivedAt,
		); err != nil {
			return nil, err
		}
		s.Person = person.String
		s.Timestamp = ts.Time
		list = append(list, s)
	}
	return list, nil
}
