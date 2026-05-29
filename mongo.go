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
		_, _ = db.Collection("connectivity_cache").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"probeUrl": res.ProbeURL, "method": res.Method, "body": res.Body, "domain": res.Domain, "ok": res.OK, "status": res.Status, "finalUrl": res.FinalURL, "error": res.Error, "ms": res.MS, "attempts": res.Attempts, "bodyBytes": res.BodyBytes, "validationLevel": res.ValidationLevel, "validationGrade": res.ValidationGrade, "validationNotes": res.ValidationNotes, "updatedAt": now}}, options.Update().SetUpsert(true))
		if res.OK {
			_, _ = db.Collection("usable_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"normalizedUrl": key, "probeUrl": res.ProbeURL, "method": res.Method, "body": res.Body, "domain": res.Domain, "ok": res.OK, "status": res.Status, "finalUrl": res.FinalURL, "error": res.Error, "ms": res.MS, "attempts": res.Attempts, "bodyBytes": res.BodyBytes, "validationLevel": res.ValidationLevel, "validationGrade": res.ValidationGrade, "validationNotes": res.ValidationNotes, "source": rep, "updatedAt": now}}, options.Update().SetUpsert(true))
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
		if entry.NextProbeAt == 0 && entry.Status != SourceStatusDisabled {
			entry.NextProbeAt = time.Now().Unix()
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
		_, _ = db.Collection("dedup_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": entry}, options.Update().SetUpsert(true))
		if entry.LastProbeAt > 0 {
			_, _ = db.Collection("connectivity_cache").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"probeUrl": entry.LastProbe.ProbeURL, "method": entry.LastProbe.Method, "body": entry.LastProbe.Body, "domain": entry.LastProbe.Domain, "ok": entry.LastProbe.OK, "status": entry.LastProbe.Status, "finalUrl": entry.LastProbe.FinalURL, "error": entry.LastProbe.Error, "ms": entry.LastProbe.MS, "attempts": entry.LastProbe.Attempts, "bodyBytes": entry.LastProbe.BodyBytes, "validationLevel": entry.LastProbe.ValidationLevel, "validationGrade": entry.LastProbe.ValidationGrade, "validationNotes": entry.LastProbe.ValidationNotes, "updatedAt": time.Now().Unix()}}, options.Update().SetUpsert(true))
		}
		if entry.Status == SourceStatusUsable {
			_, _ = db.Collection("usable_sources").UpdateOne(ctx, bson.M{"_id": key}, bson.M{"$set": bson.M{"normalizedUrl": key, "probeUrl": entry.LastProbe.ProbeURL, "method": entry.LastProbe.Method, "body": entry.LastProbe.Body, "domain": entry.LastProbe.Domain, "ok": true, "status": entry.LastProbe.Status, "finalUrl": entry.LastProbe.FinalURL, "error": entry.LastProbe.Error, "ms": entry.LastProbe.MS, "attempts": entry.LastProbe.Attempts, "bodyBytes": entry.LastProbe.BodyBytes, "validationLevel": entry.LastProbe.ValidationLevel, "validationGrade": entry.LastProbe.ValidationGrade, "validationNotes": entry.LastProbe.ValidationNotes, "source": entry.Source, "updatedAt": time.Now().Unix()}}, options.Update().SetUpsert(true))
		}
	}
	for _, spec := range []struct{ col, key string }{{"dedup_sources", "status"}, {"dedup_sources", "nextProbeAt"}, {"dedup_sources", "domain"}, {"connectivity_cache", "domain"}, {"usable_sources", "domain"}} {
		_, _ = db.Collection(spec.col).Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: spec.key, Value: 1}}})
	}
	return nil
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
