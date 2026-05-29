package main

import "time"

type BookSource map[string]any

type ProbeTarget struct {
	URL    string
	Method string
	Body   string
}

type ProbeJob struct {
	Target ProbeTarget
	Source BookSource
}

type ProbeResult struct {
	ProbeURL        string   `json:"probeUrl" bson:"probeUrl"`
	Method          string   `json:"method" bson:"method"`
	Body            string   `json:"body,omitempty" bson:"body,omitempty"`
	Domain          string   `json:"domain" bson:"domain"`
	OK              bool     `json:"ok" bson:"ok"`
	Status          int      `json:"status" bson:"status"`
	FinalURL        string   `json:"finalUrl" bson:"finalUrl"`
	Error           string   `json:"error" bson:"error"`
	MS              int64    `json:"ms" bson:"ms"`
	Attempts        int      `json:"attempts" bson:"attempts"`
	BodyBytes       int      `json:"bodyBytes" bson:"bodyBytes"`
	ValidationLevel int      `json:"validationLevel" bson:"validationLevel"`
	ValidationGrade string   `json:"validationGrade" bson:"validationGrade"`
	ValidationNotes []string `json:"validationNotes" bson:"validationNotes"`
}

type DuplicateGroup struct {
	NormalizedURL string   `json:"normalizedUrl" bson:"normalizedUrl"`
	Count         int      `json:"count" bson:"count"`
	Names         []string `json:"names" bson:"names"`
	RawURLs       []string `json:"rawUrls" bson:"rawUrls"`
}

type DedupDoc struct {
	ID              string     `bson:"_id"`
	NormalizedURL   string     `json:"normalizedUrl" bson:"normalizedUrl"`
	ProbeURL        string     `json:"probeUrl" bson:"probeUrl"`
	Method          string     `json:"method" bson:"method"`
	Body            string     `json:"body,omitempty" bson:"body,omitempty"`
	Domain          string     `json:"domain" bson:"domain"`
	OK              bool       `json:"ok" bson:"ok"`
	Status          int        `json:"status" bson:"status"`
	Error           string     `json:"error" bson:"error"`
	MS              int64      `json:"ms" bson:"ms"`
	Attempts        int        `json:"attempts" bson:"attempts"`
	ValidationLevel int        `json:"validationLevel" bson:"validationLevel"`
	ValidationGrade string     `json:"validationGrade" bson:"validationGrade"`
	ValidationNotes []string   `json:"validationNotes" bson:"validationNotes"`
	DuplicateCount  int        `json:"duplicateCount" bson:"duplicateCount"`
	Names           []string   `json:"names" bson:"names"`
	RawURLs         []string   `json:"rawUrls" bson:"rawUrls"`
	Representative  BookSource `json:"representative" bson:"representative"`
	UpdatedAt       int64      `json:"updatedAt" bson:"updatedAt"`
}

type DomainDoc struct {
	ID        string `bson:"_id"`
	Domain    string `json:"domain" bson:"domain"`
	Total     int    `json:"total" bson:"total"`
	OK        int    `json:"ok" bson:"ok"`
	Fail      int    `json:"fail" bson:"fail"`
	DomainOK  bool   `json:"domainOk" bson:"domainOk"`
	UpdatedAt int64  `json:"updatedAt" bson:"updatedAt"`
}

type Summary struct {
	Input                 string            `json:"input" bson:"input"`
	Records               int               `json:"records" bson:"records"`
	InvalidRecords        int               `json:"invalidRecords" bson:"invalidRecords"`
	UniqueNormalizedURLs  int               `json:"uniqueNormalizedUrls" bson:"uniqueNormalizedUrls"`
	DuplicateGroups       int               `json:"duplicateGroups" bson:"duplicateGroups"`
	ExtraDuplicateRecords int               `json:"extraDuplicateRecords" bson:"extraDuplicateRecords"`
	UsableDedupSources    int               `json:"usableDedupSources" bson:"usableDedupSources"`
	FailedUniqueURLs      int               `json:"failedUniqueUrls" bson:"failedUniqueUrls"`
	StatusCounts          map[string]int    `json:"statusCounts" bson:"statusCounts"`
	ErrorCounts           map[string]int    `json:"errorCounts" bson:"errorCounts"`
	Outputs               map[string]string `json:"outputs" bson:"outputs"`
	UpdatedAt             int64             `json:"updatedAt" bson:"updatedAt"`
}

