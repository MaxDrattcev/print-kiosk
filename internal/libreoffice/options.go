package libreoffice

// Options controls LibreOffice discovery / auto-install.
type Options struct {
	Configured  string
	AutoInstall bool
	// MSIURL optional direct installer URL (Windows MSI). Empty = resolve latest.
	MSIURL string
}
