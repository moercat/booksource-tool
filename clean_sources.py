#!/usr/bin/env python3
"""Clean Legado book source JSON files.

Input:  a Legado book source JSON array.
Output: a deduplicated JSON array containing only sources whose base URL is reachable.

Dedup rule:
  - Strip request options after `,{...}`.
  - Strip Legado suffixes after `##`, `#`, and `@js:`.
  - Trim trailing slash and lowercase for comparison.

Connectivity rule:
  - Probe each unique normalized URL once.
  - HEAD first, then fallback to small GET.
  - HTTP status < 500 means the URL is considered reachable.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import re
import ssl
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any


DEFAULT_USER_AGENT = "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36"


def normalize_source_url(raw: str) -> str:
    value = (raw or "").strip()
    if not value:
        return ""
    for sep in (",{", "##", "#", "@js:"):
        if sep in value:
            value = value.split(sep, 1)[0]
    value = value.strip().rstrip("/")
    return value.lower()


def probe_url_from_source_url(raw: str) -> str:
    value = normalize_source_url(raw)
    if not value or value.startswith("data:"):
        return ""
    if not re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*://", value):
        value = "https://" + value
    parsed = urllib.parse.urlparse(value)
    if not parsed.hostname:
        return ""
    return urllib.parse.urlunparse((parsed.scheme, parsed.netloc, parsed.path, "", parsed.query, ""))


def domain_of_probe_url(probe_url: str) -> str:
    try:
        return (urllib.parse.urlparse(probe_url).hostname or "").lower()
    except Exception:
        return ""


def source_quality(source: dict[str, Any]) -> tuple[int, int, int, int]:
    rule = source.get("ruleSearch") if isinstance(source.get("ruleSearch"), dict) else {}
    return (
        1 if source.get("enabled", True) else 0,
        1 if source.get("searchUrl") else 0,
        1 if rule.get("bookList") else 0,
        1 if rule.get("bookUrl") else 0,
    )


def choose_representative(sources: list[dict[str, Any]]) -> dict[str, Any]:
    return max(sources, key=source_quality)


def probe(probe_url: str, timeout: float) -> dict[str, Any]:
    started = time.time()
    result: dict[str, Any] = {
        "probeUrl": probe_url,
        "domain": domain_of_probe_url(probe_url),
        "ok": False,
        "status": 0,
        "finalUrl": "",
        "error": "",
        "ms": 0,
    }
    context = ssl._create_unverified_context()
    try:
        req = urllib.request.Request(probe_url, method="HEAD", headers={"User-Agent": DEFAULT_USER_AGENT})
        with urllib.request.urlopen(req, timeout=timeout, context=context) as response:
            result.update(ok=response.status < 500, status=response.status, finalUrl=response.geturl())
            return finish_probe(result, started)
    except Exception:
        pass

    try:
        req = urllib.request.Request(
            probe_url,
            headers={"User-Agent": DEFAULT_USER_AGENT, "Range": "bytes=0-0"},
        )
        with urllib.request.urlopen(req, timeout=timeout, context=context) as response:
            response.read(512)
            result.update(ok=response.status < 500, status=response.status, finalUrl=response.geturl())
    except Exception as exc:
        result["error"] = f"{type(exc).__name__}: {str(exc)[:200]}"
    return finish_probe(result, started)


def finish_probe(result: dict[str, Any], started: float) -> dict[str, Any]:
    result["ms"] = int((time.time() - started) * 1000)
    return result


def load_sources(path: Path) -> list[dict[str, Any]]:
    with path.open("r", encoding="utf-8-sig") as file:
        data = json.load(file)
    if not isinstance(data, list):
        raise ValueError("input JSON must be a list")
    return [item for item in data if isinstance(item, dict)]


def clean_sources(
    input_path: Path,
    output_dir: Path,
    workers: int,
    timeout: float,
    mongo_uri: str = "",
    mongo_db: str = "booksource_tool",
) -> dict[str, Any]:
    sources = load_sources(input_path)
    groups: dict[str, list[dict[str, Any]]] = defaultdict(list)
    invalid_sources: list[dict[str, Any]] = []
    for source in sources:
        key = normalize_source_url(str(source.get("bookSourceUrl") or ""))
        probe_url = probe_url_from_source_url(str(source.get("bookSourceUrl") or ""))
        if not key or not probe_url:
            invalid_sources.append(source)
            continue
        groups[key].append(source)

    probe_urls = {key: probe_url_from_source_url(str(items[0].get("bookSourceUrl") or "")) for key, items in groups.items()}
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as executor:
        probe_results = dict(zip(probe_urls.keys(), executor.map(lambda item: probe(item[1], timeout), probe_urls.items())))

    usable_sources: list[dict[str, Any]] = []
    duplicate_groups: list[dict[str, Any]] = []
    for key, items in groups.items():
        if len(items) > 1:
            duplicate_groups.append(
                {
                    "normalizedUrl": key,
                    "count": len(items),
                    "names": [str(item.get("bookSourceName") or "") for item in items],
                    "rawUrls": [str(item.get("bookSourceUrl") or "") for item in items],
                }
            )
        if probe_results[key]["ok"]:
            usable_sources.append(choose_representative(items))

    output_dir.mkdir(parents=True, exist_ok=True)
    usable_path = output_dir / "usable_dedup_sources.json"
    duplicate_path = output_dir / "duplicate_report.json"
    connectivity_path = output_dir / "connectivity_report.json"
    summary_path = output_dir / "summary.json"

    usable_path.write_text(json.dumps(usable_sources, ensure_ascii=False, indent=2), encoding="utf-8")
    duplicate_path.write_text(json.dumps(sorted(duplicate_groups, key=lambda x: x["count"], reverse=True), ensure_ascii=False, indent=2), encoding="utf-8")
    connectivity_path.write_text(json.dumps(list(probe_results.values()), ensure_ascii=False, indent=2), encoding="utf-8")

    status_counts = Counter(str(item["status"]) for item in probe_results.values())
    error_counts = Counter(str(item["error"]).split(":", 1)[0] for item in probe_results.values() if not item["ok"])
    domain_counts = Counter(item["domain"] for item in probe_results.values() if item.get("domain"))
    summary = {
        "input": str(input_path),
        "records": len(sources),
        "invalidRecords": len(invalid_sources),
        "uniqueNormalizedUrls": len(groups),
        "duplicateGroups": len(duplicate_groups),
        "extraDuplicateRecords": sum(len(items) - 1 for items in groups.values() if len(items) > 1),
        "usableDedupSources": len(usable_sources),
        "failedUniqueUrls": len(groups) - len(usable_sources),
        "statusCounts": dict(status_counts),
        "errorCounts": dict(error_counts),
        "topDuplicateDomains": domain_counts.most_common(30),
        "outputs": {
            "usable": str(usable_path),
            "duplicates": str(duplicate_path),
            "connectivity": str(connectivity_path),
            "summary": str(summary_path),
        },
    }
    summary_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    if mongo_uri:
        cache_to_mongo(mongo_uri, mongo_db, input_path, sources, groups, probe_results, duplicate_groups, usable_sources, summary)
    return summary


def cache_to_mongo(
    mongo_uri: str,
    mongo_db: str,
    input_path: Path,
    sources: list[dict[str, Any]],
    groups: dict[str, list[dict[str, Any]]],
    probe_results: dict[str, dict[str, Any]],
    duplicate_groups: list[dict[str, Any]],
    usable_sources: list[dict[str, Any]],
    summary: dict[str, Any],
) -> None:
    try:
        from pymongo import MongoClient, UpdateOne
    except ImportError as exc:
        raise RuntimeError("pymongo is required for --mongo-uri. Install with: pip install pymongo") from exc

    now = int(time.time())
    client = MongoClient(mongo_uri, serverSelectionTimeoutMS=10000)
    db = client[mongo_db]
    db.source_files.update_one(
        {"_id": str(input_path)},
        {"$set": {**summary, "_id": str(input_path), "updatedAt": now}},
        upsert=True,
    )

    raw_ops = []
    for index, source in enumerate(sources):
        raw_url = str(source.get("bookSourceUrl") or "")
        normalized = normalize_source_url(raw_url)
        probe_url = probe_url_from_source_url(raw_url)
        record_id = hashlib.sha1(f"{input_path}:{index}:{raw_url}:{source.get('bookSourceName','')}".encode("utf-8")).hexdigest()
        raw_ops.append(
            UpdateOne(
                {"_id": record_id},
                {
                    "$set": {
                        "_id": record_id,
                        "sourceFile": str(input_path),
                        "recordIndex": index,
                        "bookSourceName": source.get("bookSourceName"),
                        "bookSourceUrl": raw_url,
                        "normalizedUrl": normalized,
                        "probeUrl": probe_url,
                        "domain": domain_of_probe_url(probe_url),
                        "source": source,
                        "updatedAt": now,
                    }
                },
                upsert=True,
            )
        )
    if raw_ops:
        db.raw_sources.bulk_write(raw_ops, ordered=False)

    dedup_ops = []
    connectivity_ops = []
    usable_ops = []
    for normalized, items in groups.items():
        result = probe_results[normalized]
        representative = choose_representative(items)
        dedup_doc = {
            "_id": normalized,
            "normalizedUrl": normalized,
            "probeUrl": result["probeUrl"],
            "domain": result["domain"],
            "ok": result["ok"],
            "status": result["status"],
            "error": result["error"],
            "ms": result["ms"],
            "duplicateCount": len(items),
            "names": [str(item.get("bookSourceName") or "") for item in items],
            "rawUrls": [str(item.get("bookSourceUrl") or "") for item in items],
            "representative": representative,
            "updatedAt": now,
        }
        dedup_ops.append(UpdateOne({"_id": normalized}, {"$set": dedup_doc}, upsert=True))
        connectivity_ops.append(UpdateOne({"_id": result["probeUrl"]}, {"$set": {**result, "_id": result["probeUrl"], "updatedAt": now}}, upsert=True))
        if result["ok"]:
            usable_ops.append(UpdateOne({"_id": normalized}, {"$set": {"_id": normalized, "source": representative, "domain": result["domain"], "updatedAt": now}}, upsert=True))
    if dedup_ops:
        db.dedup_sources.bulk_write(dedup_ops, ordered=False)
    if connectivity_ops:
        db.connectivity_cache.bulk_write(connectivity_ops, ordered=False)
    if usable_ops:
        db.usable_sources.bulk_write(usable_ops, ordered=False)

    duplicate_ops = [UpdateOne({"_id": item["normalizedUrl"]}, {"$set": {**item, "_id": item["normalizedUrl"], "updatedAt": now}}, upsert=True) for item in duplicate_groups]
    if duplicate_ops:
        db.duplicate_groups.bulk_write(duplicate_ops, ordered=False)

    domain_stats: dict[str, dict[str, Any]] = defaultdict(lambda: {"total": 0, "ok": 0, "fail": 0})
    for result in probe_results.values():
        domain = result.get("domain") or ""
        if not domain:
            continue
        domain_stats[domain]["total"] += 1
        domain_stats[domain]["ok" if result["ok"] else "fail"] += 1
    domain_ops = []
    for domain, stats in domain_stats.items():
        doc = {"_id": domain, "domain": domain, "domainOk": stats["ok"] > 0, "updatedAt": now, **stats}
        domain_ops.append(UpdateOne({"_id": domain}, {"$set": doc}, upsert=True))
    if domain_ops:
        db.domains.bulk_write(domain_ops, ordered=False)

    db.raw_sources.create_index("normalizedUrl")
    db.raw_sources.create_index("domain")
    db.dedup_sources.create_index("domain")
    db.dedup_sources.create_index("ok")
    db.connectivity_cache.create_index("domain")
    db.domains.create_index("domainOk")
    db.usable_sources.create_index("domain")
    client.close()


def main() -> None:
    parser = argparse.ArgumentParser(description="Deduplicate and connectivity-filter Legado book sources.")
    parser.add_argument("input", type=Path, help="input Legado book source JSON array")
    parser.add_argument("--output-dir", type=Path, default=Path("output"), help="directory for generated files")
    parser.add_argument("--workers", type=int, default=80, help="parallel probe workers")
    parser.add_argument("--timeout", type=float, default=5.0, help="per-request timeout seconds")
    parser.add_argument("--mongo-uri", default="", help="optional MongoDB URI for caching results")
    parser.add_argument("--mongo-db", default="booksource_tool", help="MongoDB database name")
    args = parser.parse_args()

    summary = clean_sources(args.input, args.output_dir, args.workers, args.timeout, args.mongo_uri, args.mongo_db)
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