type Config struct {
	Input          string        `json:"input"`
	Sources        []string      `json:"sources"`
	RemoteURLs     []string      `json:"remoteUrls"`
	WatchDir       string        `json:"watchDir"`
	Interval       time.Duration `json:"-"`
	IntervalText   string        `json:"interval"`
	OutputDir      string        `json:"outputDir"`
	Workers        int           `json:"workers"`
	Timeout        time.Duration `json:"-"`
	TimeoutText    string        `json:"timeout"`
	Retries        int           `json:"retries"`
	RetryDelay     time.Duration `json:"-"`
	RetryDelayText string        `json:"retryDelay"`
	PublishTarget  string        `json:"publishTarget"`
	MongoURI       string        `json:"mongoURI"`
	MongoDB        string        `json:"mongoDB"`

	RemoteFetchInterval      time.Duration `json:"-"`
	RemoteFetchIntervalText  string        `json:"remoteFetchInterval"`
	UsableProbeInterval      time.Duration `json:"-"`
	UsableProbeIntervalText  string        `json:"usableProbeInterval"`
	InvalidProbeInterval     time.Duration `json:"-"`
	InvalidProbeIntervalText string        `json:"invalidProbeInterval"`
	MaxConsecutiveFailures   int           `json:"maxConsecutiveFailures"`
	MaxRemoteItems           int           `json:"maxRemoteItems"`
	MaxRemoteBookSources     int           `json:"maxRemoteBookSources"`
	MaxRemoteCollections     int           `json:"maxRemoteCollections"`
}

func (cfg Config) withDefaults() Config {
	if cfg.OutputDir == "" {
		cfg.OutputDir = "output"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 80
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = parseDurationDefault(cfg.TimeoutText, 5*time.Second)
	}
	if cfg.Retries <= 0 {
		cfg.Retries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = parseDurationDefault(cfg.RetryDelayText, 500*time.Millisecond)
	}
	if cfg.Interval == 0 {
		cfg.Interval = parseDurationDefault(cfg.IntervalText, 5*time.Minute)
	}
	if cfg.RemoteFetchInterval == 0 {
		cfg.RemoteFetchInterval = parseDurationDefault(cfg.RemoteFetchIntervalText, 15*time.Minute)
	}
	if cfg.UsableProbeInterval == 0 {
		cfg.UsableProbeInterval = parseDurationDefault(cfg.UsableProbeIntervalText, time.Hour)
	}
	if cfg.InvalidProbeInterval == 0 {
		cfg.InvalidProbeInterval = parseDurationDefault(cfg.InvalidProbeIntervalText, 24*time.Hour)
	}
	if cfg.MaxConsecutiveFailures <= 0 {
		cfg.MaxConsecutiveFailures = 60
	}
	if cfg.MaxRemoteItems <= 0 {
		cfg.MaxRemoteItems = 10
	}
	if cfg.MaxRemoteBookSources <= 0 {
		cfg.MaxRemoteBookSources = 20
	}
	if cfg.MaxRemoteCollections <= 0 {
		cfg.MaxRemoteCollections = 1
	}
	if cfg.MongoDB == "" {
		cfg.MongoDB = "booksource_tool"
	}
	if len(cfg.RemoteURLs) == 0 {
		cfg.RemoteURLs = []string{
			"https://www.yck2026.top/yuedu/shuyuan/index.html",
			"https://www.yck2026.top/yuedu/shuyuans/index.html",
		}
	}
	return cfg
}

func parseDurationDefault(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}
