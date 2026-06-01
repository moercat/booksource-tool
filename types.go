package main

import "time"

type BookSource map[string]any

type ProbeTarget struct {
	URL    string
	Method string
	Body   string
}

type ProbeJob struct {
	Target   ProbeTarget
	Source   BookSource
	Previous ProbeResult
}

type ProbeResult struct {
	ProbeURL             string              `json:"probeUrl" bson:"probeUrl"`
	Keyword              string              `json:"keyword,omitempty" bson:"keyword,omitempty"`
	Method               string              `json:"method" bson:"method"`
	Body                 string              `json:"body,omitempty" bson:"body,omitempty"`
	Domain               string              `json:"domain" bson:"domain"`
	OK                   bool                `json:"ok" bson:"ok"`
	Status               int                 `json:"status" bson:"status"`
	FinalURL             string              `json:"finalUrl" bson:"finalUrl"`
	Error                string              `json:"error" bson:"error"`
	MS                   int64               `json:"ms" bson:"ms"`
	Attempts             int                 `json:"attempts" bson:"attempts"`
	BodyBytes            int                 `json:"bodyBytes" bson:"bodyBytes"`
	ChineseChars         int                 `json:"chineseChars,omitempty" bson:"chineseChars,omitempty"`
	ValidationLevel      int                 `json:"validationLevel" bson:"validationLevel"`
	ValidationGrade      string              `json:"validationGrade" bson:"validationGrade"`
	ValidationNotes      []string            `json:"validationNotes" bson:"validationNotes"`
	SampleTotal          int                 `json:"sampleTotal,omitempty" bson:"sampleTotal,omitempty"`
	SampleSuccesses      int                 `json:"sampleSuccesses,omitempty" bson:"sampleSuccesses,omitempty"`
	SampleKeywords       []string            `json:"sampleKeywords,omitempty" bson:"sampleKeywords,omitempty"`
	SampleResults        []ProbeSampleResult `json:"sampleResults,omitempty" bson:"sampleResults,omitempty"`
	ExtractedName        string              `json:"extractedName,omitempty" bson:"extractedName,omitempty"`
	ExtractedAuthor      string              `json:"extractedAuthor,omitempty" bson:"extractedAuthor,omitempty"`
	ExtractedLastChapter string              `json:"extractedLastChapter,omitempty" bson:"extractedLastChapter,omitempty"`
	ExtractedKind        string              `json:"extractedKind,omitempty" bson:"extractedKind,omitempty"`
	ExtractedWords       string              `json:"extractedWords,omitempty" bson:"extractedWords,omitempty"`
	DetailOK             bool                `json:"detailOk,omitempty" bson:"detailOk,omitempty"`
	ContentOK            bool                `json:"contentOk,omitempty" bson:"contentOk,omitempty"`
	rawBody              []byte              // response body, not persisted
}

type ProbeSampleResult struct {
	Keyword         string   `json:"keyword" bson:"keyword"`
	ProbeURL        string   `json:"probeUrl" bson:"probeUrl"`
	OK              bool     `json:"ok" bson:"ok"`
	Status          int      `json:"status" bson:"status"`
	Error           string   `json:"error,omitempty" bson:"error,omitempty"`
	MS              int64    `json:"ms" bson:"ms"`
	BodyBytes       int      `json:"bodyBytes" bson:"bodyBytes"`
	ValidationLevel int      `json:"validationLevel" bson:"validationLevel"`
	ValidationGrade string   `json:"validationGrade" bson:"validationGrade"`
	ValidationNotes []string `json:"validationNotes,omitempty" bson:"validationNotes,omitempty"`
}

type DuplicateGroup struct {
	NormalizedURL string   `json:"normalizedUrl" bson:"normalizedUrl"`
	Count         int      `json:"count" bson:"count"`
	Names         []string `json:"names" bson:"names"`
	RawURLs       []string `json:"rawUrls" bson:"rawUrls"`
}

