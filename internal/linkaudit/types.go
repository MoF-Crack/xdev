package linkaudit

import (
	"net/http"
	"time"
)

type Mode string

const (
	ModeProbe Mode = "probe"
	ModeFull  Mode = "full"

	StatusPass                  = "PASS"
	StatusURLDead               = "URL_DEAD"
	StatusMirrorDead            = "MIRROR_DEAD"
	StatusHashMismatch          = "HASH_MISMATCH"
	StatusHTMLInsteadOfArchive  = "HTML_INSTEAD_OF_ARCHIVE"
	StatusUnsupportedLayout     = "UNSUPPORTED_LAYOUT"
	StatusAuthOrLicenseRequired = "AUTH_OR_LICENSE_REQUIRED"
	StatusImplementationMissing = "IMPLEMENTATION_MISSING"
	StatusMissingChecksum       = "MISSING_CHECKSUM"
	StatusTransientError        = "TRANSIENT_ERROR"
)

type Request struct {
	ManifestDir       string
	Platforms         []string
	Tools             []string
	Versions          []string
	Roles             []string
	Mode              Mode
	CacheDir          string
	Timeout           time.Duration
	Concurrency       int
	JSONOutput        string
	MarkdownOutput    string
	Entries           []Entry
	ExpectedSHA256    string
	Client            *http.Client
	Clock             func() time.Time
	AllowHTTPForTests bool
}

type Entry struct {
	Tool, Version, OS, Arch, Role, URL, ExpectedSHA256, Format string
	PreStatus, Detail                                          string
}

type Result struct {
	Tool            string    `json:"tool"`
	Version         string    `json:"version"`
	OS              string    `json:"os"`
	Arch            string    `json:"arch"`
	Role            string    `json:"role"`
	SourceURL       string    `json:"sourceUrl"`
	FinalURL        string    `json:"finalUrl"`
	StatusCode      int       `json:"statusCode"`
	ContentType     string    `json:"contentType"`
	ContentLength   int64     `json:"contentLength"`
	SupportsRange   bool      `json:"supportsRange"`
	ExpectedSHA256  string    `json:"expectedSha256"`
	ActualSHA256    string    `json:"actualSha256"`
	SHA256Matches   bool      `json:"sha256Matches"`
	ArchiveDetected string    `json:"archiveDetected"`
	Available       bool      `json:"available"`
	FailureReason   string    `json:"failureReason"`
	Detail          string    `json:"detail,omitempty"`
	CheckedAt       time.Time `json:"checkedAt"`
}
