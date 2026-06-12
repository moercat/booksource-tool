package main

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func loadHistoricalSourcesFromMongo(cfg Config) ([]BookSource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(context.Background())
	cursor, err := client.Database(cfg.MongoDB).Collection("raw_sources").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	sources := []BookSource{}
	for cursor.Next(ctx) {
		var doc struct {
			Source BookSource `bson:"source"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		if len(doc.Source) > 0 {
			sources = append(sources, doc.Source)
		}
	}
	return sources, cursor.Err()
}

func cacheToMongo(cfg Config, sources []BookSource, groups map[string][]BookSource, probeResults map[string]ProbeResult, duplicates []DuplicateGroup, summary Summary) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background())
	db := client.Database(cfg.MongoDB)
	now := time.Now().Unix()

	_, _ = db.Collection("source_files").UpdateOne(ctx, bson.M{"_id": cfg.Input}, bson.M{"$set": summary}, options.Update().SetUpsert(true))
	for _, collection := range []string{"dedup_sources", "connectivity_cache", "duplicate_groups", "domains", "usable_sources"} {
		_, _ = db.Collection(collection).DeleteMany(ctx, bson.M{})
	}
	for i, source := range sources {
		source = cleanBookSourceName(source)
		rawURL := stringField(source, "bookSourceUrl")
		target := ProbeTargetFromBookSource(source)
		id := sha1Hex(fmt.Sprintf("%s:%s:%s", rawURL, stringField(source, "searchUrl"), stringField(source, "bookSourceName")))
		_, _ = db.Collection("raw_sources").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"sourceFile": sourceFileLabel(cfg), "recordIndex": i, "bookSourceName": stringField(source, "bookSourceName"), "bookSourceUrl": rawURL, "normalizedUrl": NormalizeProbeTarget(target), "bookSourceNormalizedUrl": normalizeSourceURL(rawURL), "probeUrl": target.URL, "method": target.Method, "body": target.Body, "domain": domainOf(target.URL), "source": source, "updatedAt": now}}, options.Update().SetUpsert(true))
	}
	for key, items := range groups {
		res := probeResults[key]
		rep := chooseRepresentative(items)
		doc := DedupDoc{ID: key, NormalizedURL: key, ProbeURL: res.ProbeURL, Method: res.Method, Body: res.Body, Domain: res.Domain, OK: res.OK, Status: res.Status, Error: res.Error, MS: res.MS, Attempts: res.Attempts, ValidationLevel: res.ValidationLevel, ValidationGrade: res.ValidationGrade, ValidationNotes: res.ValidationNotes, DuplicateCount: len(items), Names: namesOf(items), RawURLs: rawURLsOf(items), Representative: rep, UpdatedAt: now}
		_, _ = db.Collection("dedup_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": doc}, options.Update().SetUpsert(true))
		_, _ = db.Collection("connectivity_cache").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"probeUrl": res.ProbeURL, "keyword": res.Keyword, "method": res.Method, "body": res.Body, "domain": res.Domain, "ok": res.OK, "status": res.Status, "finalUrl": res.FinalURL, "error": res.Error, "ms": res.MS, "attempts": res.Attempts, "bodyBytes": res.BodyBytes, "validationLevel": res.ValidationLevel, "validationGrade": res.ValidationGrade, "validationNotes": res.ValidationNotes, "sampleTotal": res.SampleTotal, "sampleSuccesses": res.SampleSuccesses, "sampleKeywords": res.SampleKeywords, "sampleResults": res.SampleResults, "updatedAt": now}}, options.Update().SetUpsert(true))
		if res.OK && !shouldExcludeBookSource(rep) {
			_, _ = db.Collection("usable_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"normalizedUrl": key, "probeUrl": res.ProbeURL, "keyword": res.Keyword, "method": res.Method, "body": res.Body, "domain": res.Domain, "ok": res.OK, "status": res.Status, "finalUrl": res.FinalURL, "error": res.Error, "ms": res.MS, "attempts": res.Attempts, "bodyBytes": res.BodyBytes, "validationLevel": res.ValidationLevel, "validationGrade": res.ValidationGrade, "validationNotes": res.ValidationNotes, "sampleTotal": res.SampleTotal, "sampleSuccesses": res.SampleSuccesses, "sampleKeywords": res.SampleKeywords, "sampleResults": res.SampleResults, "source": rep, "updatedAt": now}}, options.Update().SetUpsert(true))
		}
	}
	for _, dup := range duplicates {
		_, _ = db.Collection("duplicate_groups").UpdateOne(ctx, bson.M{"_id": dup.NormalizedURL}, bson.M{"$set": bson.M{"normalizedUrl": dup.NormalizedURL, "count": dup.Count, "names": dup.Names, "rawUrls": dup.RawURLs, "updatedAt": now}}, options.Update().SetUpsert(true))
	}
	domainStats := map[string]*DomainDoc{}
	for _, res := range probeResults {
		if res.Domain == "" {
			continue
		}
		stat := domainStats[res.Domain]
		if stat == nil {
			stat = &DomainDoc{ID: res.Domain, Domain: res.Domain, UpdatedAt: now}
			domainStats[res.Domain] = stat
		}
		stat.Total++
		if res.OK {
			stat.OK++
		} else {
			stat.Fail++
		}
		stat.DomainOK = stat.OK > 0
	}
	for domain, stat := range domainStats {
		_, _ = db.Collection("domains").UpdateOne(ctx, bson.M{"_id": domain}, bson.M{"$set": stat}, options.Update().SetUpsert(true))
	}
	for _, spec := range []struct{ col, key string }{{"raw_sources", "normalizedUrl"}, {"raw_sources", "domain"}, {"dedup_sources", "domain"}, {"dedup_sources", "ok"}, {"connectivity_cache", "domain"}, {"domains", "domainOk"}, {"usable_sources", "domain"}} {
		_, _ = db.Collection(spec.col).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: spec.key, Value: 1}}})
	}
	return nil
}

func restorePoolFromMongo(cfg Config, pool *SourcePool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background())
	cursor, err := client.Database(cfg.MongoDB).Collection("dedup_sources").Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var entry PoolEntry
		if err := cursor.Decode(&entry); err != nil {
			continue
		}
		if entry.Key == "" {
			continue
		}
		if entry.Status == "" {
			if entry.LastProbe.OK {
				entry.Status = SourceStatusUsable
			} else {
				entry.Status = SourceStatusNew
			}
		}
		if entry.ProbeTarget.URL == "" && len(entry.Source) > 0 {
			entry.ProbeTarget = ProbeTargetFromBookSource(entry.Source)
		}
		entry.Source = cleanBookSourceName(entry.Source)
		entry.LastProbe = compactProbeResult(entry.LastProbe)
		entry.Versions = nil
		if entry.Status != SourceStatusDisabled && entry.NextProbeAt == 0 {
			entry.NextProbeAt = defaultNextProbeAt(entry.Status, time.Now(), cfg)
		}
		pool.Entries[entry.Key] = &entry
	}
	return cursor.Err()
}

func persistPoolToMongo(cfg Config, pool *SourcePool, summary Summary) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(context.Background())
	db := client.Database(cfg.MongoDB)
	_, _ = db.Collection("source_files").UpdateOne(ctx, bson.M{"_id": "source-pool"}, bson.M{"$set": summary}, options.Update().SetUpsert(true))
	_, _ = db.Collection("usable_sources").DeleteMany(ctx, bson.M{})
	for key, entry := range pool.Entries {
		persisted := *entry
		persisted.LastProbe = compactProbeResult(persisted.LastProbe)
		persisted.Versions = nil
		_, _ = db.Collection("dedup_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": persisted}, options.Update().SetUpsert(true))
		if entry.LastProbeAt > 0 {
			probe := compactProbeResult(entry.LastProbe)
			_, _ = db.Collection("connectivity_cache").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"probeUrl": probe.ProbeURL, "keyword": probe.Keyword, "method": probe.Method, "body": probe.Body, "domain": probe.Domain, "ok": probe.OK, "status": probe.Status, "finalUrl": probe.FinalURL, "error": probe.Error, "ms": probe.MS, "attempts": probe.Attempts, "bodyBytes": probe.BodyBytes, "validationLevel": probe.ValidationLevel, "validationGrade": probe.ValidationGrade, "validationNotes": probe.ValidationNotes, "sampleTotal": probe.SampleTotal, "sampleSuccesses": probe.SampleSuccesses, "sampleKeywords": probe.SampleKeywords, "sampleResults": probe.SampleResults, "updatedAt": time.Now().Unix()}}, options.Update().SetUpsert(true))
		}
		if entry.Status == SourceStatusUsable && !shouldExcludeBookSource(entry.Source) {
			probe := compactProbeResult(entry.LastProbe)
			_, _ = db.Collection("usable_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"normalizedUrl": key, "probeUrl": probe.ProbeURL, "keyword": probe.Keyword, "method": probe.Method, "body": probe.Body, "domain": probe.Domain, "ok": true, "status": probe.Status, "finalUrl": probe.FinalURL, "error": probe.Error, "ms": probe.MS, "attempts": probe.Attempts, "bodyBytes": probe.BodyBytes, "validationLevel": probe.ValidationLevel, "validationGrade": probe.ValidationGrade, "validationNotes": probe.ValidationNotes, "sampleTotal": probe.SampleTotal, "sampleSuccesses": probe.SampleSuccesses, "sampleKeywords": probe.SampleKeywords, "sampleResults": probe.SampleResults, "source": entry.Source, "updatedAt": time.Now().Unix()}}, options.Update().SetUpsert(true))
		}
	}
	for _, spec := range []struct{ col, key string }{{"dedup_sources", "status"}, {"dedup_sources", "nextProbeAt"}, {"dedup_sources", "domain"}, {"connectivity_cache", "domain"}, {"usable_sources", "domain"}} {
		_, _ = db.Collection(spec.col).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: spec.key, Value: 1}}})
	}
	return nil
}

func defaultNextProbeAt(status string, now time.Time, cfg Config) int64 {
	switch status {
	case SourceStatusUsable:
		return now.Add(jitter(cfg.UsableProbeInterval, status)).Unix()
	case SourceStatusAbnormal, SourceStatusNew:
		return now.Add(jitter(cfg.InvalidProbeInterval, status)).Unix()
	default:
		return now.Unix()
	}
}

func sourceFileLabel(cfg Config) string {
	if cfg.Input != "" {
		return cfg.Input
	}
	if cfg.WatchDir != "" {
		return cfg.WatchDir
	}
	return "dynamic"
}