type SourceVersion struct {
	ContentHash string     `json:"contentHash" bson:"contentHash"`
	Source      BookSource `json:"source" bson:"source"`
	AddedAt     int64      `json:"addedAt" bson:"addedAt"`
	Tag         string     `json:"tag,omitempty" bson:"tag,omitempty"` // "good", "bad", "current"
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

type AppUsabilityReport struct {
	GeneratedAt       int64                    `json:"generatedAt"`
	ValidationVersion string                   `json:"validationVersion"`
	ValidationScope   string                   `json:"validationScope"`
	Count             int                      `json:"count"`
	TierCounts        map[string]int           `json:"tierCounts"`
	RiskCounts        map[string]int           `json:"riskCounts"`
	ValidationGrades  map[string]int           `json:"validationGrades"`
	Items             []AppUsabilityReportItem `json:"items"`
}

type AppUsabilityReportItem struct {
	Index                 int      `json:"index"`
	SourceName            string   `json:"sourceName"`
	BookSourceURL         string   `json:"bookSourceUrl"`
	SearchURL             string   `json:"searchUrl"`
	BookSourceType        string   `json:"bookSourceType"`
	ValidationGrade       string   `json:"validationGrade"`
	ValidationLevel       int      `json:"validationLevel"`
	ValidationLevelText   string   `json:"validationLevelText"`
	ServerVerdict         string   `json:"serverVerdict"`
	RuntimeRisk           string   `json:"runtimeRisk"`
	AppUsabilityTier      string   `json:"appUsabilityTier"`
	RiskScore             int      `json:"riskScore"`
	RiskTags              []string `json:"riskTags"`
	ValidationStatusText  string   `json:"validationStatusText"`
	ValidationGroup       string   `json:"validationGroup"`
	SearchStatus          string   `json:"searchStatus"`
	SearchMS              int64    `json:"searchMs"`
	SampleTotal           int      `json:"sampleTotal"`
	SampleSuccesses       int      `json:"sampleSuccesses"`
	SampleKeywords        []string `json:"sampleKeywords,omitempty"`
	SuccessfulSampleAvgMS int64    `json:"successfulSampleAvgMs"`
	SearchSpeedTier       string   `json:"searchSpeedTier"`
	ProbeURL              string   `json:"probeUrl"`
	BodyBytes             int      `json:"bodyBytes"`
	ValidationNotes       []string `json:"validationNotes,omitempty"`
	DetailRisk            string   `json:"detailRisk"`
	TocRisk               string   `json:"tocRisk"`
	ContentRisk           string   `json:"contentRisk"`
	ExploreRisk           string   `json:"exploreRisk"`
	RuntimeRequiredFor    []string `json:"runtimeRequiredFor,omitempty"`
	KeptInExport          bool     `json:"keptInExport"`
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

	RemoteFetchInterval          time.Duration `json:"-"`
	RemoteFetchIntervalText      string        `json:"remoteFetchInterval"`
	UsableProbeInterval          time.Duration `json:"-"`
	UsableProbeIntervalText      string        `json:"usableProbeInterval"`
	InvalidProbeInterval         time.Duration `json:"-"`
	InvalidProbeIntervalText     string        `json:"invalidProbeInterval"`
	MaxConsecutiveFailures       int           `json:"maxConsecutiveFailures"`
	MaxRemoteItems               int           `json:"maxRemoteItems"`
	MaxRemoteBookSources         int           `json:"maxRemoteBookSources"`
	MaxRemoteCollections         int           `json:"maxRemoteCollections"`
	MaxRemoteBackfillBookSources int           `json:"maxRemoteBackfillBookSources"`
	MaxRemoteBackfillCollections int           `json:"maxRemoteBackfillCollections"`
	MaxProbeJobsPerCycle         int           `json:"maxProbeJobsPerCycle"`
	SearchKeywords               []string      `json:"searchKeywords"`
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
	if cfg.MaxRemoteBackfillBookSources <= 0 {
		cfg.MaxRemoteBackfillBookSources = 100
	}
	if cfg.MaxRemoteBackfillCollections <= 0 {
		cfg.MaxRemoteBackfillCollections = 15
	}
	if cfg.MaxProbeJobsPerCycle <= 0 {
		cfg.MaxProbeJobsPerCycle = 120
	}
	if len(cfg.SearchKeywords) == 0 {
		cfg.SearchKeywords = builtinDefaultSearchKeywords()
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
