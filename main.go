package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

type sourceList []string

func (s *sourceList) String() string { return strings.Join(*s, ",") }
func (s *sourceList) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			*s = append(*s, item)
		}
	}
	return nil
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags() Config {
	configPath := flag.String("config", "etc/config.json", "config file path")
	input := flag.String("input", "", "input Legado book source JSON array")
	var sources sourceList
	var remoteURLs sourceList
	flag.Var(&sources, "source", "additional source JSON path, HTTP(S) URL, git URL, or gitURL::glob; repeatable")
	flag.Var(&remoteURLs, "remote-url", "remote yck-style listing page to poll; repeatable")
	watchDir := flag.String("watch-dir", "", "directory to poll continuously for dropped book source JSON files")
	interval := flag.Float64("interval", 300, "watch loop interval seconds")
	outputDir := flag.String("output-dir", "output", "directory for generated files")
	workers := flag.Int("workers", 80, "parallel probe workers")
	timeout := flag.Float64("timeout", 5, "per-request timeout seconds")
	retries := flag.Int("retries", 3, "probe attempts per source; non-200 or network errors are retried")
	retryDelay := flag.Float64("retry-delay", 0.5, "delay between probe retries in seconds")
	remoteFetchInterval := flag.Float64("remote-fetch-interval", 900, "remote listing fetch interval seconds")
	usableProbeInterval := flag.Float64("usable-probe-interval", 3600, "probe interval seconds for usable sources")
	invalidProbeInterval := flag.Float64("invalid-probe-interval", 86400, "base probe interval seconds for unusable sources")
	maxConsecutiveFailures := flag.Int("max-consecutive-failures", 60, "disable probing after this many consecutive failures and long abnormal duration")
	maxRemoteItems := flag.Int("max-remote-items", 10, "maximum JSON items to fetch from each remote listing page per cycle")
	maxRemoteBookSources := flag.Int("max-remote-book-sources", 20, "maximum latest individual book sources to fetch per cycle")
	maxRemoteCollections := flag.Int("max-remote-collections", 1, "maximum latest source collections to fetch per cycle")
	publishTarget := flag.String("publish-target", "", "optional scp/rsync target for usable JSON, e.g. root@8.135.16.113:/opt/filedown/var/legado-usable-book-sources.json")
	mongoURI := flag.String("mongo-uri", "", "optional MongoDB URI for caching results")
	mongoDB := flag.String("mongo-db", "booksource_tool", "MongoDB database name")
	flag.Parse()
	cfg := loadConfigFile(*configPath)
	if *input == "" && flag.NArg() > 0 {
		*input = flag.Arg(0)
	}
	if *input != "" {
		cfg.Input = *input
		sources = append(sources, *input)
	}
	if len(sources) > 0 {
		cfg.Sources = append(cfg.Sources, sources...)
	}
	if len(remoteURLs) > 0 {
		cfg.RemoteURLs = append(cfg.RemoteURLs, remoteURLs...)
	}
	if *watchDir != "" {
		cfg.WatchDir = *watchDir
	}
	if flagWasSet("interval") {
		cfg.Interval = time.Duration(*interval * float64(time.Second))
	}
	if flagWasSet("output-dir") {
		cfg.OutputDir = *outputDir
	}
	if flagWasSet("workers") {
		cfg.Workers = *workers
	}
	if flagWasSet("timeout") {
		cfg.Timeout = time.Duration(*timeout * float64(time.Second))
	}
	if flagWasSet("retries") {
		cfg.Retries = *retries
	}
	if flagWasSet("retry-delay") {
		cfg.RetryDelay = time.Duration(*retryDelay * float64(time.Second))
	}
	if flagWasSet("remote-fetch-interval") {
		cfg.RemoteFetchInterval = time.Duration(*remoteFetchInterval * float64(time.Second))
	}
	if flagWasSet("usable-probe-interval") {
		cfg.UsableProbeInterval = time.Duration(*usableProbeInterval * float64(time.Second))
	}
	if flagWasSet("invalid-probe-interval") {
		cfg.InvalidProbeInterval = time.Duration(*invalidProbeInterval * float64(time.Second))
	}
	if flagWasSet("max-consecutive-failures") {
		cfg.MaxConsecutiveFailures = *maxConsecutiveFailures
	}
	if flagWasSet("max-remote-items") {
		cfg.MaxRemoteItems = *maxRemoteItems
	}
	if flagWasSet("max-remote-book-sources") {
		cfg.MaxRemoteBookSources = *maxRemoteBookSources
	}
	if flagWasSet("max-remote-collections") {
		cfg.MaxRemoteCollections = *maxRemoteCollections
	}
	if *publishTarget != "" {
		cfg.PublishTarget = *publishTarget
	}
	if *mongoURI != "" {
		cfg.MongoURI = *mongoURI
	}
	if flagWasSet("mongo-db") {
		cfg.MongoDB = *mongoDB
	}
	return cfg.withDefaults()
}

func loadConfigFile(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config %s: %v\n", path, err)
	}
	return cfg
}

func flagWasSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
