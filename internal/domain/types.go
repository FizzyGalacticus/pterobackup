package domain

// AuthMethod defines which SSH authentication strategy to use.
type AuthMethod string

const (
	AuthMethodPassword AuthMethod = "password"
	AuthMethodKey      AuthMethod = "key"
	DefaultIntervalMin            = 60
	DefaultMaxBackups             = 20
)

// SSHConfig defines how the application connects to the remote host.
type SSHConfig struct {
	Host            string     `json:"host"`
	Port            int        `json:"port"`
	Username        string     `json:"username"`
	AuthMethod      AuthMethod `json:"authMethod"`
	Password        string     `json:"password,omitempty"`
	PrivateKeyPath  string     `json:"privateKeyPath,omitempty"`
	PrivateKeyValue string     `json:"privateKeyValue,omitempty"`
}

// BackupItem describes a container path and the local destination for backups.
type BackupItem struct {
	ID              string `json:"id"`
	ContainerName   string `json:"containerName"`
	ContainerPath   string `json:"containerPath"`
	BackupName      string `json:"backupName,omitempty"`
	LocalTargetPath string `json:"localTargetPath,omitempty"`
	IntervalMinutes int    `json:"intervalMinutes"`
	MaxBackups      int    `json:"maxBackups"`
}

// AppConfig stores all persisted application settings.
type AppConfig struct {
	SSH     SSHConfig    `json:"ssh"`
	Backups []BackupItem `json:"backups"`
}

// BackupOutcome reports the output path and metadata for one backup run.
type BackupOutcome struct {
	ItemID       string `json:"itemId"`
	ArchivePath  string `json:"archivePath"`
	IsCompressed bool   `json:"isCompressed"`
}

// BackupRunResult contains per-item outcomes.
type BackupRunResult struct {
	Results []BackupOutcome `json:"results"`
}

// BackupScheduleStatus describes scheduler state for one backup item.
type BackupScheduleStatus struct {
	NextRunAt     string `json:"nextRunAt,omitempty"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}
